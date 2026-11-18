package main

import "testing"

func TestInlineEnglishStaysSelectableAndPreservesTranslation(t *testing.T) {
	d := draft{LanguageCode: "zh-Hans"}
	for _, r := range "shift" {
		d.press(uint32(r-'a'+'A'), false)
	}
	if choices := d.candidates(); len(choices) < 2 || !isInlineEnglishCandidate(d.Pinyin, choices[0]) {
		t.Fatalf("English word was not first alongside Chinese: %+v", choices)
	}
	d.press(keySpace, false)
	if d.Chinese != "shift" || d.Pinyin != "" || d.Literal || d.LanguageCode != "zh-Hans" {
		t.Fatalf("English selection changed Chinese input flow: %+v", d)
	}
	if got := cachedTranslation(d, translationRoute{language: "ja"}); got != "shift" {
		t.Fatalf("inline English was translated: %q", got)
	}
	d.press(keyBackspace, false)
	if d.Chinese != "" || d.Pinyin != "shift" || len(d.literals) != 0 {
		t.Fatalf("undo left stale English literal ranges: %+v", d)
	}
	d.press(keySpace, false)
	for _, r := range "nihao" {
		d.press(uint32(r-'a'+'A'), false)
	}
	choices := d.candidates()
	for _, c := range choices {
		if c.text == "你好" {
			d.selectWord(c)
			break
		}
	}
	if d.Pinyin != "" {
		t.Fatal("Chinese was no longer selectable after inline English")
	}
	if got := cachedTranslation(d, translationRoute{language: "en"}); got != "shift Hello" {
		t.Fatalf("mixed English/Chinese translation = %q", got)
	}
}

func TestUppercaseInlineEnglishBuffersWholeWord(t *testing.T) {
	d := draft{LanguageCode: "zh-Hans"}
	if !d.handles(symbolKeyBase + 'W') {
		t.Fatal("uppercase initial bypassed Chinese candidate buffer")
	}
	d.press(symbolKeyBase+'W', false)
	for _, r := range "indows" {
		d.press(uint32(r-'a'+'A'), false)
	}
	if d.Pinyin != "Windows" || d.Chinese != "" {
		t.Fatalf("uppercase English was split: %+v", d)
	}
	choices := d.candidates()
	if len(choices) != 1 || choices[0].text != "Windows" {
		t.Fatalf("uppercase candidates = %+v", choices)
	}
	d.press(keySpace, false)
	if d.Chinese != "Windows" || d.Pinyin != "" || d.Literal {
		t.Fatalf("uppercase English did not finish as a word: %+v", d)
	}
}
