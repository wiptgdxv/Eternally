package main

import (
	"strings"
	"sync"
)

func isInlineEnglishWord(input string) bool {
	if input == "" {
		return false
	}
	for i := range len(input) {
		letter := input[i]
		if !(letter >= 'a' && letter <= 'z' || letter >= 'A' && letter <= 'Z') {
			return false
		}
	}
	return true
}

func isInlineEnglishCandidate(input string, candidate choice) bool {
	return isInlineEnglishWord(input) && candidate.text == input && candidate.consume == len(input)
}

var inlinePinyinPrefixes = sync.OnceValue(func() map[string]bool {
	prefixes := make(map[string]bool)
	for syllable := range dictionary().syllables {
		for size := 1; size < len(syllable); size++ {
			prefixes[syllable[:size]] = true
		}
	}
	return prefixes
})

// Keep a partially typed final syllable in the Chinese path. For instance,
// zhongg is a useful Chinese spelling while the user is still typing guo.
func inlinePinyinSpelling(input string) bool {
	syllables, prefixes := dictionary().syllables, inlinePinyinPrefixes()
	covered := make([]bool, len(input)+1)
	covered[0] = true
	for start := range len(input) {
		if !covered[start] {
			continue
		}
		if prefixes[input[start:]] {
			return true
		}
		for size := 1; size <= min(6, len(input)-start); size++ {
			if syllables[input[start:start+size]] {
				covered[start+size] = true
			}
		}
	}
	return covered[len(input)]
}

func inlineEnglishFirst(input string) bool {
	lower := strings.ToLower(input)
	if input != lower {
		return true
	}
	if len(input) < 3 {
		return false
	}
	// Initialisms such as nhb and zhrm are Chinese shortcuts even though
	// they cannot be split into full pinyin syllables.
	allInitials := true
	for i := range len(lower) {
		if !strings.ContainsRune("bpmfdtnlgkhjqxrzcsyw", rune(lower[i])) {
			allInitials = false
			break
		}
	}
	return !allInitials && !inlinePinyinSpelling(lower)
}

// Make the typed Latin word selectable without changing input mode. Chinese
// spelling and initialisms retain their ranking; the raw word stays on page one.
func withEnglishCandidate(input string, choices []choice) []choice {
	if !isInlineEnglishWord(input) {
		return choices
	}
	result := make([]choice, 0, len(choices)+1)
	for _, candidate := range choices {
		if !strings.EqualFold(candidate.text, input) {
			result = append(result, candidate)
		}
	}
	position := min(4, len(result))
	if inlineEnglishFirst(input) {
		position = 0
	}
	result = append(result, choice{})
	copy(result[position+1:], result[position:])
	result[position] = choice{input, len(input)}
	return result
}
