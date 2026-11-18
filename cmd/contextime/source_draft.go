package main

import (
	"strings"
	"unicode"
)

// Source text is translatable content, unlike Literal ranges which are copied
// unchanged into the result. Keep it out of the Chinese pinyin candidate path.
func (d draft) handlesSource(key uint32) bool {
	if key == keyPasteSource || key >= 'A' && key <= 'Z' || key >= '0' && key <= '9' {
		return true
	}
	if sourceSymbol(key) != 0 {
		return true
	}
	switch key {
	case keyEnter, keyEscape, keyBackspace, keySpace, keyLeft, keyRight, keyDelete,
		editKeyBase + keyLeft, editKeyBase + keyRight:
		return d.active()
	}
	return false
}

func sourceSymbol(key uint32) rune {
	if key <= symbolKeyBase || key > symbolKeyBase+0xffff {
		return 0
	}
	r := rune(key - symbolKeyBase)
	if unicode.IsPrint(r) && (r < 0xd800 || r > 0xdfff) {
		return r
	}
	return 0
}

func (d *draft) insertSource(text string) {
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, text)
	if text == "" {
		return
	}
	content, inserted := []rune(d.Chinese), []rune(text)
	if len(content)+len(inserted) > 480 {
		d.CommitStatus = "原文过长，请分段输入（最多 480 字）"
		return
	}
	cursor := len(content) - min(d.SourceTail, len(content))
	d.Chinese = string(content[:cursor]) + text + string(content[cursor:])
	d.English, d.TranslationStatus, d.CommitStatus = "", "", ""
}

func (d *draft) pressSource(key uint32, repeated bool) string {
	if !d.handlesSource(key) || key == keyEnter && repeated {
		return ""
	}
	d.CommitStatus = ""
	content := []rune(d.Chinese)
	cursor := len(content) - min(d.SourceTail, len(content))
	changed := false
	switch {
	case key >= 'A' && key <= 'Z':
		d.insertSource(string(rune(key + 'a' - 'A')))
	case key >= '0' && key <= '9':
		d.insertSource(string(rune(key)))
	case sourceSymbol(key) != 0:
		d.insertSource(string(sourceSymbol(key)))
	case key == keySpace:
		d.insertSource(" ")
	case key == keyLeft || key == editKeyBase+keyLeft:
		d.SourceTail = min(len(content), d.SourceTail+1)
	case key == keyRight || key == editKeyBase+keyRight:
		d.SourceTail = max(0, d.SourceTail-1)
	case key == keyBackspace && cursor > 0:
		d.Chinese = string(content[:cursor-1]) + string(content[cursor:])
		changed = true
	case key == keyDelete && cursor < len(content):
		d.Chinese = string(content[:cursor]) + string(content[cursor+1:])
		d.SourceTail--
		changed = true
	case key == keyEscape:
		*d = d.afterCommit()
	case key == keyEnter && d.English != "":
		return d.English
	}
	if changed {
		d.English, d.TranslationStatus = "", ""
	}
	return ""
}
