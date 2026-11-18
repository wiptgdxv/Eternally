package main

import (
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsViewTranslationPair(t *testing.T) {
	s := defaultIMEConfig()
	s.SourceLanguage = "en"
	for i, language := range targetLanguages {
		if language.code == "zh-Hans" {
			s.Language = i
			break
		}
	}
	v := newSettingsView(s)
	if got := v.editedSettings(); got != s {
		t.Fatalf("opening settings changed English-to-Chinese pair: %+v", got)
	}
	v.swapTranslationLanguages()
	got := v.editedSettings()
	if got.SourceLanguage != "zh-Hans" || targetLanguages[got.Language].code != "en" {
		t.Fatalf("swapped pair = %s → %s", got.SourceLanguage, targetLanguages[got.Language].code)
	}
	v.swapTranslationLanguages()
	if got := v.editedSettings(); got != s {
		t.Fatalf("swapping back changed settings: %+v", got)
	}
}

func TestSettingsViewAutoSourceCannotBecomeTarget(t *testing.T) {
	s := defaultIMEConfig()
	s.SourceLanguage = "auto"
	v := newSettingsView(s)
	v.swapTranslationLanguages()
	if got := v.editedSettings(); got != s {
		t.Fatalf("auto source was swapped into target: %+v", got)
	}
}

func TestLegacySettingsDefaultToChineseSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"language":1,"appearance":"dark","ordinaryUsesChinese":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	got := readIMEConfig(path)
	if got.SourceLanguage != "zh-Hans" || got.Language != 1 || got.Appearance != "dark" || !got.OrdinaryUsesChinese {
		t.Fatalf("legacy settings migration changed preferences: %+v", got)
	}
}

func TestSettingsViewSavingPreservesModeAcrossPairChanges(t *testing.T) {
	const profileVariable = "CONTEXTIME_SETTINGS_PAIR_TEST_PROFILE"
	if os.Getenv(profileVariable) == "" {
		profile := t.TempDir()
		command := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
		command.Env = append(command.Environ(), "APPDATA="+profile, profileVariable+"="+profile)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated settings test: %v\n%s", err, output)
		}
		return
	}
	previousMode := currentInputMode.Load()
	t.Cleanup(func() { currentInputMode.Store(previousMode) })
	for _, ordinaryChinese := range []bool{false, true} {
		s := defaultIMEConfig()
		s.OrdinaryUsesChinese = ordinaryChinese
		if err := saveIMEConfig(s); err != nil {
			t.Fatal(err)
		}
		currentInputMode.Store(&inputMode{chinese: true, direct: !ordinaryChinese, ordinaryUsesChinese: ordinaryChinese})
		applyInputSettings()
		v := newSettingsView(s)
		v.appearance = "深色"
		v.saveSettings()
		if v.saveError != "" {
			t.Fatal(v.saveError)
		}
		mode := currentInputMode.Load()
		if !mode.chinese || mode.direct != !ordinaryChinese || mode.ordinaryUsesChinese != ordinaryChinese {
			t.Fatalf("appearance save changed ordinary mode: %+v", mode)
		}
		v.sourceLanguage = sourceLanguageName("en")
		v.language = sourceLanguageName("zh-Hans")
		v.saveSettings()
		if v.saveError != "" {
			t.Fatal(v.saveError)
		}
		mode = currentInputMode.Load()
		if !mode.chinese || mode.direct != !ordinaryChinese || mode.ordinaryUsesChinese != ordinaryChinese || mode.sourceLanguage != "en" || targetLanguages[mode.language].code != "zh-Hans" {
			t.Fatalf("pair save changed ordinary mode or its selected language: %+v", mode)
		}
	}
}

func TestSettingsTranslationPairLayoutAndMouse(t *testing.T) {
	for _, size := range [][2]int{{790, 610}, {560, 465}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			s := defaultIMEConfig()
			s.Appearance = "light"
			v := newSettingsView(s)
			v.tab = 1
			view := ui.NewTester(v.render, size[0], size[1])
			var previous ui.Rect
			for _, label := range []string{"翻译引擎", "输入语言", "目标语言", "交换语言"} {
				role := ui.RolePopUpButton
				if label == "交换语言" {
					role = ui.RoleButton
				}
				control, ok := view.AccessByRole(role, label)
				if !ok || control.Offscreen {
					t.Fatalf("language-pair control %q not visible", label)
				}
				bounds := control.Bounds
				if bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(size[0]) || bounds.Y+bounds.H > float32(size[1]) {
					t.Errorf("control %q outside window: %+v", label, bounds)
				}
				if previous.Y+previous.H > bounds.Y {
					t.Errorf("control %q overlaps previous row", label)
				}
				previous = bounds
			}
			if control, ok := view.AccessByRole(ui.RoleButton, "保存设置"); !ok || !control.Disabled {
				t.Fatal("unchanged settings should show disabled save button")
			}
			if directory := os.Getenv("CONTEXTIME_SETTINGS_PAIR_QA_DIR"); directory != "" {
				file, err := os.Create(filepath.Join(directory, t.Name()[len("TestSettingsTranslationPairLayoutAndMouse/"):]+".png"))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(file, view.Image()); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := view.Click("交换语言"); err != nil {
				t.Fatal(err)
			}
			if v.sourceLanguage != sourceLanguageName("en") || v.language != sourceLanguageName("zh-Hans") {
				t.Fatalf("mouse swap failed: %s → %s", v.sourceLanguage, v.language)
			}
			if control, ok := view.AccessByRole(ui.RoleButton, "保存设置"); !ok || control.Disabled {
				t.Fatal("changed language pair should enable save button")
			}
		})
	}
}
