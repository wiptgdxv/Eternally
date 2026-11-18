package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyPunctuationShortcutDoesNotResetPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := `{"sourceLanguage":"en","language":1,"ordinaryLanguage":"ja","appearance":"dark","hotkeys":{"punctuation":131262}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	got := readIMEConfig(path)
	if got.SourceLanguage != "en" || got.Language != 1 || got.OrdinaryLanguage != "ja" || got.Appearance != "dark" {
		t.Fatalf("retired punctuation shortcut reset preferences: %+v", got)
	}
	data, err := json.Marshal(got)
	if err != nil || strings.Contains(string(data), `"punctuation"`) {
		t.Fatalf("saved settings still expose punctuation shortcut: %s, %v", data, err)
	}
}

func TestSettingsReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := defaultIMEConfig()
	if err := os.WriteFile(path, []byte(`{"language":2,"ordinaryUsesChinese":true,"fuzzyPinyin":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	want.Language, want.OrdinaryUsesChinese, want.FuzzyPinyin = 2, true, true
	if got := readIMEConfig(path); got != want {
		t.Fatalf("partial settings = %+v, want %+v", got, want)
	}
	want.CandidateFontSize, want.Appearance, want.Paging = 18, "dark", "brackets"
	want.Hotkeys = shortcutSettings{
		ToggleTranslation: shortcutComma,
		NextLanguage:      keyToggleTranslation,
		CopyTranslation:   editKeyBase + 0x79,
	}
	want.QuickLanguages = 1<<englishTarget | 1<<14
	want.TranslationEngine = engineOffline
	want.OfflineOnlineFallback = true
	if err := writeLocalJSON(path, want); err != nil {
		t.Fatal(err)
	}
	if got := readIMEConfig(path); got != want {
		t.Fatalf("saved settings = %+v, want %+v", got, want)
	}
	want.Hotkeys.NextLanguage = shortcutComma
	if err := validateIMEConfig(want); err == nil {
		t.Fatal("accepted conflicting shortcuts")
	}
	want.Hotkeys.NextLanguage = keyToggleTranslation
	want.Hotkeys.CopyTranslation = want.Hotkeys.NextLanguage
	if err := validateIMEConfig(want); err == nil {
		t.Fatal("accepted a conflicting copy shortcut")
	}
}

func TestQuickTranslationLanguages(t *testing.T) {
	settings := defaultIMEConfig()
	if targetLanguages[21].code != "zh-Hans" || nextTargetLanguage(7, settings) != englishTarget {
		t.Fatal("new language options altered the default shortcut cycle")
	}
	const vietnamese = 14
	if targetLanguages[vietnamese].code != "vi" {
		t.Fatal("unexpected Vietnamese target index")
	}
	settings.QuickLanguages = 1<<englishTarget | 1<<vietnamese
	if nextTargetLanguage(englishTarget, settings) != vietnamese ||
		nextTargetLanguage(vietnamese, settings) != englishTarget ||
		nextTargetLanguage(2, settings) != vietnamese {
		t.Fatal("favorite languages did not cycle in catalog order")
	}
	settings.Language = vietnamese
	settings.TranslationEngine = engineOffline
	if settings.effectiveLanguage() != englishTarget || nextTargetLanguage(vietnamese, settings) != englishTarget {
		t.Fatal("offline translation offered a target other than English")
	}
	settings.TranslationEngine = engineOnline
	if settings.effectiveLanguage() != vietnamese {
		t.Fatal("offline mode changed the saved online target")
	}
	settings.QuickLanguages = 0
	if err := validateIMEConfig(settings); err != nil || nextTargetLanguage(vietnamese, settings) != vietnamese {
		t.Fatalf("disabled language cycling changed the current language: %v", err)
	}
}

func TestPersonalDictionaryEditing(t *testing.T) {
	const profileVariable = "CONTEXTIME_DICTIONARY_TEST_PROFILE"
	profile := os.Getenv(profileVariable)
	if profile == "" {
		profile = t.TempDir()
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
		cmd.Env = append(cmd.Environ(), "APPDATA="+profile, profileVariable+"="+profile)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, output)
		}
		return
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	backup := filepath.Join(profile, "backup.json")
	check(os.WriteFile(backup, []byte(`{"nh":[{"text":"你好","count":1},{"text":"年会","count":3}],"sh":["上海"]}`), 0600))
	check(importPersonalWords(backup))
	check(importPersonalWords(backup))
	words := personalWords("nh")
	if len(words) != 2 || words[0].Text != "年会" || words[0].Count != 3 {
		t.Fatalf("unexpected merged words: %+v", words)
	}
	check(editPersonalWord(words[1], false))
	if choices := preferLearned("nh", nil); len(choices) != 2 || choices[0].text != "你好" {
		t.Fatalf("unexpected rank after pin: %+v", choices)
	}
	check(exportPersonalWords(backup))
	check(editPersonalWord(words[0], true))
	if words := personalWords("NH"); len(words) != 1 || !words[0].Pinned {
		t.Fatalf("unexpected words after delete: %+v", words)
	}
	check(editPersonalWord(words[1], true))
	check(importPersonalWords(backup))
	if words := personalWords("nh"); len(words) != 2 || !words[0].Pinned {
		t.Fatalf("unexpected words after restoring backup: %+v", words)
	}
	check(editPersonalWord(words[1], false))
	if choices := preferLearned("nh", nil); len(choices) != 2 || choices[0].text != "年会" {
		t.Fatalf("unexpected rank after unpin: %+v", choices)
	}
	check(editPhrase(" ZZ ", "", "自定义短语"))
	if choices := customPhraseChoices("zz"); len(choices) != 1 || choices[0].text != "自定义短语" {
		t.Fatalf("unexpected phrase suggestions: %+v", choices)
	}
	check(editPhrase("zz", "自定义短语", "新的值"))
	check(editPhrase("zz", "新的值", ""))
	if choices := customPhraseChoices("zz"); len(choices) != 0 {
		t.Fatalf("unexpected phrase suggestions after deletion: %+v", choices)
	}
}
