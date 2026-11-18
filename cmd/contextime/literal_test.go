package main

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
)

func typeLiteralTest(d *draft, text string) {
	for _, char := range text {
		key := uint32(char)
		if char >= 'a' && char <= 'z' {
			key = uint32(char - 'a' + 'A')
		} else if char >= '!' && (char < '0' || char > '9') {
			key += symbolKeyBase
		}
		d.press(key, false)
	}
}

func TestLiteralFastPath(t *testing.T) {
	for _, value := range []string{"name@example.com", "www.example.com", "https://Example.com/v1?code=a+b"} {
		t.Run(value, func(t *testing.T) {
			var d draft
			typeLiteralTest(&d, value)
			if d.Chinese != value || d.Pinyin != "" || !d.autoLiteral {
				t.Fatalf("raw draft = %q, pinyin = %q, automatic = %v", d.Chinese, d.Pinyin, d.autoLiteral)
			}
			if result := cachedTranslation(d, translationRoute{language: "ja"}); result != value {
				t.Fatalf("raw result changed to %q", result)
			}
			d.press(keySpace, false)
			typeLiteralTest(&d, "nihao")
			d.press(keySpace, false)
			result, err := translateDraft(context.Background(), d, translationRoute{language: "en"})
			if err != nil || result != value+" Hello" {
				t.Fatalf("mixed result = %q, %v", result, err)
			}
		})
	}
}

func TestLiteralBackspaceAndTranslation(t *testing.T) {
	d := draft{Chinese: "你好"}
	d.press(keyLiteral, false)
	typeLiteralTest(&d, ` C:\Dev\main.go!`)
	snapshot := d
	d.press(keyBackspace, false)
	d.press(keyLiteral, false)
	typeLiteralTest(&d, "shijie")
	d.press(keyEnter, false)
	if parts := snapshot.translationParts(); len(parts) != 2 || parts[1].text != ` C:\Dev\main.go!` {
		t.Fatalf("editing a draft changed the translation snapshot: %v", parts)
	}
	result, err := translateDraft(context.Background(), d, translationRoute{language: "en"})
	if err != nil || result != `Hello C:\Dev\main.go World` {
		t.Fatalf("translated code draft = %q, %v", result, err)
	}
	d = snapshot
	d.press(keyLiteral, false)
	typeLiteralTest(&d, ".")
	result, err = translateDraft(context.Background(), d, translationRoute{language: "en"})
	if err != nil || result != `Hello C:\Dev\main.go!.` {
		t.Fatalf("ordinary punctuation outside the literal = %q, %v", result, err)
	}
}

func TestCandidatePagingKeepsNumericPunctuation(t *testing.T) {
	d := draft{Pinyin: "nihao", WindowsChoices: slices.Repeat([]choice{{"你好", 5}}, 12)}
	if key := d.pagingKey(symbolKeyBase+']', "brackets"); key != keyPageDown {
		t.Fatalf("bracket didn't page: %d", key)
	}
	d.Pinyin = "v12"
	if key := d.pagingKey(symbolKeyBase+'=', "minus-equals"); key != symbolKeyBase+'=' {
		t.Fatalf("numeric key was converted: %d", key)
	}
}

func TestTranslationErrorClassification(t *testing.T) {
	for err, want := range map[error]string{
		context.DeadlineExceeded:        translationTimeout,
		&net.DNSError{IsNotFound: true}: translationOffline,
		errors.New("service HTTP 503"):  translationFailed,
	} {
		if got := translationErrorStatus(err); got != want {
			t.Errorf("status of %v = %q, want %q", err, got, want)
		}
	}
}
