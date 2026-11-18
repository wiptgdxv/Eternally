package main

import (
	_ "embed"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

//go:embed tray.png
var settingsTrayIcon []byte

const (
	productName   = "Eternally"
	settingsTitle = productName + " 设置"
	panelWidth    = 286
	panelPadding  = 11
	panelInset    = 8
	panelGap      = 5
	candidateGap  = 3
	candidateRow  = 30
)

type panelPalette struct {
	surface, ink, muted, accent, selected, border, hover, errorText ui.Color
}

func panelColors(c *ui.Context) panelPalette {
	if c.Preferences().HighContrast {
		t := c.Theme()
		return panelPalette{t.Background, t.Text, t.Text, t.Text, t.SurfacePressed, t.Text, t.SurfaceHover, t.Text}
	}
	appearance := settingsSnapshot().Appearance
	if appearance == "dark" || appearance == "system" && c.Theme().Dark {
		return panelPalette{ui.Hex("#242829"), ui.Hex("#E1E5E3"), ui.Hex("#ABB4B1"), ui.Hex("#A1CFC2"), ui.Hex("#324B44"), ui.Hex("#424A48"), ui.Hex("#343B39"), ui.Hex("#ECC0AB")}
	}
	return panelPalette{ui.Hex("#F7F7F5"), ui.Hex("#24282A"), ui.Hex("#626A6D"), ui.Hex("#1D6258"), ui.Hex("#DDEDE7"), ui.Hex("#CFD7D2"), ui.Hex("#EAEEEB"), ui.Hex("#903E28")}
}

type panel struct {
	state   draft
	preview bool
	native  bool
	target  target
	owner   uint64
	input   inputMode
	pick    func(string, choice)
	browse  func(string, uint32)

	anchor      anchor
	height      int
	width       int
	wantedWidth int
	resize      func()

	translationActions       translationPanelActions
	updateTranslationActions func(func())
}

func (p *panel) view(c *ui.Context) {
	if p.preview {
		if c.Shortcut(0, ui.KeyEscape) {
			p.state.press(keyEscape, false)
		}
		for k := ui.KeyA; k <= ui.KeyZ; k++ {
			if c.Shortcut(0, k) {
				p.state.press(uint32('A'+k-ui.KeyA), false)
			}
		}
		for k := ui.Key0; k <= ui.Key9; k++ {
			if c.Shortcut(0, k) {
				p.state.press(uint32('0'+k-ui.Key0), false)
			}
		}
		if c.Shortcut(0, ui.KeyLeft) {
			p.state.press(keyLeft, false)
		}
		if c.Shortcut(0, ui.KeyRight) {
			p.state.press(keyRight, false)
		}
		for _, binding := range []struct {
			key  ui.Key
			code uint32
		}{
			{ui.KeyUp, keyUp}, {ui.KeyDown, keyDown}, {ui.KeyDelete, keyDelete},
			{ui.KeyPageUp, keyPageUp}, {ui.KeyPageDown, keyPageDown},
		} {
			if c.Shortcut(0, binding.key) {
				p.state.press(binding.code, false)
			}
		}
		if c.Shortcut(ui.Shift, ui.KeyLeft) {
			p.state.press(editKeyBase+keyLeft, false)
		}
		if c.Shortcut(ui.Shift, ui.KeyRight) {
			p.state.press(editKeyBase+keyRight, false)
		}
		if c.Shortcut(ui.Shift, ui.KeySpace) {
			p.state.press(keyLiteral, false)
		}
		if c.Shortcut(0, ui.KeyQuote) {
			p.state.press(symbolKeyBase+'\'', false)
		}
		if c.Shortcut(0, ui.KeyPeriod) {
			p.state.press(symbolKeyBase+'.', false)
		}
		if c.Shortcut(0, ui.KeyBackspace) {
			p.state.press(keyBackspace, false)
		}
		if c.Shortcut(0, ui.KeySpace) {
			p.state.press(keySpace, false)
		}
		if c.Shortcut(0, ui.KeyEnter) {
			p.state.press(keyEnter, false)
		}
		p.cancelObsoleteSpeech()
	}
	s := p.state
	fontSize := float32(settingsSnapshot().CandidateFontSize)
	rowHeight := max(float32(candidateRow), fontSize+14)
	colors := panelColors(c)
	c.Root().Background(colors.surface)
	all := s.candidates()
	if s.Selected >= len(all) {
		s.Selected = 0
	}
	pick := p.pick
	browse := p.browse
	if p.preview {
		pick = func(_ string, item choice) {
			p.state.selectWord(item)
			p.state.English = sampleTranslation(p.state.Chinese)
		}
		browse = func(_ string, key uint32) {
			p.state.press(key, false)
			c.AnimationFrame()
		}
	}
	canExpand := browse != nil && len(all) > pageSize
	expanded := s.Expanded && len(all) > pageSize
	if expanded && browse != nil && len(all) > expandedPageSize {
		start := s.Selected / expandedPageSize * expandedPageSize
		c.Root().HandleInput(func(event ui.InputEvent) bool {
			if event.Kind != ui.InputScroll || event.DY == 0 || event.Momentum {
				return false
			}
			switch {
			case event.DY < 0 && start > 0:
				browse(s.Pinyin, keyPageUp)
			case event.DY > 0 && start+expandedPageSize < len(all):
				browse(s.Pinyin, keyPageDown)
			}
			return true
		})
	}
	var expandWidth float32
	if canExpand {
		expandWidth = candidateToggleWidth + 2*candidateGap
	}
	start := s.pageStart()
	candidates := all[start:min(start+pageSize, len(all))]
	labels := make([]string, len(candidates))
	widths := make([]float32, len(candidates))
	rowWidth := float32(max(0, len(candidates)-1) * candidateGap)
	for i, candidate := range candidates {
		labels[i] = fmt.Sprintf("%d  %s", i+1, candidate.text)
		labelWidth, _ := c.MeasureText(0, ui.Span{Text: labels[i], Size: fontSize})
		widths[i] = float32(math.Ceil(float64(labelWidth))) + 14
		rowWidth += widths[i]
	}
	minWidth := panelWidth
	if p.input.chinese {
		minWidth = 210
	}
	wantedWidth := max(minWidth, int(rowWidth+expandWidth)+2*panelPadding)
	if expanded {
		wantedWidth = 396
	}
	layoutWidth := wantedWidth
	if !p.preview {
		layoutWidth = min(layoutWidth, availablePanelWidth(p.anchor))
	}
	contentWidth := float32(max(1, layoutWidth-2*panelPadding))
	if !expanded && rowWidth+expandWidth > contentWidth {
		// Give short labels their natural width before sharing space among long ones.
		remaining := contentWidth - expandWidth - float32(max(0, len(widths)-1)*candidateGap)
		ordered := slices.Clone(widths)
		slices.Sort(ordered)
		var limit float32
		for i, itemWidth := range ordered {
			limit = remaining / float32(len(ordered)-i)
			if itemWidth >= limit {
				break
			}
			remaining -= itemWidth
		}
		for i := range widths {
			widths[i] = min(widths[i], max(1, limit))
		}
	}
	height := float32(2 * panelInset)
	ui.Column(c).Fill().Padding(panelInset, panelPadding).Gap(panelGap).Children(func() {
		top := s.Chinese
		if !s.active() {
			top = "中文草稿"
			if p.input.rawSource() {
				top = "输入或粘贴" + sourceLanguageName(p.input.sourceCode())
			}
			if p.preview {
				top = "输入 nihao 或 shijie"
			}
		} else if top != "" && s.Pinyin != "" {
			top += "  "
		}
		var spelling strings.Builder
		left := top
		var sourceAfter string
		if p.input.rawSource() && s.Chinese != "" {
			content := []rune(s.Chinese)
			cursor := len(content) - min(s.SourceTail, len(content))
			left, sourceAfter = string(content[:cursor]), string(content[cursor:])
		}
		caret := s.pinyinCursor()
		breaks := syllableBreaks(s.Pinyin)
		if s.LanguageCode == "ja" || len(all) > 0 && isInlineEnglishCandidate(s.Pinyin, all[0]) {
			breaks = make([]bool, len(s.Pinyin))
		}
		for i, cut := range breaks {
			if cut {
				spelling.WriteByte('\'')
			}
			if i == caret {
				left += spelling.String()
				spelling.Reset()
			}
			spelling.WriteByte(s.Pinyin[i])
		}
		if caret == len(s.Pinyin) {
			left += spelling.String()
			spelling.Reset()
		}
		draftSpans := []ui.Span{{Text: left, Color: colors.ink, Size: 15}}
		if s.active() && s.CommitStatus != "正在上屏…" {
			draftSpans = append(draftSpans, ui.Span{Text: "▏", Color: colors.accent, Size: 15})
		}
		if spelling.Len() > 0 {
			draftSpans = append(draftSpans, ui.Span{Text: spelling.String(), Color: colors.muted, Size: 15})
		}
		if sourceAfter != "" {
			draftSpans = append(draftSpans, ui.Span{Text: sourceAfter, Color: colors.ink, Size: 15})
		}
		_, draftHeight := c.MeasureText(contentWidth, draftSpans...)
		height += draftHeight
		ui.RichText(c, draftSpans...).Width(contentWidth).Height(draftHeight).Shrink(0)
		if expanded {
			height += drawExpandedCandidates(c, contentWidth, fontSize, rowHeight, s, all, colors, pick, browse)
		} else if len(candidates) > 0 {
			height += panelGap + rowHeight
			ui.Row(c).Width(contentWidth).Gap(candidateGap).Shrink(0).AlignItems(ui.Center).Children(func() {
				for i, label := range labels {
					item := ui.ButtonBase(c).Label(label).KeepFocus().Size(widths[i], rowHeight).Shrink(0).Radius(5)
					tint := colors.ink
					if pick != nil {
						item.Cursor(ui.CursorPointer).Tooltip(candidates[i].text)
					}
					if start+i == s.Selected {
						item.Background(colors.selected)
						tint = colors.accent
					} else if pick != nil && item.Hovered() {
						item.Background(colors.hover)
					}
					item.Children(func() {
						ui.Text(c, label).FontSize(fontSize).TextColor(tint).MaxWidth(max(1, widths[i]-12)).NoWrap().MaxLines(1).Ellipsis("…")
					})
					if pick != nil && item.Clicked() {
						pick(s.Pinyin, candidates[i])
					}
				}
				if canExpand {
					ui.Box(c).Grow(1)
					if candidateArrow(c, "展开更多候选", keyDown, true, colors).Expanded(false).Clicked() {
						browse(s.Pinyin, keyToggleCandidates)
					}
				}
			})
		} else {
			hint := ""
			if s.CommitStatus != "" && s.CommitStatus != "正在上屏…" {
				hint = s.CommitStatus
			} else if s.CommitStatus == "" {
				switch {
				case !p.input.chinese && s.Pinyin != "":
					hint = "继续输入拼音"
					if s.LanguageCode == "ja" {
						hint = "继续输入日语罗马字"
					}
				case !p.input.chinese && s.English != "":
					hint = "回车提交" + p.input.targetName()
				case p.input.rawSource():
					hint = sourceLanguageName(p.input.sourceCode()) + " → " + p.input.targetName()
				case s.autoLiteral:
					hint = "原样输入 · 空格结束"
				case s.Literal:
					hint = "原样输入 · Shift + 空格退出"
				case !p.input.chinese && s.Chinese != "":
					hint = "继续输入中文"
					if s.LanguageCode == "ja" {
						hint = "继续输入日语"
					}
				case !p.input.chinese:
					hint = "等待拼音"
					if s.LanguageCode == "ja" {
						hint = "等待日语罗马字"
					}
				}
			}
			if hint != "" || !p.input.chinese {
				height += panelGap + candidateRow
				ui.Row(c).FillWidth().Height(candidateRow).Shrink(0).AlignItems(ui.Center).Children(func() {
					ui.Text(c, hint).FontSize(13).TextColor(colors.muted)
				})
			}
		}
		if !p.input.chinese && (s.Chinese != "" || s.English != "" || s.TranslationStatus != "") {
			ui.Box(c).FillWidth().Height(1).Shrink(0).Background(colors.border)
			height += panelGap + 1
			translation := s.English
			color := colors.accent
			actions := s.English != "" && (p.native || p.preview) && p.updateTranslationActions != nil
			textWidth := contentWidth
			if actions {
				textWidth = max(1, contentWidth-2*(translationActionSize+translationActionGap))
			}
			if translation == "" {
				translation = s.TranslationStatus
				if translation == "" {
					translation = " "
				} else if translation == translationPending || translation == translationSlow {
					color = colors.muted
				} else {
					color = colors.errorText
				}
			}
			spans := []ui.Span{{Text: translation, Color: color, Size: 14}}
			_, previewHeight := c.MeasureText(textWidth, spans...)
			if actions {
				rowHeight := max(previewHeight, translationActionSize)
				height += panelGap + rowHeight
				ui.Row(c).Width(contentWidth).Height(rowHeight).Gap(translationActionGap).Shrink(0).AlignItems(ui.Start).Children(func() {
					ui.RichText(c, spans...).Width(textWidth).Height(previewHeight).Shrink(0).AlignSelf(ui.Center)
					p.drawTranslationActions(c, p.displayedTranslation(), colors)
				})
			} else {
				height += panelGap + previewHeight
				ui.RichText(c, spans...).Width(contentWidth).Height(previewHeight).Shrink(0)
			}
		}
	})
	if newHeight := int(math.Ceil(float64(height))) + 2; newHeight != p.height || layoutWidth != p.width || wantedWidth != p.wantedWidth {
		p.height, p.width, p.wantedWidth = newHeight, layoutWidth, wantedWidth
		if p.resize != nil {
			p.resize()
		}
	}
}

func main() {
	native := slices.Contains(os.Args[1:], "--native")
	preview := !native && !slices.Contains(os.Args[1:], "--moqi")
	var listener net.Listener
	if native {
		applyInputSettings()
		var err error
		listener, err = nativeListener()
		if err != nil {
			log.Fatal(err)
		}
		defer listener.Close()
	}
	mygo.App.SetName(productName)
	p := &panel{preview: preview, native: native, height: 108, width: panelWidth, wantedWidth: panelWidth}
	if preview {
		p.state.Pinyin = "nihao"
	}
	var pending atomic.Pointer[panelUpdate]
	var focusEpoch atomic.Uint64
	var displayed atomic.Uint64
	done := make(chan struct{})
	mygo.App.OnQuit(func() { close(done) })
	mygo.App.WhenReady(func() {
		go dictionary()
		window := mygo.NewWindow(mygo.WindowOptions{
			Title: productName, Width: panelWidth, Height: 138, Hidden: !preview,
			Frameless: !preview, SkipTaskbar: !preview, AlwaysOnTop: !preview,
			BackgroundColor: "#F7F7F5", Content: ui.View(p.view),
		})
		p.updateTranslationActions = window.Update
		window.OnClosed(p.stopTranslationSpeech)
		if preview {
			if err := window.SetIcon(settingsTrayIcon); err != nil {
				log.Printf("preview icon: %v", err)
			}
			p.resize = func() {
				window.Update(func() { window.SetSize(p.width, p.height) })
			}
			return
		}
		keepFocus(window)
		p.resize = func() {
			window.Update(func() {
				if p.state.active() && p.target == foreground() && window.IsVisible() {
					position(window, p.anchor, p.wantedWidth, p.height)
				}
			})
		}
		if native {
			copyLast := &mygo.MenuItem{Label: "复制最近译文", Disabled: true, Click: func(*mygo.MenuItem, *mygo.Window) {
				if translated := latestTranslation(); translated != "" {
					if err := copyTranslationText(translated); err != nil {
						log.Print(err)
					}
				}
			}}
			if tray, err := mygo.NewTray(mygo.TrayOptions{
				ID: "ContextIME.Settings", Icon: settingsTrayIcon, ToolTip: productName,
				Menu: mygo.NewMenu([]*mygo.MenuItem{
					{Label: settingsTitle, Click: func(*mygo.MenuItem, *mygo.Window) { showSettingsWindow() }},
					copyLast,
				}),
			}); err != nil {
				log.Printf("settings tray: %v", err)
			} else {
				tray.OnRightClick(func() { copyLast.SetEnabled(latestTranslation() != "") })
			}
			var noticeText string
			var noticeID uint64
			notice := mygo.NewWindow(mygo.WindowOptions{
				Title: productName + " 模式", Width: modeNoticeWidth, Height: modeNoticeHeight,
				Hidden: true, Frameless: true, SkipTaskbar: true, AlwaysOnTop: true,
				BackgroundColor: "#F7F7F5", Content: ui.View(func(c *ui.Context) {
					colors := panelColors(c)
					c.Root().Background(colors.surface)
					ui.Column(c).Fill().Padding(0, 14).Justify(ui.Center).Children(func() {
						ui.Text(c, noticeText).FontSize(14).TextColor(colors.ink).FillWidth()
					})
				}),
			})
			keepFocus(notice)
			go func() {
				err := serveNative(listener, func(event nativeUpdate) {
					if current := event.update; current.mode != "" {
						notice.Update(func() {
							if current.anchor.target.window != foreground().window {
								return
							}
							noticeText = current.mode
							noticeID++
							id := noticeID
							positionModeNotice(notice, current.anchor)
							notice.ShowInactive()
							time.AfterFunc(1400*time.Millisecond, func() {
								notice.Update(func() {
									if noticeID == id {
										notice.Hide()
									}
								})
							})
						})
						if current.input.direct || current.input.chinese || !current.state.active() {
							window.Update(func() {
								if p.target.window == current.anchor.target.window {
									p.stopTranslationSpeech()
									if current.input.direct || !current.state.active() {
										window.Hide()
									}
								}
							})
						}
						return
					}
					window.Update(func() {
						current := event.update
						if !current.state.active() {
							if p.owner == event.owner {
								p.stopTranslationSpeech()
								window.Hide()
								p.owner = 0
							}
							return
						}
						if current.anchor.target != foreground() {
							return
						}
						p.state, p.target, p.owner = current.state, current.anchor.target, event.owner
						p.input = current.input
						p.cancelObsoleteSpeech()
						p.pick = event.pick
						p.browse = event.browse
						p.anchor = current.anchor
						position(window, current.anchor, p.wantedWidth, p.height)
						window.ShowInactive()
					})
				})
				if err != nil {
					log.Print(err)
				}
			}()
			go watchFocus(window, p, nil, done)
			return
		}
		go func() {
			if err := serveMoqi(os.Stdin, os.Stdout, focusEpoch.Load, displayed.Load, func(update panelUpdate) {
				pending.Store(&update)
				window.Update(func() {
					current := pending.Swap(nil)
					if current == nil {
						return
					}
					p.state = current.state
					p.target = current.anchor.target
					p.anchor = current.anchor
					if !current.state.active() || current.epoch != focusEpoch.Load() || current.anchor.target != foreground() {
						window.Hide()
						return
					}
					position(window, current.anchor, p.wantedWidth, p.height)
					window.ShowInactive()
					displayed.Store(current.serial)
				})
			}); err != nil {
				log.Print(err)
			}
			mygo.App.Quit()
		}()
		go watchFocus(window, p, func() { focusEpoch.Add(1) }, done)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(fmt.Errorf("MyGo: %w", err))
	}
}
