package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

//go:embed overlay.html
var overlayHTML string

type translationView struct {
	Source   string `json:"source"`
	Text     string `json:"text"`
	State    string `json:"state"`
	Revision uint64 `json:"revision"`
}

type Overlay struct {
	current atomic.Pointer[translationView]
}

func (o *Overlay) Copy(revision uint64) error {
	view := o.current.Load()
	if view == nil || view.State != "ready" || view.Revision != revision {
		return errors.New("translation is no longer current")
	}
	return mygo.Clipboard.Write(transfer.TextData(view.Text))
}

type translationResult struct {
	revision uint64
	text     string
	err      error
}

func main() {
	app := mygo.App
	app.SetName("语境翻译")
	if !app.RequestSingleInstanceLock() {
		return
	}
	ui := &Overlay{}
	mygo.Bind(ui)
	app.OnWindowAllClosed(func() {})
	app.WhenReady(func() {
		if !mygo.WebViewAvailable() {
			startupError("请先安装 Microsoft Edge WebView2 Runtime。")
			return
		}
		listener, err := listenToRime()
		if err != nil {
			log.Print(err)
			startupError("无法连接小狼毫。请关闭其他语境翻译实例后再试。")
			return
		}
		done := make(chan struct{})
		app.OnQuit(func() { close(done); _ = listener.Close() })
		events := make(chan inputEvent, 32)
		go readRime(listener, events, done)
		preview, reset := make(chan struct{}, 1), make(chan struct{}, 1)
		var enabled atomic.Bool
		enabled.Store(true)
		window := mygo.NewWindow(mygo.WindowOptions{
			Title: "语境翻译", Width: 360, Height: 90, Hidden: true,
			Frameless: true, Transparent: true, SkipTaskbar: true, AlwaysOnTop: true,
		})
		keepKeyboardInTarget(window)
		ready := make(chan struct{}, 1)
		window.Page().OnDidFinishLoad(func() { signal(ready) })
		window.Page().OnDidFailLoad(func(err *mygo.LoadError) {
			log.Print(err)
			startupError("无法加载译文浮窗。请重新启动语境翻译。")
		})
		window.Page().LoadHTML(overlayHTML, "")
		if slices.Contains(os.Args[1:], "--preview") {
			signal(preview)
		}
		app.OnSecondInstance(func(args []string, _ string) {
			if slices.Contains(args, "--preview") {
				signal(preview)
			}
		})
		_, err = mygo.NewTray(mygo.TrayOptions{
			Icon: trayIcon(), ToolTip: "语境翻译",
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: "跟随翻译", Type: mygo.MenuItemCheckbox, Checked: true, Click: func(item *mygo.MenuItem, _ *mygo.Window) {
					enabled.Store(item.IsChecked())
					signal(reset)
				}},
				{Label: "预览浮窗", Click: func(*mygo.MenuItem, *mygo.Window) { signal(preview) }},
				{Label: "复制当前译文", Click: func(*mygo.MenuItem, *mygo.Window) {
					if view := ui.current.Load(); view != nil {
						_ = ui.Copy(view.Revision)
					}
				}},
				mygo.Separator(),
				{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { app.Quit() }},
			}),
		})
		if err != nil {
			log.Print(err)
			startupError("无法创建语境翻译托盘图标。")
			return
		}
		go func() {
			select {
			case <-ready:
				runTranslation(window, ui, events, preview, reset, done, &enabled)
			case <-done:
			}
		}()
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func startupError(message string) {
	go func() {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{Title: "语境翻译", Detail: message})
		mygo.App.Quit()
	}()
}

func signal(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func runTranslation(window *mygo.Window, ui *Overlay, events <-chan inputEvent, preview, reset, done <-chan struct{}, enabled *atomic.Bool) {
	var source string
	var revision uint64
	var target inputTarget
	var lastEdit time.Time
	var cancel context.CancelFunc
	var showingPreview bool
	var dimensions [2]int
	var rendered translationView
	results := make(chan translationResult)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()

	invalidate := func() {
		revision++
		timer.Stop()
		if cancel != nil {
			cancel()
			cancel = nil
		}
		ui.current.Store(nil)
	}
	defer invalidate()
	clear := func() {
		invalidate()
		source, target, showingPreview = "", inputTarget{}, false
		dimensions = [2]int{}
		window.Hide()
	}
	draw := func(view translationView) {
		rendered = view
		data, _ := json.Marshal(view)
		ctx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		value, err := window.Page().EvalContext(ctx, "return window.renderTranslation("+string(data)+")")
		if err != nil {
			return
		}
		values, ok := value.([]any)
		if !ok || len(values) != 2 {
			return
		}
		w, wok := values[0].(float64)
		h, hok := values[1].(float64)
		if !wok || !hok || w < 1 || h < 1 {
			return
		}
		if !showingPreview && (target != foregroundTarget() || !enabled.Load()) {
			clear()
			return
		}
		dimensions = [2]int{int(w), int(h)}
		if view.State == "ready" {
			ui.current.Store(&view)
		}
		mygo.RunOnMain(func() {
			positionOverlay(window, dimensions[0], dimensions[1], showingPreview)
			if !window.IsVisible() {
				window.ShowInactive()
			}
		})
	}
	for {
		select {
		case <-done:
			return
		case <-reset:
			clear()
		case <-preview:
			clear()
			showingPreview, lastEdit = true, time.Now()
			draw(translationView{"我晚点把更新后的报价发给你。", "I’ll send you the updated quote later.", "ready", revision})
		case event := <-events:
			if !enabled.Load() || event.target.window == 0 || event.target != foregroundTarget() {
				continue
			}
			now := time.Now()
			if showingPreview || event.target != target || now.Sub(lastEdit) > 8*time.Second {
				clear()
			}
			if event.kind == 'R' {
				clear()
				continue
			}
			if event.kind == 'B' {
				characters := []rune(source)
				if len(characters) == 0 {
					continue
				}
				source = string(characters[:len(characters)-1])
			} else {
				if strings.ContainsAny(event.text, "\r\n") {
					clear()
					continue
				}
				characters := []rune(source + event.text)
				source = string(characters[max(0, len(characters)-160):])
			}
			if !strings.ContainsFunc(source, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
				clear()
				continue
			}
			target, lastEdit = event.target, now
			invalidate()
			// Keep an existing overlay synchronized, while delaying the first reveal.
			if dimensions[0] > 0 {
				draw(translationView{source, "翻译中…", "loading", revision})
			}
			if source != "" {
				timer.Reset(500 * time.Millisecond)
			}
		case <-timer.C:
			if source == "" || !enabled.Load() || target != foregroundTarget() {
				clear()
				continue
			}
			view := translationView{source, "翻译中…", "loading", revision}
			if view != rendered || dimensions[0] == 0 {
				draw(view)
			}
			if source == "" {
				continue
			}
			ctx, stop := context.WithCancel(context.Background())
			cancel = stop
			go func(text string, generation uint64) {
				translated, err := translate(ctx, text)
				select {
				case results <- translationResult{generation, translated, err}:
				case <-ctx.Done():
				case <-done:
				}
			}(source, revision)
		case result := <-results:
			if result.revision != revision {
				continue
			}
			if result.err != nil {
				draw(translationView{source, "翻译暂不可用", "error", revision})
				continue
			}
			draw(translationView{source, result.text, "ready", revision})
		case <-ticker.C:
			if dimensions[0] == 0 {
				continue
			}
			if time.Since(lastEdit) > 15*time.Second || (!showingPreview && target != foregroundTarget()) {
				clear()
			}
		}
	}
}

func trayIcon() []byte {
	icon := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 5; y < 27; y++ {
		for x := 5; x < 27; x++ {
			icon.Set(x, y, color.NRGBA{30, 30, 30, 255})
		}
	}
	for y := 10; y < 22; y++ {
		for x := 11; x < 22; x++ {
			if x < 13 || y >= 20 {
				icon.Set(x, y, color.White)
			}
		}
	}
	var data bytes.Buffer
	_ = png.Encode(&data, icon)
	return data.Bytes()
}
