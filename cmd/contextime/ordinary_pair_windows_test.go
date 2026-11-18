package main

import "testing"

func ordinaryPairTargetIndex(t *testing.T, code string) int {
	t.Helper()
	for index, language := range targetLanguages {
		if language.code == code {
			return index
		}
	}
	t.Fatalf("missing language %q", code)
	return -1
}

func TestOrdinaryModeShiftUsesSelectedLanguagePair(t *testing.T) {
	for _, pair := range [][2]string{{"zh-Hans", "ja"}, {"en", "ja"}, {"en", "zh-Hans"}} {
		for _, initial := range pair {
			t.Run(pair[0]+"-"+pair[1]+"/"+initial, func(t *testing.T) {
				mode := inputMode{
					sourceLanguage: pair[0], language: ordinaryPairTargetIndex(t, pair[1]),
					ordinaryLanguage: initial, chinese: true,
					direct: initial != "zh-Hans" && initial != "ja",
				}
				if got := mode.ordinaryCode(); got != initial {
					t.Fatalf("selected ordinary language = %s, want %s", got, initial)
				}
				other := pair[0]
				if other == initial {
					other = pair[1]
				}
				if !mode.toggleOrdinaryLanguage() {
					t.Fatal("ordinary Shift did not change language")
				}
				if !mode.chinese || mode.ordinaryCode() != other || mode.compositionCode() != other {
					t.Fatalf("Shift changed mode or selected wrong pair language: %+v", mode)
				}
				if wantDirect := other != "zh-Hans" && other != "ja"; mode.direct != wantDirect {
					t.Fatalf("ordinary %s direct=%v, want %v", other, mode.direct, wantDirect)
				}
				if mode.sourceCode() != pair[0] || targetLanguages[mode.language].code != pair[1] {
					t.Fatal("ordinary Shift changed translation direction")
				}
				if !mode.toggleOrdinaryLanguage() || mode.ordinaryCode() != initial || !mode.chinese {
					t.Fatalf("second Shift did not restore ordinary language: %+v", mode)
				}
			})
		}
	}
}

func TestTranslationModeShiftKeepsPairAndRememberedOrdinaryLanguage(t *testing.T) {
	for _, pair := range [][2]string{{"zh-Hans", "ja"}, {"en", "ja"}, {"en", "zh-Hans"}} {
		mode := inputMode{
			sourceLanguage: pair[0], language: ordinaryPairTargetIndex(t, pair[1]),
			ordinaryLanguage: pair[1], chinese: false, direct: false,
		}
		before := mode
		if mode.toggleOrdinaryLanguage() || mode != before {
			t.Fatalf("translation-mode Shift changed input mode: before=%+v after=%+v", before, mode)
		}
		if got := mode.compositionCode(); got != pair[0] {
			t.Fatalf("translation composition uses %s instead of source %s", got, pair[0])
		}
	}
}

func TestTranslationToggleRemembersEachOrdinaryLanguage(t *testing.T) {
	for _, pair := range [][2]string{{"zh-Hans", "ja"}, {"en", "ja"}, {"en", "zh-Hans"}} {
		for _, selected := range pair {
			t.Run(pair[0]+"-"+pair[1]+"/"+selected, func(t *testing.T) {
				mode := inputMode{
					sourceLanguage: pair[0], language: ordinaryPairTargetIndex(t, pair[1]),
					ordinaryLanguage: selected, chinese: true,
				}
				mode.syncOrdinaryLanguage()
				initial := mode
				mode.toggleTranslation()
				if mode.chinese || mode.direct || mode.ordinaryCode() != selected || mode.compositionCode() != pair[0] {
					t.Fatalf("switching to translation lost ordinary language or source: %+v", mode)
				}
				if mode.toggleOrdinaryLanguage() {
					t.Fatal("Shift changed ordinary preference while translating")
				}
				mode.toggleTranslation()
				if mode != initial {
					t.Fatalf("translation round trip changed ordinary language: before=%+v after=%+v", initial, mode)
				}
			})
		}
	}
}

func TestOrdinaryLanguageFallbackStaysWithinPair(t *testing.T) {
	cases := []struct {
		source, target, selected string
		legacyChinese            bool
		want                     string
	}{
		{"zh-Hans", "ja", "", true, "zh-Hans"},
		{"zh-Hans", "ja", "", false, "ja"},
		{"zh-Hans", "ja", "en", false, "ja"},
		{"en", "ja", "", true, "en"},
		{"en", "ja", "zh-Hans", true, "en"},
		{"en", "zh-Hans", "", true, "zh-Hans"},
		{"en", "zh-Hans", "", false, "en"},
		{"ja", "zh-Hans", "fr", false, "zh-Hans"},
	}
	for _, test := range cases {
		t.Run(test.source+"-"+test.target+"/"+test.selected, func(t *testing.T) {
			mode := inputMode{
				sourceLanguage: test.source, language: ordinaryPairTargetIndex(t, test.target),
				ordinaryLanguage: test.selected, ordinaryUsesChinese: test.legacyChinese,
				chinese: true,
			}
			if got := mode.ordinaryCode(); got != test.want {
				t.Fatalf("ordinary fallback = %s, want %s", got, test.want)
			}
			settings := defaultIMEConfig()
			settings.SourceLanguage, settings.Language = test.source, mode.language
			settings.OrdinaryLanguage, settings.OrdinaryUsesChinese = test.selected, test.legacyChinese
			if got := settings.effectiveOrdinaryLanguage(); got != test.want {
				t.Fatalf("settings ordinary fallback = %s, want %s", got, test.want)
			}
		})
	}
}
