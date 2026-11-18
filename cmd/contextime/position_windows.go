package main

import (
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var user = windows.NewLazySystemDLL("user32.dll")
var getForeground = user.NewProc("GetForegroundWindow")
var getGUI = user.NewProc("GetGUIThreadInfo")
var getClientRect = user.NewProc("GetClientRect")
var toScreen = user.NewProc("ClientToScreen")
var getLong = user.NewProc("GetWindowLongPtrW")
var setLong = user.NewProc("SetWindowLongPtrW")
var setPos = user.NewProc("SetWindowPos")
var getMonitor = user.NewProc("MonitorFromPoint")
var getWorkArea = user.NewProc("GetMonitorInfoW")
var setDPI = user.NewProc("SetThreadDpiAwarenessContext")
var getDPI = user.NewProc("GetDpiForWindow")
var sendInput = user.NewProc("SendInput")
var getAsyncKeyState = user.NewProc("GetAsyncKeyState")

// INPUT's union is sized for MOUSEINPUT on Windows x64.
type unicodeKeyInput struct {
	kind, padding uint32
	virtual, scan uint16
	flags, time   uint32
	extra         uintptr
	unionTail     uint64
}

func modifiersDown() bool {
	for _, modifier := range [...]uintptr{0x10, 0x11, 0x12, 0x5B, 0x5C} {
		if state, _, _ := getAsyncKeyState.Call(modifier); state&0x8000 != 0 {
			return true
		}
	}
	return false
}

func insertUnicode(value string, expected target) int {
	const (
		inputKeyboard = 1
		keyUp         = 2
		unicode       = 4
	)
	units := utf16.Encode([]rune(value))
	inputs := make([]unicodeKeyInput, 0, len(units)*2)
	for _, unit := range units {
		inputs = append(inputs,
			unicodeKeyInput{kind: inputKeyboard, scan: unit, flags: unicode},
			unicodeKeyInput{kind: inputKeyboard, scan: unit, flags: unicode | keyUp},
		)
	}
	if len(inputs) == 0 || modifiersDown() || expected.focus == 0 || foreground() != expected {
		return 0
	}
	sent, _, _ := sendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	runtime.KeepAlive(inputs)
	if sent == uintptr(len(inputs)) {
		return 1
	}
	if sent != 0 {
		return -1
	}
	return 0
}

type target struct{ window, focus uintptr }
type point struct{ x, y int32 }
type rect struct{ left, top, right, bottom int32 }
type guiInfo struct {
	size, flags                               uint32
	active, focus, capture, menu, move, caret uintptr
	rect                                      rect
}
type monitorInfo struct {
	size          uint32
	monitor, work rect
	flags         uint32
}
type anchor struct {
	target     target
	point      point
	top        int32
	caret      bool
	tsf        bool
	accessible bool
}
type panelUpdate struct {
	state  draft
	anchor anchor
	epoch  uint64
	serial uint64
	mode   string
	input  inputMode
}

func foreground() target {
	return currentAnchor().target
}

func currentAnchor() anchor {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	old, _, _ := setDPI.Call(^uintptr(3))
	if old != 0 {
		defer setDPI.Call(old)
	}
	window, _, _ := getForeground.Call()
	info := guiInfo{size: uint32(unsafe.Sizeof(guiInfo{}))}
	a := anchor{target: target{window: window}}
	ok, _, _ := getGUI.Call(0, uintptr(unsafe.Pointer(&info)))
	if ok != 0 && info.active == window {
		a.target.focus = info.focus
		if info.caret != 0 && info.rect.bottom > info.rect.top {
			a.point = point{info.rect.left, info.rect.bottom}
			if ok, _, _ := toScreen.Call(info.caret, uintptr(unsafe.Pointer(&a.point))); ok != 0 {
				a.top = a.point.y - (info.rect.bottom - info.rect.top)
				a.caret = true
				return a
			}
		}
	}
	control := a.target.focus
	if control == 0 {
		control = window
	}
	var client rect
	if ok, _, _ := getClientRect.Call(control, uintptr(unsafe.Pointer(&client))); ok != 0 {
		a.point = point{client.left + 12, client.bottom - 12}
		toScreen.Call(control, uintptr(unsafe.Pointer(&a.point)))
	}
	return a
}

func keepFocus(window *mygo.Window) {
	const extended = ^uintptr(19)
	h := window.NativeHandle()
	flags, _, _ := getLong.Call(h, extended)
	setLong.Call(h, extended, flags|0x08000000)
}

func anchorWorkArea(a anchor) (rect, int, bool) {
	dpi, _, _ := getDPI.Call(a.target.window)
	if dpi == 0 {
		dpi = 96
	}
	p := a.point
	packed := uintptr(uint64(uint32(p.x)) | uint64(uint32(p.y))<<32)
	m, _, _ := getMonitor.Call(packed, 2)
	info := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	ok, _, _ := getWorkArea.Call(m, uintptr(unsafe.Pointer(&info)))
	return info.work, int(dpi), ok != 0
}

func availablePanelWidth(a anchor) int {
	work, dpi, ok := anchorWorkArea(a)
	if !ok {
		return panelWidth
	}
	return int(work.right-work.left) * 96 / dpi
}

const caretGap = 4

const modeNoticeWidth, modeNoticeHeight = 168, 42

func positionModeNotice(window *mygo.Window, a anchor) {
	work, dpi, ok := anchorWorkArea(a)
	if !ok {
		return
	}
	const margin = 16
	wPx := min(int32(modeNoticeWidth*dpi/96), work.right-work.left)
	hPx := min(int32(modeNoticeHeight*dpi/96), work.bottom-work.top)
	inset := int32(margin * dpi / 96)
	x := max(work.left, work.right-wPx-inset)
	y := max(work.top, work.bottom-hPx-inset)
	setPos.Call(window.NativeHandle(), ^uintptr(0), uintptr(x), uintptr(y), uintptr(wPx), uintptr(hPx), 0x0010)
}

func position(window *mygo.Window, a anchor, width, h int) {
	work, dpi, ok := anchorWorkArea(a)
	if !ok {
		return
	}
	wPx := min(int32(width*dpi/96), work.right-work.left)
	hPx := min(int32(h*dpi/96), work.bottom-work.top)
	p := a.point
	x := max(work.left, min(p.x, work.right-wPx))
	gap := int32(max(1, caretGap*dpi/96))
	y := p.y + gap
	if y+hPx > work.bottom {
		top := p.y
		if a.caret {
			top = a.top
		}
		if top-work.top > work.bottom-p.y {
			y = top - hPx - gap
		}
	}
	y = max(work.top, min(y, work.bottom-hPx))
	setPos.Call(window.NativeHandle(), ^uintptr(0), uintptr(x), uintptr(y), uintptr(wPx), uintptr(hPx), 0x0010)
}

func watchFocus(window *mygo.Window, p *panel, lost func(), done <-chan struct{}) {
	tick := time.NewTicker(120 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			mygo.RunOnMain(func() {
				if p.target.window == 0 || !window.IsVisible() {
					p.stopTranslationSpeech()
					return
				}
				location := currentAnchor()
				if location.target != p.target {
					if lost != nil {
						lost()
					}
					p.stopTranslationSpeech()
					window.Hide()
				} else if p.state.active() && !p.anchor.tsf && (location.caret || !p.anchor.accessible) && location != p.anchor {
					p.anchor = location
					position(window, location, p.wantedWidth, p.height)
				}
			})
		}
	}
}
