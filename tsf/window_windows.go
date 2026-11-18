package main

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var user = windows.NewLazySystemDLL("user32.dll")
var createWindow = user.NewProc("CreateWindowExW")
var destroyWindow = user.NewProc("DestroyWindow")
var showWindow = user.NewProc("ShowWindow")
var setWindowText = user.NewProc("SetWindowTextW")
var moveWindow = user.NewProc("SetWindowPos")
var cursorPosition = user.NewProc("GetCursorPos")
var clientToScreen = user.NewProc("ClientToScreen")
var threadInfo = user.NewProc("GetGUIThreadInfo")
var keyState = user.NewProc("GetKeyState")
var sendMessage = user.NewProc("SendMessageW")
var gdi = windows.NewLazySystemDLL("gdi32.dll")
var stockObject = gdi.NewProc("GetStockObject")

type point struct{ x, y int32 }
type rect struct{ left, top, right, bottom int32 }
type guiInfo struct {
	size, flags                               uint32
	active, focus, capture, menu, move, caret uintptr
	area                                      rect
}

func shortcutHeld() bool {
	for _, key := range []uintptr{0x11, 0x12, 0x5b, 0x5c} {
		if value, _, _ := keyState.Call(key); value&0x8000 != 0 {
			return true
		}
	}
	return false
}

func (s *service) draw() {
	if s.phonetic == "" && s.draft == "" {
		if s.window != 0 {
			showWindow.Call(s.window, 0)
		}
		return
	}
	if s.window == 0 {
		class := windows.StringToUTF16Ptr("STATIC")
		s.window, _, _ = createWindow.Call(0x08000088, uintptr(ptr(class)), 0, 0x80800000, 0, 0, 300, 92, 0, 0, 0, 0)
		if s.window == 0 {
			return
		}
		font, _, _ := stockObject.Call(17)
		sendMessage.Call(s.window, 0x0030, font, 0)
	}
	rows := []string{s.draft}
	if s.phonetic != "" {
		row := s.phonetic
		if word := candidate(s.phonetic); word != "" {
			row += "    1. " + word
		}
		rows = append(rows, row)
	}
	if s.english != "" {
		rows = append(rows, s.english)
	}
	label := windows.StringToUTF16Ptr(strings.Join(rows, "\r\n"))
	setWindowText.Call(s.window, uintptr(ptr(label)))
	position := point{}
	info := guiInfo{size: uint32(unsafe.Sizeof(guiInfo{}))}
	threadInfo.Call(0, uintptr(ptr(&info)))
	if info.caret != 0 {
		position = point{info.area.left, info.area.bottom}
		if rc, _, _ := clientToScreen.Call(info.caret, uintptr(ptr(&position))); rc == 0 {
			cursorPosition.Call(uintptr(ptr(&position)))
		}
	} else {
		cursorPosition.Call(uintptr(ptr(&position)))
	}
	moveWindow.Call(s.window, ^uintptr(0), uintptr(position.x), uintptr(position.y+8), 300, 86, 0x0010|0x0040)
}
