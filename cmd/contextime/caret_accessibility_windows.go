package main

import (
	"math"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var automationBusy atomic.Bool
var automationClass = windows.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
var automationIID = windows.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
var textPatternIID = windows.GUID{Data1: 0x32eba289, Data2: 0x3583, Data3: 0x42c9, Data4: [8]byte{0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a}}
var ole32 = windows.NewLazySystemDLL("ole32.dll")
var coInitializeEx = ole32.NewProc("CoInitializeEx")
var coUninitialize = ole32.NewProc("CoUninitialize")
var coCreateInstance = ole32.NewProc("CoCreateInstance")
var oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")
var safeArrayDestroy = oleaut32.NewProc("SafeArrayDestroy")
var safeArrayGetElement = oleaut32.NewProc("SafeArrayGetElement")

type automationObject struct{ vtable *[17]uintptr }

//go:uintptrescapes
func (o *automationObject) call(slot int, args ...uintptr) bool {
	params := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	hr, _, _ := syscall.SyscallN(o.vtable[slot], params...)
	return int32(hr) >= 0
}

// accessibilityCaret queries only the selection rectangle, on a thread outside
// the UI and the TSF request path because the accessibility provider may block.
func accessibilityCaret(expected target) (anchor, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := coInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
	if int32(hr) < 0 {
		return anchor{}, false
	}
	defer coUninitialize.Call()
	if foreground() != expected {
		return anchor{}, false
	}

	var automation *automationObject
	hr, _, _ = coCreateInstance.Call(uintptr(unsafe.Pointer(&automationClass)), 0, 1,
		uintptr(unsafe.Pointer(&automationIID)), uintptr(unsafe.Pointer(&automation)))
	if int32(hr) < 0 || automation == nil {
		return anchor{}, false
	}
	defer automation.call(2) // IUnknown::Release
	var element, pattern, ranges, selection *automationObject
	if !automation.call(8, uintptr(unsafe.Pointer(&element))) || element == nil { // GetFocusedElement
		return anchor{}, false
	}
	defer element.call(2)
	const textPattern = 10014
	if !element.call(14, textPattern, uintptr(unsafe.Pointer(&textPatternIID)), uintptr(unsafe.Pointer(&pattern))) || pattern == nil { // GetCurrentPatternAs
		return anchor{}, false
	}
	defer pattern.call(2)
	if !pattern.call(5, uintptr(unsafe.Pointer(&ranges))) || ranges == nil { // GetSelection
		return anchor{}, false
	}
	defer ranges.call(2)
	if !ranges.call(4, 0, uintptr(unsafe.Pointer(&selection))) || selection == nil { // GetElement
		return anchor{}, false
	}
	defer selection.call(2)

	var bounds uintptr
	if !selection.call(10, uintptr(unsafe.Pointer(&bounds))) || bounds == 0 { // GetBoundingRectangles
		return anchor{}, false
	}
	defer safeArrayDestroy.Call(bounds)
	var dimensions [4]float64
	for i := int32(0); i < int32(len(dimensions)); i++ {
		hr, _, _ := safeArrayGetElement.Call(bounds, uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&dimensions[i])))
		if int32(hr) < 0 || math.IsNaN(dimensions[i]) || math.Abs(dimensions[i]) > math.MaxInt32/2 {
			return anchor{}, false
		}
	}
	left, top, width, height := dimensions[0], dimensions[1], dimensions[2], dimensions[3]
	if width < 0 || height < 1 || foreground() != expected {
		return anchor{}, false
	}
	return anchor{
		target: expected, point: point{int32(math.Round(left)), int32(math.Round(top + height))},
		top: int32(math.Round(top)), caret: true, accessible: true,
	}, true
}
