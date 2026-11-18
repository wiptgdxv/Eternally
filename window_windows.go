package main

import (
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	getForegroundWindow = user32.NewProc("GetForegroundWindow")
	getGUIThreadInfo    = user32.NewProc("GetGUIThreadInfo")
	clientToScreen      = user32.NewProc("ClientToScreen")
	getCursorPos        = user32.NewProc("GetCursorPos")
	monitorFromPoint    = user32.NewProc("MonitorFromPoint")
	getMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	getWindowLong       = user32.NewProc("GetWindowLongPtrW")
	setWindowLong       = user32.NewProc("SetWindowLongPtrW")
	setWindowPos        = user32.NewProc("SetWindowPos")
	getDPIForWindow     = user32.NewProc("GetDpiForWindow")
	setThreadDPIContext = user32.NewProc("SetThreadDpiAwarenessContext")
)

type point struct{ x, y int32 }
type rect struct{ left, top, right, bottom int32 }
type guiThreadInfo struct {
	size, flags                                    uint32
	active, focus, capture, menuOwner, move, caret uintptr
	caretRect                                      rect
}
type monitorInfo struct {
	size          uint32
	monitor, work rect
	flags         uint32
}

type inputTarget struct{ window, focus uintptr }

func foregroundTarget() inputTarget {
	hwnd, _, _ := getForegroundWindow.Call()
	info := guiThreadInfo{size: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	if ok, _, _ := getGUIThreadInfo.Call(0, uintptr(unsafe.Pointer(&info))); ok != 0 && info.active == hwnd {
		return inputTarget{hwnd, info.focus}
	}
	return inputTarget{window: hwnd}
}

func keepKeyboardInTarget(window *mygo.Window) {
	const exStyle = ^uintptr(19) // GWL_EXSTYLE = -20
	const noActivate = 0x08000000
	hwnd := window.NativeHandle()
	style, _, _ := getWindowLong.Call(hwnd, exStyle)
	setWindowLong.Call(hwnd, exStyle, style|noActivate)
}

// Physical coordinates keep the anchor and the work area on the same monitor
// when the user's displays have different scale factors.
func positionOverlay(window *mygo.Window, width, height int, preview bool) {
	const perMonitorAwareV2 = ^uintptr(3)
	oldContext, _, _ := setThreadDPIContext.Call(perMonitorAwareV2)
	if oldContext != 0 {
		defer setThreadDPIContext.Call(oldContext)
	}
	anchor := point{}
	info := guiThreadInfo{size: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	getGUIThreadInfo.Call(0, uintptr(unsafe.Pointer(&info)))
	if info.caret != 0 && !preview {
		anchor = point{info.caretRect.left, info.caretRect.top}
		if ok, _, _ := clientToScreen.Call(info.caret, uintptr(unsafe.Pointer(&anchor))); ok == 0 {
			getCursorPos.Call(uintptr(unsafe.Pointer(&anchor)))
		}
	} else {
		getCursorPos.Call(uintptr(unsafe.Pointer(&anchor)))
	}
	packed := uintptr(uint64(uint32(anchor.x)) | uint64(uint32(anchor.y))<<32)
	monitor, _, _ := monitorFromPoint.Call(packed, 2) // MONITOR_DEFAULTTONEAREST
	area := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := getMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&area))); ok == 0 {
		return
	}
	hwnd := window.NativeHandle()
	dpi := uintptr(96)
	if active := foregroundTarget().window; active != 0 {
		if v, _, _ := getDPIForWindow.Call(active); v != 0 {
			dpi = v
		}
	}
	scale := func(n int) int32 { return int32((n*int(dpi) + 48) / 96) }
	w, h := scale(width), scale(height)
	x := min(max(anchor.x, area.work.left), max(area.work.left, area.work.right-w))
	y := anchor.y - h - scale(8)
	if y < area.work.top {
		y = anchor.y + max(info.caretRect.bottom-info.caretRect.top, scale(20)) + scale(8)
	}
	y = min(max(y, area.work.top), max(area.work.top, area.work.bottom-h))
	const noActivate = 0x0010
	setWindowPos.Call(hwnd, ^uintptr(0), uintptr(x), uintptr(y), uintptr(w), uintptr(h), noActivate)
}
