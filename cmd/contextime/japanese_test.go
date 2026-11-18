package main

import "testing"

func TestRomajiKana(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {"aiueo", "あいうえお"}, {"nihongo", "にほんご"},
		{"konnichiha", "こんにちは"}, {"konnichiwa", "こんにちわ"},
		{"onnanoko", "おんなのこ"}, {"shin'you", "しんよう"}, {"shinyou", "しにょう"},
		{"n", "ん"}, {"nn", "ん"}, {"nna", "んな"}, {"nnya", "んにゃ"}, {"kan'i", "かんい"},
		{"gakkou", "がっこう"}, {"kitte", "きって"}, {"matcha", "まっちゃ"},
		{"kyaku", "きゃく"}, {"ryokou", "りょこう"}, {"SHA", "しゃ"},
		{"thisyu", "てぃしゅ"}, {"fyu-jon", "ふゅーじょん"}, {"veranda", "ゔぇらんだ"},
		{"xtsu", "っ"}, {"ltsu", "っ"}, {"xya", "ゃ"}, {"xka", "ゕ"},
		{"k", "k"}, {"ky", "ky"}, {"kanj", "かんj"}, {"日本語かな", "日本語かな"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := romajiKana(tc.input); got != tc.want {
				t.Fatalf("romajiKana(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestJapaneseCandidatesKeepKanaAndFullConsumption(t *testing.T) {
	choices := mergeJapaneseChoices("nihongo", []string{"日本語", "日本語", "", "にほんご"})
	want := []string{"日本語", "にほんご", "ニホンゴ"}
	if len(choices) != len(want) {
		t.Fatalf("Japanese choices = %#v", choices)
	}
	for i, item := range choices {
		if item.text != want[i] || item.consume != len("nihongo") {
			t.Fatalf("Japanese choice %d = %#v, want %q consuming the romaji buffer", i, item, want[i])
		}
	}
	if len(japaneseChoices("")) != 0 {
		t.Fatal("empty composition offered a candidate")
	}
	if got := japaneseChoices("kya"); len(got) != 2 || got[0].text != "きゃ" || got[1].text != "キャ" {
		t.Fatalf("offline kana choices = %#v", got)
	}
}
