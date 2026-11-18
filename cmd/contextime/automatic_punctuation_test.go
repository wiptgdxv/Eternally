package main

import (
	"reflect"
	"testing"
)

func TestAutomaticPunctuationFollowsCompositionLanguage(t *testing.T) {
	cases := []struct {
		language string
		input    string
		want     string
	}{
		{"", `,.!?:;()[]<>`, `，。！？：；（）【】《》`},
		{"zh-Hans", `,.!?:;()[]<>`, `，。！？：；（）【】《》`},
		{"ja", `,.!?[]`, `、。！？「」`},
		{"en", `,.!?:;()[]<>"'`, `,.!?:;()[]<>"'`},
		{"fr", `,.!?:;()[]<>"'`, `,.!?:;()[]<>"'`},
		{"de", `,.!?:;()[]<>"'`, `,.!?:;()[]<>"'`},
	}
	for _, test := range cases {
		t.Run(test.language, func(t *testing.T) {
			d := draft{LanguageCode: test.language, Chinese: "text"}
			var got []rune
			for _, symbol := range test.input {
				got = append(got, d.localizedSymbol(symbol))
			}
			if string(got) != test.want {
				t.Fatalf("%s punctuation = %q, want %q", test.language, string(got), test.want)
			}
		})
	}
}

func TestAutomaticQuotesRemainPairedAcrossCommits(t *testing.T) {
	for _, test := range []struct{ language, doubleOpen, singleOpen, singleClose, doubleClose string }{
		{"zh-Hans", "“", "‘", "’", "”"},
		{"ja", "「", "『", "』", "」"},
	} {
		t.Run(test.language, func(t *testing.T) {
			d := draft{LanguageCode: test.language, Chinese: "outer"}
			appendQuote := func(symbol rune, want string) {
				t.Helper()
				prefix := d.Chinese
				d.press(symbolKeyBase+uint32(symbol), false)
				if d.Chinese != prefix+want {
					t.Fatalf("quote %q produced %q, want %q", symbol, d.Chinese, prefix+want)
				}
			}
			appendQuote('"', test.doubleOpen)
			d = d.afterCommit()
			d.Chinese = "inner"
			appendQuote('\'', test.singleOpen)
			d = d.afterCommit()
			d.Chinese = "nested"
			appendQuote('\'', test.singleClose)
			appendQuote('"', test.doubleClose)
			d = d.afterCommit()
			if d.LanguageCode != test.language || d.localizedSymbol('"') != []rune(test.doubleOpen)[0] || d.localizedSymbol('\'') != []rune(test.singleOpen)[0] {
				t.Fatalf("next sentence did not restart balanced %s quote pairs: %+v", test.language, d)
			}
		})
	}
}

func TestAutomaticPunctuationPreservesDecimalsAndLiteralCode(t *testing.T) {
	for _, language := range []string{"zh-Hans", "ja"} {
		t.Run(language, func(t *testing.T) {
			d := draft{LanguageCode: language, Chinese: "2"}
			d.press(symbolKeyBase+'.', false)
			d.press('5', false)
			if d.Chinese != "2.5" {
				t.Fatalf("decimal became %q", d.Chinese)
			}
			d.Chinese += " units"
			d.press(symbolKeyBase+'.', false)
			if d.Chinese != "2.5 units。" {
				t.Fatalf("sentence punctuation after decimal = %q", d.Chinese)
			}
			code := `https://Example.com/v2.5?x=[1]!`
			d = draft{LanguageCode: language, Literal: true}
			typeLiteralTest(&d, code)
			if d.Chinese != code {
				t.Fatalf("literal URL/code punctuation changed: %q", d.Chinese)
			}
			if len(d.literals) == 0 {
				t.Fatal("literal URL/code stopped preserving its source range")
			}
		})
	}
}

func TestAutomaticPunctuationUsesSourceAndSelectedOrdinaryLanguage(t *testing.T) {
	for _, test := range []struct {
		source, target, ordinary   string
		sourceComma, ordinaryComma rune
	}{
		{"zh-Hans", "ja", "ja", '，', '、'},
		{"en", "ja", "ja", ',', '、'},
		{"ja", "en", "en", '、', ','},
	} {
		t.Run(test.source+"-"+test.target, func(t *testing.T) {
			mode := inputMode{sourceLanguage: test.source, language: ordinaryPairTargetIndex(t, test.target), ordinaryLanguage: test.ordinary}
			punctuation := func() rune {
				return (draft{LanguageCode: mode.compositionCode(), Chinese: "text"}).localizedSymbol(',')
			}
			if got := punctuation(); got != test.sourceComma {
				t.Fatalf("translation punctuation used target language: %q", got)
			}
			mode.toggleTranslation()
			if got := punctuation(); got != test.ordinaryComma {
				t.Fatalf("ordinary punctuation ignored selected language %s: %q", test.ordinary, got)
			}
			if test.ordinary == "en" && !mode.direct {
				t.Fatal("automatic punctuation interfered with ordinary English direct input")
			}
			mode.toggleTranslation()
			if got := punctuation(); got != test.sourceComma {
				t.Fatalf("returning to translation retained ordinary punctuation: %q", got)
			}
		})
	}
}

func TestControlPeriodDoesNotTogglePunctuation(t *testing.T) {
	for _, language := range []string{"zh-Hans", "ja", "en"} {
		d := draft{LanguageCode: language, Chinese: "draft", English: "translation"}
		before := d
		if d.handles(keyControlPeriod) || d.handlesSource(keyControlPeriod) {
			t.Fatalf("%s draft still consumes Ctrl+. as a punctuation action", language)
		}
		d.press(keyControlPeriod, false)
		d.pressSource(keyControlPeriod, false)
		if !reflect.DeepEqual(d, before) {
			t.Fatalf("Ctrl+. changed %s draft: before=%+v after=%+v", language, before, d)
		}
	}
}
