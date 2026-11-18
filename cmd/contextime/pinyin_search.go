package main

import (
	"cmp"
	"math/bits"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	sentenceLimit = 4
	spellingLimit = 12
)

type sentenceRoute struct {
	text string
	cost int
}

// Build the best complete readings from bounded dictionary paths. A per-word
// cost favors ordinary phrases over a string of unrelated single characters.
func sentenceChoices(input string, db lexicon, abbreviations bool) []string {
	routes := make([][]sentenceRoute, len(input)+1)
	routes[len(input)] = []sentenceRoute{{}}
	for start := len(input) - 1; start >= 0; start-- {
		if input[start] == '\'' && start > 0 {
			routes[start] = routes[start+1]
			continue
		}
		for end := min(len(input), start+db.maxCodeLen); end > start; end-- {
			if len(routes[end]) == 0 {
				continue
			}
			code := input[start:end]
			words := db.index[code]
			penalty := 0
			if abbreviations && len(words) == 0 && len(code) > 1 {
				words = db.index[":"+code]
				penalty = 6 * len(code)
			}
			for _, word := range words[:min(len(words), sentenceLimit)] {
				weight := min(bits.Len32(db.frequency[word]), 20)
				for _, tail := range routes[end] {
					cost := 60 - 2*weight + penalty + tail.cost
					if tail.text != "" {
						// A common word crossing the boundary is evidence for context.
						_, last := utf8.DecodeLastRuneInString(word)
						_, first := utf8.DecodeRuneInString(tail.text)
						boundary := word[len(word)-last:] + tail.text[:first]
						cost -= min(8, bits.Len32(db.frequency[boundary])/2)
					}
					best := routes[start]
					if len(best) < sentenceLimit || cost < best[len(best)-1].cost {
						routes[start] = addSentenceRoute(best, sentenceRoute{word + tail.text, cost})
					}
				}
			}
		}
	}
	words := make([]string, 0, len(routes[0]))
	for _, route := range routes[0] {
		words = append(words, route.text)
	}
	return words
}

func addSentenceRoute(routes []sentenceRoute, next sentenceRoute) []sentenceRoute {
	if i := slices.IndexFunc(routes, func(route sentenceRoute) bool { return route.text == next.text }); i >= 0 {
		if routes[i].cost <= next.cost {
			return routes
		}
		routes = slices.Delete(routes, i, i+1)
	}
	routes = append(routes, next)
	slices.SortStableFunc(routes, func(a, b sentenceRoute) int { return cmp.Compare(a.cost, b.cost) })
	return routes[:min(len(routes), sentenceLimit)]
}

type pinyinPath struct {
	text    string
	changes int
	full    bool
}

func addPinyinPath(paths []pinyinPath, path pinyinPath) []pinyinPath {
	if slices.Contains(paths, path) {
		return paths
	}
	index := len(paths)
	for i, previous := range paths {
		if path.changes < previous.changes {
			index = i
			break
		}
	}
	if index == spellingLimit {
		return paths
	}
	paths = slices.Insert(paths, index, path)
	return paths[:min(len(paths), spellingLimit)]
}

func initialLength(syllable string) int {
	if len(syllable) >= 2 && syllable[1] == 'h' && strings.ContainsRune("zcs", rune(syllable[0])) {
		return 2
	}
	return 1
}

// Find phrases by their initials, then verify the typed spelling. An all-initial
// spelling needs this search only if it adds separators or uses a digraph.
func mixedChoices(input string, db lexicon) []string {
	paths := make([][]pinyinPath, len(input)+1)
	paths[0] = []pinyinPath{{}}
	for start, options := range paths[:len(input)] {
		if len(options) == 0 {
			continue
		}
		if input[start] == '\'' && start > 0 {
			for _, p := range options {
				paths[start+1] = addPinyinPath(paths[start+1], p)
			}
			continue
		}
		if input[start] < 'a' || input[start] > 'z' {
			continue
		}
		for _, p := range options {
			initials := p.text + input[start:start+1]
			for n := min(6, len(input)-start); n >= 1; n-- {
				if db.syllables[input[start:start+n]] {
					paths[start+n] = addPinyinPath(paths[start+n], pinyinPath{initials, p.changes, true})
				}
			}
			for n := 1; n <= initialLength(input[start:]); n++ {
				paths[start+n] = addPinyinPath(paths[start+n], pinyinPath{initials, p.changes + 1, p.full})
			}
		}
	}

	var matches []string
	seen := make(map[string]bool)
	for _, p := range paths[len(input)] {
		if p.changes == 0 || !p.full && p.text == input || seen[p.text] {
			continue
		}
		seen[p.text] = true
		for _, word := range db.index[":"+p.text] {
			if spellingCoverage(input, word, db.readings, false) == len(input) && !slices.Contains(matches, word) {
				matches = append(matches, word)
				if len(matches) == spellingLimit {
					return matches
				}
			}
		}
	}
	return matches
}

var commonTypoEndings = [...]struct{ typed, normal string }{
	{"ign", "ing"}, {"img", "ing"}, {"uei", "ui"}, {"uen", "un"}, {"iou", "iu"},
}

func canCorrectPinyin(input string) bool {
	for _, pair := range commonTypoEndings {
		if strings.Contains(input, pair.typed) {
			return true
		}
	}
	return strings.HasPrefix(input, "sogou")
}

var fuzzyPairs = [...]struct{ a, b string }{
	{"n", "l"}, {"z", "zh"}, {"c", "ch"}, {"s", "sh"},
}

func fuzzySyllable(syllable string) string {
	first := initialLength(syllable)
	prefix, rest := syllable[:first], syllable[first:]
	if rest == "" {
		return ""
	}
	for _, pair := range fuzzyPairs {
		switch prefix {
		case pair.a:
			return pair.b + rest
		case pair.b:
			return pair.a + rest
		}
	}
	return ""
}

func correctedSyllable(typed string) string {
	if typed == "so" {
		return "sou"
	}
	for _, pair := range commonTypoEndings {
		if strings.HasSuffix(typed, pair.typed) {
			return strings.TrimSuffix(typed, pair.typed) + pair.normal
		}
	}
	return ""
}

// Substitute only entire valid syllables. Keep the original byte count at the
// call site so choosing a correction consumes exactly the user's typed text.
func variantChoices(input string, db lexicon, fuzzy bool) []string {
	paths := make([][]pinyinPath, len(input)+1)
	paths[0] = []pinyinPath{{}}
	for start, options := range paths[:len(input)] {
		if len(options) == 0 {
			continue
		}
		if input[start] == '\'' && start > 0 {
			for _, p := range options {
				p.text += "'"
				paths[start+1] = addPinyinPath(paths[start+1], p)
			}
			continue
		}
		for n := min(7, len(input)-start); n >= 1; n-- {
			typed := input[start : start+n]
			valid := db.syllables[typed]
			var alternate string
			if fuzzy {
				if valid {
					alternate = fuzzySyllable(typed)
				}
			} else {
				alternate = correctedSyllable(typed)
			}
			for _, p := range options {
				if valid {
					paths[start+n] = addPinyinPath(paths[start+n], pinyinPath{text: p.text + typed, changes: p.changes})
				}
				if p.changes < 2 && db.syllables[alternate] {
					paths[start+n] = addPinyinPath(paths[start+n], pinyinPath{text: p.text + alternate, changes: p.changes + 1})
				}
			}
		}
	}
	var candidates []string
	for _, path := range paths[len(input)] {
		if path.changes == 0 || path.text == input {
			continue
		}
		words := db.index[path.text]
		if len(words) == 0 && len(path.text) >= 6 {
			words = sentenceChoices(path.text, db, false)
		}
		for _, word := range words[:min(len(words), spellingLimit)] {
			if !slices.Contains(candidates, word) {
				candidates = append(candidates, word)
				if len(candidates) == spellingLimit {
					return candidates
				}
			}
		}
	}
	return candidates
}
