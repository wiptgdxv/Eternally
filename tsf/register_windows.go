package main

import (
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	profilesClass    = guid("{33C53A50-F456-4884-B049-85FD643ECFED}")
	profilesIID      = guid("{1F02B6C5-7842-4EE6-8A0B-9A24183A95CA}")
	categoriesClass  = guid("{A4B544A1-438D-4B41-9325-869523E2D6C7}")
	categoriesIID    = guid("{C3ACEFB5-F69D-4905-938F-FCADCF4BE830}")
	keyboardCategory = guid("{34745C63-B2F0-4784-8B67-5E12C8701A31}")
)

const serviceName = "语境翻译 Go 原型"
const lang = 0x0804
const comKey = `Software\Classes\CLSID\{7F033F4B-8C89-4586-9E26-A910C3CB83CC}`

func ownDLL() string {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	var module uintptr
	// FROM_ADDRESS | UNCHANGED_REFCOUNT. Use an address inside this DLL.
	callback := syscall.NewCallback(moduleAnchor)
	res, _, _ := kernel.NewProc("GetModuleHandleExW").Call(0x6, callback, uintptr(ptr(&module)))
	if res == 0 {
		return ""
	}
	path := make([]uint16, 32768)
	n, _, _ := kernel.NewProc("GetModuleFileNameW").Call(module, uintptr(ptr(&path[0])), uintptr(len(path)))
	if n == 0 || n >= uintptr(len(path)) {
		return ""
	}
	return windows.UTF16ToString(path[:n])
}

func moduleAnchor() uintptr { return 0 }

func configureTSF(path string, remove bool) uintptr {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	uninit := ole.NewProc("CoUninitialize")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if !isFailure(hr) {
		defer uninit.Call()
	} else if uint32(hr) != 0x80010106 {
		return hr
	}

	var profiles, categories uintptr
	createInstance := ole.NewProc("CoCreateInstance")
	hr, _, _ = createInstance.Call(uintptr(ptr(&profilesClass)), 0, 1, uintptr(ptr(&profilesIID)), uintptr(ptr(&profiles)))
	if isFailure(hr) {
		return hr
	}
	defer method(profiles, 2)
	hr, _, _ = createInstance.Call(uintptr(ptr(&categoriesClass)), 0, 1, uintptr(ptr(&categoriesIID)), uintptr(ptr(&categories)))
	if isFailure(hr) {
		return hr
	}
	defer method(categories, 2)

	clean := func() {
		method(categories, 4, ptr(&clsid), ptr(&keyboardCategory), ptr(&clsid))
		method(profiles, 6, ptr(&clsid), lang, ptr(&profile))
		method(profiles, 4, ptr(&clsid))
		_ = registry.DeleteKey(registry.CURRENT_USER, comKey+`\InprocServer32`)
		_ = registry.DeleteKey(registry.CURRENT_USER, comKey)
	}
	if remove {
		clean()
		return ok
	}
	if path == "" {
		return failed
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, comKey+`\InprocServer32`, registry.SET_VALUE)
	if err != nil {
		return failed
	}
	pathErr := key.SetStringValue("", path)
	threadErr := key.SetStringValue("ThreadingModel", "Apartment")
	key.Close()
	if pathErr != nil || threadErr != nil {
		clean()
		return failed
	}

	name := windows.StringToUTF16(serviceName)
	icon := windows.StringToUTF16(path)
	for _, step := range []func() uintptr{
		func() uintptr { return method(profiles, 3, ptr(&clsid)) },
		func() uintptr {
			return method(profiles, 5, ptr(&clsid), lang, ptr(&profile), ptr(&name[0]), len(name)-1, ptr(&icon[0]), len(icon)-1, 0)
		},
		func() uintptr { return method(categories, 3, ptr(&clsid), ptr(&keyboardCategory), ptr(&clsid)) },
		func() uintptr { return method(profiles, 17, ptr(&clsid), lang, ptr(&profile), 1) },
	} {
		if hr = step(); isFailure(hr) {
			clean()
			return hr
		}
	}
	return ok
}
