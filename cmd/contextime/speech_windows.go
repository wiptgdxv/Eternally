package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	localeNameToLCID = windows.NewLazySystemDLL("kernel32.dll").NewProc("LocaleNameToLCID")
	errNoSpeechVoice = errors.New("未安装对应的系统语音")
	speechOutput     sync.Mutex

	speechVoiceClass = windows.GUID{Data1: 0x96749377, Data2: 0x3391, Data3: 0x11d2, Data4: [8]byte{0x9e, 0xe3, 0, 0xc0, 0x4f, 0x79, 0x73, 0x96}}
	speechVoiceIID   = windows.GUID{Data1: 0x6c44df74, Data2: 0x72b9, Data3: 0x4992, Data4: [8]byte{0xa1, 0xec, 0xef, 0x99, 0x6e, 0x04, 0x22, 0xd4}}
	speechTokenClass = windows.GUID{Data1: 0xef411752, Data2: 0x3736, Data3: 0x4cb4, Data4: [8]byte{0x9c, 0x8c, 0x8e, 0xf4, 0xcc, 0xb5, 0x8e, 0xfe}}
	speechTokenIID   = windows.GUID{Data1: 0x14056589, Data2: 0xe16c, Data3: 0x11d2, Data4: [8]byte{0xbb, 0x90, 0, 0xc0, 0x4f, 0x8e, 0xe6, 0xc0}}
)

type speechObject struct{ vtable unsafe.Pointer }

//go:uintptrescapes
func (o *speechObject) call(slot int, args ...uintptr) int32 {
	entry := *(*uintptr)(unsafe.Add(o.vtable, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	params := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	hr, _, _ := syscall.SyscallN(entry, params...)
	return int32(hr)
}

func openSpeechObject(class, iid *windows.GUID) (*speechObject, error) {
	var result *speechObject
	hr, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(class)), 0, 1,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&result)))
	if int32(hr) < 0 || result == nil {
		return nil, fmt.Errorf("SAPI CoCreateInstance 0x%08x", uint32(hr))
	}
	return result, nil
}

func installedSpeechToken(locale string) string {
	name, err := windows.UTF16PtrFromString(locale)
	if err != nil {
		return ""
	}
	lcid, _, _ := localeNameToLCID.Call(uintptr(unsafe.Pointer(name)), 0)
	if lcid == 0 {
		return ""
	}
	bestID, bestRank := "", 0
	// Both token stores are already maintained by Windows. An exact match in
	// either store takes precedence over another region of the same language.
	for _, category := range []string{`SOFTWARE\Microsoft\Speech\Voices\Tokens`, `SOFTWARE\Microsoft\Speech_OneCore\Voices\Tokens`} {
		store, err := registry.OpenKey(registry.LOCAL_MACHINE, category, registry.READ)
		if err != nil {
			continue
		}
		tokens, _ := store.ReadSubKeyNames(-1)
		for _, id := range tokens {
			attributes, err := registry.OpenKey(store, id+`\Attributes`, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			language, _, _ := attributes.GetStringValue("Language")
			attributes.Close()
			if rank := speechLocaleRank(uint16(lcid), language); rank > bestRank {
				bestID, bestRank = `HKEY_LOCAL_MACHINE\`+category+`\`+id, rank
			}
		}
		store.Close()
	}
	return bestID
}

func speechLocaleRank(request uint16, supported string) int {
	rank := 0
	for _, code := range strings.Split(supported, ";") {
		id, err := strconv.ParseUint(strings.TrimSpace(code), 16, 16)
		if err != nil || id == 0 {
			continue
		}
		if uint16(id) == request {
			return 2
		}
		if uint16(id)&0x3ff == request&0x3ff {
			rank = 1
		}
	}
	return rank
}

// Must run in a COM apartment. Both objects use the IUnknown Release slot.
func openSpeechVoice(locale string) (*speechObject, error) {
	voiceID := installedSpeechToken(locale)
	if voiceID == "" {
		return nil, errNoSpeechVoice
	}
	name, err := windows.UTF16PtrFromString(voiceID)
	if err != nil {
		return nil, err
	}
	token, err := openSpeechObject(&speechTokenClass, &speechTokenIID)
	if err != nil {
		return nil, err
	}
	defer token.call(2)
	if hr := token.call(15, 0, uintptr(unsafe.Pointer(name)), 0); hr < 0 { // ISpObjectToken::SetId
		return nil, fmt.Errorf("SAPI SetId 0x%08x", uint32(hr))
	}
	voice, err := openSpeechObject(&speechVoiceClass, &speechVoiceIID)
	if err != nil {
		return nil, err
	}
	if hr := voice.call(18, uintptr(unsafe.Pointer(token))); hr < 0 { // ISpVoice::SetVoice
		voice.call(2)
		return nil, fmt.Errorf("SAPI SetVoice 0x%08x", uint32(hr))
	}
	return voice, nil
}

func speakTranslationText(ctx context.Context, text, locale string) error {
	line, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return err
	}
	speechOutput.Lock()
	defer speechOutput.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := coInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
	if int32(hr) < 0 {
		return fmt.Errorf("SAPI CoInitializeEx 0x%08x", uint32(hr))
	}
	defer coUninitialize.Call()
	voice, err := openSpeechVoice(locale)
	if err != nil {
		return err
	}
	defer voice.call(2)
	if err := ctx.Err(); err != nil {
		return err
	}
	const (
		speak         = 20
		waitUntilDone = 32
		speakAsync    = 1
		purgePending  = 2
		plainText     = 16
	)
	if hr := voice.call(speak, uintptr(unsafe.Pointer(line)), speakAsync|plainText, 0); hr < 0 {
		return fmt.Errorf("SAPI Speak 0x%08x", uint32(hr))
	}
	for {
		if err := ctx.Err(); err != nil {
			voice.call(speak, 0, speakAsync|purgePending, 0)
			return err
		}
		switch hr := voice.call(waitUntilDone, 50); {
		case hr == 0: // S_OK: output completed.
			return nil
		case hr < 0:
			return fmt.Errorf("SAPI WaitUntilDone 0x%08x", uint32(hr))
		}
	}
}
