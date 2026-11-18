package main

import (
	"reflect"
	"testing"
)

func TestInlineEnglishWordsAreAvailableWithoutChangingChinesePriority(t *testing.T) {
	for _, input := range []string{"nihao", "zhongguo", "nhb", "zhongg", "n", "sh"} {
		t.Run(input, func(t *testing.T) {
			original, _ := lookupRankedChoices(input, false)
			if len(original) == 0 {
				t.Fatal("test spelling has no Chinese candidates")
			}
			before := append([]choice(nil), original...)
			got := withEnglishCandidate(input, original)
			if got[0] != original[0] {
				t.Fatalf("Chinese first choice changed from %+v to %+v", original[0], got[0])
			}
			englishAt := -1
			var chinese []choice
			for index, candidate := range got {
				if isInlineEnglishCandidate(input, candidate) {
					englishAt = index
				} else {
					chinese = append(chinese, candidate)
				}
			}
			if englishAt < 1 || englishAt >= pageSize {
				t.Fatalf("raw Latin spelling is not visible on first page: index=%d", englishAt)
			}
			if !reflect.DeepEqual(chinese, before) || !reflect.DeepEqual(original, before) {
				t.Fatal("adding English mutated, reordered or removed Chinese candidates")
			}
		})
	}
}

func TestClearlyEnglishWordsBecomeExactFirstChoiceOffline(t *testing.T) {
	for _, input := range []string{"shift", "hello", "Windows", "Python", "world", "browser"} {
		t.Run(input, func(t *testing.T) {
			original := []choice{{"是", 3}, {"时候", 3}, {"shift键", len(input)}}
			got := withEnglishCandidate(input, original)
			if !isInlineEnglishCandidate(input, got[0]) {
				t.Fatalf("English exact spelling is not first: %+v", got)
			}
			if !reflect.DeepEqual(got[1:], original) {
				t.Fatal("English candidate removed Chinese or mixed-language alternatives")
			}
		})
	}
}

func TestInlineEnglishCandidatePreservesCaseAndDeduplicates(t *testing.T) {
	input := "Windows"
	original := []choice{{"windows", len(input)}, {"窗口", len(input)}, {"Windows", 3}, {"WINDOWS", len(input)}}
	got := withEnglishCandidate(input, original)
	if len(got) != 2 || !isInlineEnglishCandidate(input, got[0]) || got[1].text != "窗口" {
		t.Fatalf("case-preserving exact candidate was not deduplicated: %+v", got)
	}
	if isInlineEnglishCandidate(input, choice{"windows", len(input)}) || isInlineEnglishCandidate(input, choice{input, 3}) {
		t.Fatal("partial or differently cased candidate was mistaken for raw input")
	}
	if got := withEnglishCandidate("shift", nil); len(got) != 1 || !isInlineEnglishCandidate("shift", got[0]) {
		t.Fatalf("missing Chinese candidates hid the English word: %+v", got)
	}
}

func TestInlineEnglishDoesNotCapturePinyinSeparatorsNumbersOrURLs(t *testing.T) {
	original := []choice{{"西安", 5}}
	for _, input := range []string{"", "xi'an", "2.5", "v123", "https://example.com", "name@example.com", "你好", "a b", "foo_bar"} {
		if isInlineEnglishWord(input) || isInlineEnglishCandidate(input, choice{input, len(input)}) {
			t.Fatalf("non-word input %q was treated as raw English", input)
		}
		if got := withEnglishCandidate(input, original); !reflect.DeepEqual(got, original) {
			t.Fatalf("non-word input %q changed special candidates: %+v", input, got)
		}
	}
}
