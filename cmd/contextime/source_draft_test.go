package main

import "testing"

func TestEnglishSourceEditingAndCommit(t *testing.T) {
	var d draft
	for _, key := range []uint32{symbolKeyBase + 'H', 'E', 'L', 'L', 'O', keySpace, 'W', 'O', 'R', 'L', 'D', symbolKeyBase + '!'} {
		if !d.handlesSource(key) {
			t.Fatalf("source key %d was passed through", key)
		}
		d.pressSource(key, false)
	}
	if d.Chinese != "Hello world!" || d.Pinyin != "" || d.Literal || len(d.literals) != 0 {
		t.Fatalf("source entered pinyin or literal path: %+v", d)
	}
	parts := d.translationParts()
	if len(parts) != 1 || !parts[0].translate || parts[0].text != "Hello world!" {
		t.Fatalf("English source bypassed translation: %+v", parts)
	}
	d.pressSource(keyLeft, false)
	d.pressSource(keyBackspace, false)
	d.pressSource('D', false)
	if d.Chinese != "Hello world!" {
		t.Fatalf("source cursor edit = %q", d.Chinese)
	}
	d.English = "你好，世界！"
	if got := d.pressSource(keyEnter, true); got != "" {
		t.Fatalf("repeated enter committed %q", got)
	}
	if got := d.pressSource(keyEnter, false); got != "你好，世界！" {
		t.Fatalf("enter = %q", got)
	}
	if d.Chinese != "Hello world!" {
		t.Fatal("draft was discarded before commit acknowledgement")
	}
}

func TestSourcePasteUnicodeAndBounds(t *testing.T) {
	var d draft
	d.insertSource("こんにちは\r\n世界🙂")
	d.pressSource(keyLeft, false)
	d.pressSource(keyBackspace, false)
	if d.Chinese != "こんにちは 世🙂" {
		t.Fatalf("Unicode deletion = %q", d.Chinese)
	}
	d.pressSource(keyDelete, false)
	if d.Chinese != "こんにちは 世" {
		t.Fatalf("Unicode forward deletion = %q", d.Chinese)
	}
	d.insertSource("！")
	if d.Chinese != "こんにちは 世！" {
		t.Fatalf("Unicode insertion = %q", d.Chinese)
	}
	d.English = "你好世界"
	d.pressSource(keyEscape, false)
	if d.active() || d.English != "" || d.SourceTail != 0 {
		t.Fatalf("escape left stale source state: %+v", d)
	}
	d.insertSource(string(make([]rune, 481)))
	if d.active() {
		t.Fatal("control characters in clipboard became source")
	}
}

func TestTranslationCacheSeparatesSourceLanguages(t *testing.T) {
	var memory translationMemory
	english := translationRoute{sourceLanguage: "en", language: "zh-Hans"}
	french := translationRoute{sourceLanguage: "fr", language: "zh-Hans"}
	memory.set(english.cacheKey("chat"), "聊天")
	memory.set(french.cacheKey("chat"), "猫")
	if memory.get(english.cacheKey("chat")) != "聊天" || memory.get(french.cacheKey("chat")) != "猫" {
		t.Fatal("different source languages shared cached translations")
	}
}
