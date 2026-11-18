package main

import (
	"cmp"
	"encoding/json"
	"maps"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
)

type learnedWord struct {
	Text   string `json:"text"`
	Count  uint32 `json:"count"`
	Pinned bool   `json:"pinned,omitempty"`
}

type wordFrequencies []learnedWord

func (words *wordFrequencies) UnmarshalJSON(data []byte) error {
	var counts []learnedWord
	if err := json.Unmarshal(data, &counts); err == nil {
		*words = counts
		return nil
	}
	// Older versions stored words in most-recent-first order without counts.
	var previous []string
	if err := json.Unmarshal(data, &previous); err != nil {
		return err
	}
	counts = make([]learnedWord, 0, len(previous))
	for _, word := range previous {
		counts = append(counts, learnedWord{Text: word, Count: 1})
	}
	*words = counts
	return nil
}

var learned = sync.OnceValue(func() map[string]wordFrequencies {
	data, _ := os.ReadFile(learningPath())
	words := make(map[string]wordFrequencies)
	_ = json.Unmarshal(data, &words)
	if words == nil {
		words = make(map[string]wordFrequencies)
	}
	for _, list := range words {
		sortLearnedWords(list)
	}
	return words
})
var learningLock sync.Mutex
var learningFileLock sync.Mutex
var learningSave = make(chan struct{}, 1)
var startLearningWriter = sync.OnceFunc(func() {
	go func() {
		for range learningSave {
			saveLearning()
		}
	}()
})

func learningPath() string {
	path, _ := localConfigPath("words.json")
	return path
}

func preferLearned(input string, choices []choice) []choice {
	if input == "" {
		return choices
	}
	learningLock.Lock()
	words := slices.Clone(learned()[strings.TrimRight(input, "'")])
	learningLock.Unlock()
	if len(words) == 0 {
		return choices
	}
	preferred := make([]choice, 0, len(words))
	for _, word := range words {
		preferred = append(preferred, choice{word.Text, len(input)})
	}
	return joinChoices(preferred, choices)
}

// Learn only after the host acknowledges insertion, so cancelled drafts never affect ranking.
func rememberDraft(d draft) {
	if d.words == nil || d.LanguageCode == "ja" {
		return
	}
	learningLock.Lock()
	db := learned()
	changed := false
	remember := func(spelling, word string) {
		key := strings.TrimRight(spelling, "'")
		if key == "" || numericCode(key) {
			return
		}
		if _, exists := db[key]; !exists && len(db) >= 4096 {
			return
		}
		words := slices.Clone(db[key])
		entry := learnedWord{Text: word, Count: 1}
		if i := slices.IndexFunc(words, func(previous learnedWord) bool { return previous.Text == word }); i >= 0 {
			entry = words[i]
			if entry.Count < math.MaxUint32 {
				entry.Count++
			}
			words = slices.Delete(words, i, i+1)
		}
		// Make room for a new word even when the existing words have higher counts.
		if len(words) >= 8 {
			last := len(words) - 1
			if words[last].Pinned {
				return
			}
			words = words[:last]
		}
		words = append([]learnedWord{entry}, words...)
		sortLearnedWords(words)
		db[key] = words
		changed = true
	}
	var parts []string
	contiguous := d.words.after == d.Chinese
	for w := d.words; w != nil; w = w.previous {
		if w.literal {
			contiguous = false
			continue
		}
		if !strings.HasPrefix(d.Chinese, w.after) {
			contiguous = false
			continue
		}
		remember(w.spelling, w.word)
		if w.previous != nil && w.previous.after != w.before || w.previous == nil && w.before != "" {
			contiguous = false
		}
		parts = append(parts, w.spelling)
	}
	if contiguous && len(parts) > 1 {
		slices.Reverse(parts)
		remember(strings.Join(parts, ""), d.Chinese)
	}
	learningLock.Unlock()
	if !changed {
		return
	}
	startLearningWriter()
	select {
	case learningSave <- struct{}{}:
	default:
		// The queued write will take a fresh snapshot.
	}
}

func saveLearning() {
	learningFileLock.Lock()
	defer learningFileLock.Unlock()
	learningLock.Lock()
	// Learning replaces each word list, so the snapshot stays stable during encoding.
	snapshot := maps.Clone(learned())
	learningLock.Unlock()
	path := learningPath()
	if path != "" {
		_ = writeLocalJSON(path, snapshot)
	}
}

func sortLearnedWords(words wordFrequencies) {
	slices.SortStableFunc(words, func(left, right learnedWord) int {
		if left.Pinned != right.Pinned {
			if left.Pinned {
				return -1
			}
			return 1
		}
		return cmp.Compare(right.Count, left.Count)
	})
}
