package main

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"
)

func TestNativeModeQueryKeepsDirectInputFlag(t *testing.T) {
	cases := []struct {
		name string
		mode *inputMode
		flag uint32
		text string
	}{
		{"uninitialized", nil, 0, ""},
		{"ordinary Chinese", &inputMode{chinese: true, language: 1, ordinaryLanguage: "zh-Hans"}, 0, "中\x00中文 · 普通输入"},
		{"direct English", &inputMode{chinese: true, direct: true, language: 0, ordinaryLanguage: "en"}, 1, "英\x00英语 · 普通输入"},
		{"ordinary Japanese", &inputMode{chinese: true, language: 1, ordinaryLanguage: "ja"}, 0, "日\x00日语 · 普通输入"},
		{"English translation", &inputMode{language: englishTarget}, 0, "英\x00中文 → 英语\x002"},
		{"Japanese translation", &inputMode{language: 1}, 0, "日\x00中文 → 日语\x002"},
		{"regional language", &inputMode{language: 10}, 0, "葡\x00中文 → 葡萄牙语（葡萄牙）\x002"},
		{"English to Chinese", &inputMode{language: 21, sourceLanguage: "en"}, 0, "中\x00英语 → 中文\x001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := modeQueryReply(tc.mode)
			flag := binary.LittleEndian.Uint32(reply[:4])
			length := binary.LittleEndian.Uint32(reply[4:8])
			if flag != tc.flag || len(reply) != 8+int(length)*2 {
				t.Fatalf("invalid reply header, flag %d and length %d for %d bytes", flag, length, len(reply))
			}
			units := make([]uint16, length)
			for i := range units {
				units[i] = binary.LittleEndian.Uint16(reply[8+2*i:])
			}
			if text := string(utf16.Decode(units)); text != tc.text {
				t.Fatalf("mode text = %q, want %q", text, tc.text)
			}
		})
	}
}

func TestCandidateCommitHintShowsTargetLanguage(t *testing.T) {
	for _, language := range []int{englishTarget, 1, 10} {
		mode := inputMode{language: language}
		panel := panel{
			state: draft{Chinese: "你好", English: "Hello"},
			input: mode,
			width: panelWidth,
		}
		view := ui.NewTester(panel.view, panelWidth, 140)
		hint := "回车提交" + targetLanguages[language].name
		if _, ok := view.AccessByRole(ui.RoleText, hint); !ok {
			t.Errorf("candidate panel did not show %q", hint)
		}
	}
}
