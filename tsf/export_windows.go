package main

import "C"
import (
	"unsafe"

	"golang.org/x/sys/windows"
)

//export DllGetClassObject
func DllGetClassObject(class, iid, out unsafe.Pointer) C.int {
	if out == nil || class == nil || iid == nil {
		return badPointerSigned()
	}
	*(*uintptr)(out) = 0
	if *(*windows.GUID)(class) != clsid {
		return signed(classUnavailable)
	}
	o := create([]windows.GUID{classFactory}, []uintptr{factoryTable}, nil, nil)
	if o == nil {
		return signed(failed)
	}
	defer release(o.primary)
	return signed(query(o.primary, uintptr(iid), uintptr(out)))
}

//export DllCanUnloadNow
func DllCanUnloadNow() C.int {
	// Keep the Go runtime loaded for the lifetime of this host process.
	return 1 // S_FALSE
}

//export DllRegisterServer
func DllRegisterServer() C.int { return signed(configureTSF(ownDLL(), false)) }

//export DllUnregisterServer
func DllUnregisterServer() C.int { return signed(configureTSF("", true)) }

func signed(hr uintptr) C.int { return C.int(int32(hr)) }
func badPointerSigned() C.int { return signed(badPointer) }
