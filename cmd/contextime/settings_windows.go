package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"lo-windows/internal/translation"
)

type shortcutSettings struct {
	ToggleTranslation uint32 `json:"toggleTranslation"`
	NextLanguage      uint32 `json:"nextLanguage"`
	CopyTranslation   uint32 `json:"copyTranslation,omitempty"`
}

type imeSettings struct {
	Language              int              `json:"language"`
	SourceLanguage        string           `json:"sourceLanguage"`
	OrdinaryUsesChinese   bool             `json:"ordinaryUsesChinese"`
	OrdinaryLanguage      string           `json:"ordinaryLanguage,omitempty"`
	CandidateFontSize     int              `json:"candidateFontSize"`
	Appearance            string           `json:"appearance"`
	Paging                string           `json:"paging"`
	FuzzyPinyin           bool             `json:"fuzzyPinyin"`
	Hotkeys               shortcutSettings `json:"hotkeys"`
	QuickLanguages        uint32           `json:"quickLanguages"`
	TranslationEngine     string           `json:"translationEngine"`
	OfflineOnlineFallback bool             `json:"offlineOnlineFallback,omitempty"`
}

const (
	shortcutComma               = editKeyBase + 0xbc
	engineOnline                = "online"
	engineOffline               = "offline"
	englishTarget               = 0
	legacyQuickLanguages uint32 = (1 << 8) - 1
)

func defaultIMEConfig() imeSettings {
	return imeSettings{
		SourceLanguage:    "zh-Hans",
		CandidateFontSize: 14,
		Appearance:        "system",
		Paging:            "page",
		Hotkeys: shortcutSettings{
			ToggleTranslation: keyToggleTranslation,
			NextLanguage:      keyNextLanguage,
		},
		QuickLanguages:    legacyQuickLanguages,
		TranslationEngine: engineOnline,
	}
}

func (s imeSettings) effectiveLanguage() int {
	if s.TranslationEngine == engineOffline {
		return englishTarget
	}
	return s.Language
}

func (s imeSettings) effectiveOrdinaryLanguage() string {
	source := s.SourceLanguage
	if s.TranslationEngine == engineOffline {
		source = "zh-Hans"
	}
	return ordinaryLanguageChoice(source, targetLanguages[s.effectiveLanguage()].code, s.OrdinaryLanguage, s.OrdinaryUsesChinese)
}

func nextTargetLanguage(current int, s imeSettings) int {
	if s.TranslationEngine == engineOffline {
		return englishTarget
	}
	for step := 1; step <= len(targetLanguages); step++ {
		candidate := (current + step) % len(targetLanguages)
		if s.QuickLanguages&(1<<candidate) != 0 {
			return candidate
		}
	}
	return current
}

func localConfigPath(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ContextIME", name), nil
}

var settingsLock sync.Mutex
var storedSettings = sync.OnceValue(func() *imeSettings {
	s := defaultIMEConfig()
	path, err := localConfigPath("settings.json")
	if err == nil {
		s = readIMEConfig(path)
	}
	return &s
})

func readIMEConfig(path string) imeSettings {
	initial := defaultIMEConfig()
	var next = initial
	info, err := os.Stat(path)
	if err != nil || info.Size() > 32*1024 {
		return initial
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &next) != nil {
		return initial
	}
	if err := validateIMEConfig(next); err != nil {
		return initial
	}
	return next
}

func settingsSnapshot() imeSettings {
	settingsLock.Lock()
	defer settingsLock.Unlock()
	return *storedSettings()
}

func fuzzyPinyinEnabled() bool { return settingsSnapshot().FuzzyPinyin }

func validateIMEConfig(s imeSettings) error {
	validSource := s.SourceLanguage == "auto" || s.SourceLanguage == ""
	for _, language := range targetLanguages {
		validSource = validSource || language.code == s.SourceLanguage
	}
	if !validSource {
		return errors.New("输入语言选项无效")
	}
	validOrdinary := s.OrdinaryLanguage == ""
	for _, language := range targetLanguages {
		validOrdinary = validOrdinary || language.code == s.OrdinaryLanguage
	}
	if !validOrdinary {
		return errors.New("普通模式语言选项无效")
	}
	if s.Language < 0 || s.Language >= len(targetLanguages) || s.CandidateFontSize < 12 || s.CandidateFontSize > 20 {
		return errors.New("设置值超出范围")
	}
	if s.Appearance != "system" && s.Appearance != "light" && s.Appearance != "dark" {
		return errors.New("外观选项无效")
	}
	if s.Paging != "page" && s.Paging != "brackets" && s.Paging != "minus-equals" {
		return errors.New("翻组键选项无效")
	}
	if s.TranslationEngine != engineOnline && s.TranslationEngine != engineOffline {
		return errors.New("翻译引擎选项无效")
	}
	if s.QuickLanguages >= 1<<len(targetLanguages) {
		return errors.New("快捷切换语言选项无效")
	}
	keys := s.Hotkeys
	bindings := [...]uint32{keys.ToggleTranslation, keys.NextLanguage, keys.CopyTranslation}
	for i, binding := range bindings {
		if i == len(bindings)-1 && binding == 0 {
			continue
		}
		if !configurableShortcut(binding) {
			return errors.New("快捷键选项无效")
		}
		for _, previous := range bindings[:i] {
			if binding == previous {
				return errors.New("不同操作需要使用各自的快捷键")
			}
		}
	}
	return nil
}

func configurableShortcut(key uint32) bool {
	return key == shortcutComma || key == keyControlPeriod || key >= editKeyBase+0x75 && key <= editKeyBase+0x7b
}

// configuredShortcut maps a physical key code from the TSF transport to its action.
func configuredShortcut(key uint32) uint32 {
	if !configurableShortcut(key) {
		return key
	}
	h := settingsSnapshot().Hotkeys
	switch key {
	case h.ToggleTranslation:
		return keyToggleTranslation
	case h.NextLanguage:
		return keyNextLanguage
	case h.CopyTranslation:
		return keyCopyTranslation
	}
	if key == shortcutComma && h.ToggleTranslation == keyToggleTranslation {
		return keyToggleTranslation
	}
	return 0
}

func applyInputSettings() {
	s := settingsSnapshot()
	for {
		previous := currentInputMode.Load()
		next := inputMode{}
		if previous != nil {
			next = *previous
		}
		next.language = s.effectiveLanguage()
		next.sourceLanguage = s.SourceLanguage
		next.translationEngine = s.TranslationEngine
		next.offlineOnlineFallback = s.OfflineOnlineFallback
		next.ordinaryUsesChinese = s.OrdinaryUsesChinese
		next.ordinaryLanguage = s.effectiveOrdinaryLanguage()
		next.syncOrdinaryLanguage()
		if currentInputMode.CompareAndSwap(previous, &next) {
			if previous != nil && previous.translationEngine == engineOffline && next.translationEngine != engineOffline {
				go translation.ReleaseOfflineModel()
			}
			return
		}
	}
}

func saveIMEConfig(next imeSettings) error {
	if err := validateIMEConfig(next); err != nil {
		return err
	}
	settingsLock.Lock()
	defer settingsLock.Unlock()
	if err := writeIMEConfig(next); err != nil {
		return err
	}
	*storedSettings() = next
	return nil
}

func writeIMEConfig(next imeSettings) error {
	path, err := localConfigPath("settings.json")
	if err != nil {
		return err
	}
	return writeLocalJSON(path, next)
}

var modeSettingsWake = make(chan struct{}, 1)
var startModeSettingsWriter = sync.OnceFunc(func() {
	go func() {
		for range modeSettingsWake {
			settingsLock.Lock()
			if err := writeIMEConfig(*storedSettings()); err != nil {
				log.Printf("save input settings: %v", err)
			}
			settingsLock.Unlock()
		}
	}()
})

func rememberModeSettings(mode inputMode) {
	settingsLock.Lock()
	s := storedSettings()
	changed := s.OrdinaryUsesChinese != mode.ordinaryUsesChinese
	s.OrdinaryUsesChinese = mode.ordinaryUsesChinese
	if s.OrdinaryLanguage != mode.ordinaryCode() {
		s.OrdinaryLanguage = mode.ordinaryCode()
		changed = true
	}
	if s.TranslationEngine == engineOnline && mode.translationEngine != engineOffline && s.Language != mode.language {
		s.Language = mode.language
		changed = true
	}
	settingsLock.Unlock()
	if changed {
		startModeSettingsWriter()
		select {
		case modeSettingsWake <- struct{}{}:
		default:
		}
	}
}

func writeLocalJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".contextime-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(value)
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("保存数据: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, path)
}
