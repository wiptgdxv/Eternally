package main

import (
	"slices"
	"testing"
)

func TestOfflinePinyinRanking(t *testing.T) {
	for _, tc := range []struct {
		input string
		first string
	}{
		{"nihao", "你好"},
		{"niha", "你好"},
		{"nhb", "那好吧"},
		{"n'h'b", "那好吧"},
		{"nhaoba", "那好吧"},
		{"bjdaxue", "北京大学"},
		{"bj'daxue", "北京大学"},
		{"jintiantq", "今天天气"},
		{"xi'an", "西安"},
		{"woxiangqubeijing", "我想去北京"},
		{"jintiantianqihenhao", "今天天气很好"},
		{"sogoushurufa", "搜狗输入法"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			choices := lookupChoices(tc.input)
			if len(choices) == 0 || choices[0] != (choice{tc.first, len(tc.input)}) {
				t.Fatalf("first candidate should be %q, got %v", tc.first, choices[:min(6, len(choices))])
			}
		})
	}
}

func TestPinyinCorrections(t *testing.T) {
	for _, tc := range []struct {
		input string
		word  string
	}{
		{"dign", "顶"},
		{"dimg", "顶"},
		{"gueilai", "归来"},
		{"guendong", "滚动"},
		{"lioushui", "流水"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			choices := lookupChoices(tc.input)
			if !slices.Contains(choices, choice{tc.word, len(tc.input)}) {
				t.Fatalf("missing %q with full source consumption in %v", tc.word, choices[:min(10, len(choices))])
			}
		})
	}
}

func TestFuzzyPinyinPairs(t *testing.T) {
	for _, tc := range []struct {
		input string
		word  string
	}{
		{"zongguo", "中国"},
		{"lanjing", "南京"},
		{"congqing", "重庆"},
		{"suzhuang", "梳妆"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			want := choice{tc.word, len(tc.input)}
			if choices, _ := lookupRankedChoices(tc.input, false); slices.Contains(choices, want) {
				t.Fatalf("found %q with fuzzy pinyin disabled", tc.word)
			}
			if choices, keep := lookupRankedChoices(tc.input, true); !slices.Contains(choices[:keep], want) {
				t.Fatalf("missing %q in local fuzzy shortlist %v", tc.word, choices[:keep])
			}
			if choices := mergePinyinChoices(tc.input, []string{"宗国", "棕果"}, true); !slices.Contains(choices, want) {
				t.Fatalf("missing %q in merged fuzzy candidates %v", tc.word, choices[:min(len(choices), 12)])
			}
		})
	}
}

func TestPinyinCandidateCoverage(t *testing.T) {
	choices := mergeWindowsChoices("jintiantianqizenmeyang", []string{"今天天气怎么样", "今天天气"})
	want := choice{"今天天气", len("jintiantianqi")}
	if !slices.Contains(choices, want) {
		t.Fatalf("missing partial candidate %v in %v", want, choices[:min(10, len(choices))])
	}
	choices = mergeWindowsChoices("xi'an", []string{"先", "西安"})
	if slices.Contains(choices, choice{"先", len("xi'an")}) {
		t.Fatal("explicit syllable split was ignored")
	}
}

func BenchmarkOfflinePinyinRanking(b *testing.B) {
	dictionary()
	for _, input := range []string{"nihao", "bjdaxue", "jintiantq", "woxiangqubeijingdaxue", "jintiantianqihenhaowomenyiqiquchifan"} {
		b.Run(input, func(b *testing.B) {
			for range b.N {
				lookupChoices(input)
			}
		})
	}
}
