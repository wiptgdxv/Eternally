package main

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTranslationActionsStayInTranslatedRow(t *testing.T) {
	p := panel{native: true, updateTranslationActions: func(func()) {}}
	p.state = draft{Chinese: "你好", English: "Hello"}
	tester := ui.NewTester(p.view, panelWidth, 200)
	tester.SetScale(1.25)
	verify := func(visible bool) {
		t.Helper()
		tester.Frame()
		copyButton, hasCopy := tester.Find("复制译文")
		speechButton, hasSpeech := tester.Find("朗读译文")
		if hasCopy != visible || hasSpeech != visible {
			t.Fatalf("translation actions visible = %v/%v, want %v", hasCopy, hasSpeech, visible)
		}
		if visible && (copyButton.X+copyButton.W > speechButton.X || speechButton.X+speechButton.W > panelWidth || copyButton.Y != speechButton.Y || speechButton.Y+speechButton.H > float32(p.height)) {
			t.Fatalf("translation actions overlap or clip: copy=%v speech=%v", copyButton, speechButton)
		}
	}
	verify(true)
	shortHeight := p.height
	p.state.English = strings.Repeat("A carefully translated sentence remains readable. ", 4)
	verify(true)
	if p.height <= shortHeight {
		t.Fatalf("long translation did not wrap: long height %d, short height %d", p.height, shortHeight)
	}
	p.input.chinese = true
	verify(false)
	p.input.chinese = false
	p.state.English, p.state.TranslationStatus = "", translationPending
	verify(false)
}

func TestReplacingDraftCancelsSpeech(t *testing.T) {
	p := panel{state: draft{Chinese: "你好", English: "Hello"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.translationActions.speechOf = p.displayedTranslation()
	p.translationActions.stopSpeech = cancel
	p.cancelObsoleteSpeech()
	if ctx.Err() != nil {
		t.Fatalf("unchanged translation cancelled speech: %v", ctx.Err())
	}
	p.state.Pinyin = "shi"
	p.cancelObsoleteSpeech()
	if ctx.Err() != context.Canceled || p.translationActions.stopSpeech != nil {
		t.Fatal("speech continued after the displayed draft changed")
	}
}

func TestSpeechLocaleRank(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested uint16
		token     string
		want      int
	}{
		{"exact region", 0x0809, "409;809", 2},
		{"same language", 0x0809, "409", 1},
		{"different language", 0x0411, "409;809", 0},
		{"invalid token", 0x0411, "N/A", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rank := speechLocaleRank(tc.requested, tc.token); rank != tc.want {
				t.Fatalf("speechLocaleRank = %d, want %d", rank, tc.want)
			}
		})
	}
}
