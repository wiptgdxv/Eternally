package main

import (
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	enter     = 0x0d
	escape    = 0x1b
	backspace = 0x08
	space     = 0x20
)

type service struct {
	keys, source             uintptr
	client, cookie           uintptr
	context                  uintptr
	phonetic, draft, english string
	window                   uintptr
}

func activate(this, manager, client uintptr) uintptr {
	o := lookup(this)
	s := o.service
	s.client = client
	s.keys = getInterface(manager, &keyManager)
	if s.keys == 0 {
		return noInterface
	}
	s.source = getInterface(manager, &sourceIID)
	if s.source == 0 {
		deactivate(this)
		return noInterface
	}
	var cookie uint32
	hr := method(s.source, 3, ptr(&threadSink), o.faces[threadSink], ptr(&cookie))
	if isFailure(hr) {
		deactivate(this)
		return hr
	}
	s.cookie = uintptr(cookie) + 1
	hr = method(s.keys, 3, client, o.faces[keySink], 1)
	if isFailure(hr) {
		deactivate(this)
	}
	return hr
}

func deactivate(this uintptr) uintptr {
	s := lookup(this).service
	s.reset()
	if s.keys != 0 {
		method(s.keys, 4, s.client)
		method(s.keys, 2)
		s.keys = 0
	}
	if s.source != 0 {
		if s.cookie != 0 {
			method(s.source, 4, s.cookie-1)
		}
		method(s.source, 2)
		s.source, s.cookie = 0, 0
	}
	if s.window != 0 {
		destroyWindow.Call(s.window)
		s.window = 0
	}
	return ok
}

func (s *service) reset() {
	s.phonetic, s.draft, s.english = "", "", ""
	if s.context != 0 {
		method(s.context, 2)
		s.context = 0
	}
	if s.window != 0 {
		showWindow.Call(s.window, 0)
	}
}

func onForeground(this, active uintptr) uintptr {
	if active == 0 {
		lookup(this).service.reset()
	}
	return ok
}
func ignoreDocument(_, _ uintptr) uintptr         { return ok }
func focusDocument(this, _, _ uintptr) uintptr    { lookup(this).service.reset(); return ok }
func focusContextLost(this, _ uintptr) uintptr    { lookup(this).service.reset(); return ok }
func preservedKey(_, _, _, eaten uintptr) uintptr { return eat(eaten, false) }
func eat(out uintptr, value bool) uintptr {
	if out == 0 {
		return badPointer
	}
	p := (*int32)(unsafe.Pointer(out))
	*p = 0
	if value {
		*p = 1
	}
	return ok
}
func testUp(_, _, _, _, eaten uintptr) uintptr { return eat(eaten, false) }
func keyUp(_, _, _, _, eaten uintptr) uintptr  { return eat(eaten, false) }

func (s *service) wants(ctx, key uintptr) bool {
	if ctx == 0 || shortcutHeld() {
		return false
	}
	if key >= 'A' && key <= 'Z' {
		return true
	}
	if s.context != ctx {
		return false
	}
	switch key {
	case enter, space, backspace, escape:
		return s.phonetic != "" || s.draft != ""
	}
	return false
}

func testDown(this, ctx, key, _, eaten uintptr) uintptr {
	return eat(eaten, lookup(this).service.wants(ctx, key))
}

func keyDown(this, ctx, key, flags, eaten uintptr) uintptr {
	s := lookup(this).service
	if !s.wants(ctx, key) {
		s.reset()
		return eat(eaten, false)
	}
	if s.context != ctx {
		s.reset()
		method(ctx, 1)
		s.context = ctx
	}
	// Holding Enter must never both select a word and submit the English.
	if key == enter && flags&(1<<30) != 0 {
		return eat(eaten, true)
	}
	switch {
	case key >= 'A' && key <= 'Z':
		if len(s.phonetic) < 20 {
			s.phonetic += string(rune(key + 'a' - 'A'))
		}
	case key == enter || key == space:
		if s.phonetic != "" {
			if word := candidate(s.phonetic); word != "" && len([]rune(s.draft)) < 40 {
				s.draft += word
				s.english = fixedTranslation(s.draft)
				s.phonetic = ""
			}
		} else if key == enter && s.english != "" && s.commit(ctx) {
			s.reset()
		}
	case key == escape:
		s.reset()
	case key == backspace:
		if n := len(s.phonetic); n > 0 {
			s.phonetic = s.phonetic[:n-1]
		} else if text := []rune(s.draft); len(text) > 0 {
			s.draft = string(text[:len(text)-1])
			s.english = fixedTranslation(s.draft)
		}
	}
	s.draw()
	return eat(eaten, true)
}

func candidate(input string) string {
	switch input {
	case "nihao":
		return "你好"
	case "shijie":
		return "世界"
	}
	return ""
}

var sampleWords = strings.NewReplacer("你好", "Hello ", "世界", "world ")

func fixedTranslation(draft string) string {
	text := strings.TrimSpace(sampleWords.Replace(draft))
	if text == "world" {
		return "World"
	}
	return text
}

type editRequest struct {
	context uintptr
	text    []uint16
}

func (s *service) commit(ctx uintptr) bool {
	edit := &editRequest{ctx, utf16.Encode([]rune(s.english))}
	o := create([]windows.GUID{editIID}, []uintptr{editTable}, nil, edit)
	if o == nil {
		return false
	}
	defer release(o.primary)
	var result int32 = -1
	const synchronousReadWrite = 0x1 | 0x6
	hr := method(ctx, 3, s.client, o.primary, synchronousReadWrite, ptr(&result))
	return !isFailure(hr) && result == 0
}

func doEdit(this, cookie uintptr) uintptr {
	edit := lookup(this).edit
	insert := getInterface(edit.context, &insertIID)
	if insert == 0 {
		return noInterface
	}
	defer method(insert, 2)
	return method(insert, 3, cookie, 1, ptr(&edit.text[0]), len(edit.text), 0)
}
