package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var symbolGroups = [...]string{
	"、。！，？；：…—·「」『』《》〈〉【】〔〕（）“”‘’",
	"①②③④⑤⑥⑦⑧⑨⑩⑪⑫⑬⑭⑮⑯⑰⑱⑲⑳ⅠⅡⅢⅣⅤⅥⅦⅧⅨⅩ",
	"×÷±≈≠≤≥∞√∑∏∫∂∈∉∪∩∅℃℉°‰％㎜㎝㎞㎡㎏",
	"あいうえおかきくけこさしすせそたちつてとなにぬねのはひふへほまみむめもやゆよらりるれろわをん",
	"アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン",
	"αβγδεζηθικλμνξοπρστυφχψωΑΒΓΔΩæœßø",
	"АБВГДЕЁЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯабвгдеёжзийклмнопрстуфхцчшщъыьэюя",
	"āáǎàōóǒòēéěèīíǐìūúǔùǖǘǚǜüㄅㄆㄇㄈㄉㄊㄋㄌㄍㄎㄏ",
	"─│┌┐└┘├┤┬┴┼═║╔╗╚╝╠╣╦╩╬■□▲△●○◆◇",
}

func symbolCode(input string) bool {
	return len(input) == 2 && input[0] == 'v' && input[1] >= '1' && input[1] <= '9'
}

var phraseFile struct {
	sync.Mutex
	checkAfter time.Time
	entries    map[string][]string
}

func customPhraseChoices(input string) []choice {
	if input == "" {
		return nil
	}
	phraseFile.Lock()
	defer phraseFile.Unlock()
	now := time.Now()
	if !now.Before(phraseFile.checkAfter) {
		phraseFile.checkAfter = now.Add(time.Second)
		dir, err := os.UserConfigDir()
		if err == nil {
			path := filepath.Join(dir, "ContextIME", "phrases.json")
			if info, err := os.Stat(path); err == nil && info.Size() <= 64*1024 {
				if data, err := os.ReadFile(path); err == nil {
					var entries map[string][]string
					if json.Unmarshal(data, &entries) == nil {
						phraseFile.entries = entries
					}
				}
			} else if os.IsNotExist(err) {
				phraseFile.entries = nil
			}
		}
	}
	var choices []choice
	for _, phrase := range phraseFile.entries[input] {
		if strings.TrimSpace(phrase) != "" && len([]rune(phrase)) <= 80 {
			choices = append(choices, choice{phrase, len(input)})
			if len(choices) == pageSize {
				break
			}
		}
	}
	return choices
}
