package main

import (
	"context"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var combase = windows.NewLazySystemDLL("combase.dll")
var roInitialize = combase.NewProc("RoInitialize")
var roUninitialize = combase.NewProc("RoUninitialize")
var roGetActivationFactory = combase.NewProc("RoGetActivationFactory")
var windowsCreateString = combase.NewProc("WindowsCreateString")
var windowsDeleteString = combase.NewProc("WindowsDeleteString")
var windowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")

var conversionFactoryID = windows.GUID{Data1: 0xfcaa3781, Data2: 0x3083, Data3: 0x49ab, Data4: [8]byte{0xbe, 0x15, 0x56, 0xdf, 0xbb, 0xb7, 0x4d, 0x6f}}
var asyncInfoID = windows.GUID{Data1: 0x00000036, Data2: 0, Data3: 0, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}

type conversionObject struct{ vtable *[11]uintptr }

//go:uintptrescapes
func (o *conversionObject) call(slot int, args ...uintptr) bool {
	params := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	hr, _, _ := syscall.SyscallN(o.vtable[slot], params...)
	return int32(hr) >= 0
}

func makeHString(value string) uintptr {
	chars, err := windows.UTF16FromString(value)
	if err != nil {
		return 0
	}
	var handle uintptr
	windowsCreateString.Call(uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1), uintptr(unsafe.Pointer(&handle)))
	return handle
}

func windowsCandidates(ctx context.Context, input string) []string {
	return windowsCandidatesForLanguage(ctx, input, "zh-Hans")
}

// TextConversionGenerator supports Chinese pinyin and Japanese kana. Keep its
// language tag separate from the translation target and the current key layout.
func windowsCandidatesForLanguage(ctx context.Context, input, languageTag string) []string {
	switch strings.ToLower(languageTag) {
	case "zh", "zh-cn", "zh-hans":
		languageTag = "zh-Hans"
	case "ja", "ja-jp":
		languageTag = "ja"
		input = romajiKana(input)
	default:
		return nil
	}
	if input == "" || ctx.Err() != nil {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const multiThreaded = 1
	hr, _, _ := roInitialize.Call(multiThreaded)
	if int32(hr) < 0 {
		return nil
	}
	defer roUninitialize.Call()
	class := makeHString("Windows.Data.Text.TextConversionGenerator")
	language := makeHString(languageTag)
	pinyin := makeHString(input)
	defer windowsDeleteString.Call(class)
	defer windowsDeleteString.Call(language)
	defer windowsDeleteString.Call(pinyin)
	if class == 0 || language == 0 || pinyin == 0 || ctx.Err() != nil {
		return nil
	}

	var factory, generator, operation, info, list *conversionObject
	hr, _, _ = roGetActivationFactory.Call(class, uintptr(unsafe.Pointer(&conversionFactoryID)), uintptr(unsafe.Pointer(&factory)))
	if int32(hr) < 0 || factory == nil {
		return nil
	}
	defer factory.call(2) // IUnknown::Release
	if !factory.call(6, language, uintptr(unsafe.Pointer(&generator))) || generator == nil {
		return nil
	}
	defer generator.call(2)
	if !generator.call(9, pinyin, candidateLimit, uintptr(unsafe.Pointer(&operation))) || operation == nil {
		return nil
	}
	defer operation.call(2)
	if !operation.call(0, uintptr(unsafe.Pointer(&asyncInfoID)), uintptr(unsafe.Pointer(&info))) || info == nil {
		return nil
	}
	defer info.call(2)
	defer info.call(10) // IAsyncInfo::Close
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status int32
		if !info.call(7, uintptr(unsafe.Pointer(&status))) {
			return nil
		}
		if status != 0 {
			if status != 1 {
				return nil
			}
			break
		}
		select {
		case <-ctx.Done():
			info.call(9) // Cancel
			return nil
		case <-ticker.C:
		}
	}
	if !operation.call(8, uintptr(unsafe.Pointer(&list))) || list == nil {
		return nil
	}
	defer list.call(2)
	var size uint32
	if !list.call(7, uintptr(unsafe.Pointer(&size))) {
		return nil
	}
	var words []string
	for i := uint32(0); i < min(size, candidateLimit); i++ {
		var value uintptr
		if list.call(6, uintptr(i), uintptr(unsafe.Pointer(&value))) && value != 0 {
			var length uint32
			ptr, _, _ := windowsGetStringRawBuffer.Call(value, uintptr(unsafe.Pointer(&length)))
			words = append(words, windows.UTF16PtrToString((*uint16)(unsafe.Pointer(ptr))))
			windowsDeleteString.Call(value)
		}
	}
	return words
}
