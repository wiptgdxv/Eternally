package main

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type personalWord struct {
	key string
	learnedWord
}

func personalWords(filter string) []personalWord {
	filter = strings.ToLower(strings.TrimSpace(filter))
	learningLock.Lock()
	defer learningLock.Unlock()
	db := learned()
	var words []personalWord
	for _, key := range slices.Sorted(maps.Keys(db)) {
		for _, word := range db[key] {
			if strings.Contains(key, filter) || strings.Contains(strings.ToLower(word.Text), filter) {
				words = append(words, personalWord{key, word})
			}
		}
	}
	return words
}

func editPersonalWord(word personalWord, remove bool) error {
	return changePersonalWords(func(db map[string]wordFrequencies) {
		list := slices.Clone(db[word.key])
		index := slices.IndexFunc(list, func(item learnedWord) bool { return item.Text == word.Text })
		if index < 0 {
			return
		}
		if remove {
			list = slices.Delete(list, index, index+1)
		} else {
			list[index].Pinned = !list[index].Pinned
			// Preserve recency for matching counts.
			entry := list[index]
			list = append([]learnedWord{entry}, slices.Delete(list, index, index+1)...)
			sortLearnedWords(list)
		}
		if len(list) == 0 {
			delete(db, word.key)
		} else {
			db[word.key] = list
		}
	})
}

func changePersonalWords(change func(map[string]wordFrequencies)) error {
	learningFileLock.Lock()
	defer learningFileLock.Unlock()
	learningLock.Lock()
	defer learningLock.Unlock()
	db := learned()
	updated := maps.Clone(db)
	change(updated)
	path, err := localConfigPath("words.json")
	if err != nil {
		return err
	}
	if err := writeLocalJSON(path, updated); err != nil {
		return err
	}
	clear(db)
	maps.Copy(db, updated)
	return nil
}

func exportPersonalWords(path string) error {
	learningLock.Lock()
	words := maps.Clone(learned())
	learningLock.Unlock()
	return writeLocalJSON(path, words)
}

func sameDictionaryPath(a, b string) bool {
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	first, firstErr := os.Stat(a)
	second, secondErr := os.Stat(b)
	return firstErr == nil && secondErr == nil && os.SameFile(first, second)
}

func importPersonalWords(path string) error {
	data, err := readDictionaryFile(path, 64*1024*1024)
	if err != nil {
		return err
	}
	var imported map[string]wordFrequencies
	if json.Unmarshal(data, &imported) != nil || imported == nil || len(imported) > 4096 {
		return errors.New("个人词库文件格式无效")
	}
	for key, list := range imported {
		// A learned sentence can combine several shorter pinyin inputs.
		if key == "" || dictionaryKey(key, 1024) != key || len(list) > 8 {
			return errors.New("个人词库包含无效的拼音或过多的同音词")
		}
		for _, word := range list {
			if !dictionaryText(word.Text, 192) || word.Count == 0 {
				return errors.New("个人词库包含无效的词条")
			}
		}
	}
	return changePersonalWords(func(db map[string]wordFrequencies) {
		for _, key := range slices.Sorted(maps.Keys(imported)) {
			existing, ok := db[key]
			if !ok && len(db) >= 4096 {
				continue
			}
			list := slices.Clone(existing)
			for _, word := range imported[key] {
				i := slices.IndexFunc(list, func(item learnedWord) bool { return item.Text == word.Text })
				if i >= 0 {
					list[i].Count = max(list[i].Count, word.Count)
					list[i].Pinned = list[i].Pinned || word.Pinned
				} else {
					list = append(list, word)
				}
			}
			sortLearnedWords(list)
			if len(list) > 0 {
				db[key] = list[:min(len(list), 8)]
			}
		}
	})
}

func readDictionaryFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("词库文件过大或不可读取")
	}
	return os.ReadFile(path)
}

func validDictionaryKey(value string) string {
	return dictionaryKey(value, 96)
}

func dictionaryKey(value string, limit int) string {
	key := strings.ToLower(strings.TrimSpace(value))
	if len(key) == 0 || len(key) > limit || key[0] == '\'' || key[len(key)-1] == '\'' || strings.Contains(key, "''") {
		return ""
	}
	for _, ch := range key {
		if (ch < 'a' || ch > 'z') && ch != '\'' {
			return ""
		}
	}
	return key
}

func validDictionaryText(value string) bool {
	return dictionaryText(value, 80)
}

func dictionaryText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit &&
		!strings.ContainsRune(value, 0)
}

func readPhrases(path string) (map[string][]string, error) {
	entries := make(map[string][]string)
	data, err := readDictionaryFile(path, 64*1024)
	if os.IsNotExist(err) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(data, &entries) != nil || entries == nil {
		return nil, errors.New("自定义短语文件格式无效")
	}
	return entries, nil
}

func phraseEntries() (map[string][]string, error) {
	path, err := localConfigPath("phrases.json")
	if err != nil {
		return nil, err
	}
	phraseFile.Lock()
	defer phraseFile.Unlock()
	return readPhrases(path)
}

func editPhrase(key, old, text string) error {
	key, text = validDictionaryKey(key), strings.TrimSpace(text)
	if key == "" || text != "" && !validDictionaryText(text) {
		return errors.New("输入有效的拼音或缩写，短语最多 80 个字符")
	}
	if old == "" && text == "" {
		return errors.New("请输入短语")
	}
	path, err := localConfigPath("phrases.json")
	if err != nil {
		return err
	}
	phraseFile.Lock()
	defer phraseFile.Unlock()
	entries, err := readPhrases(path)
	if err != nil {
		return err
	}
	list := entries[key]
	index := -1
	if old != "" {
		index = slices.Index(list, old)
	}
	if old != "" && index < 0 {
		return errors.New("短语已被修改，请重新选择")
	}
	if existing := slices.Index(list, text); text != "" && existing >= 0 && existing != index {
		return errors.New("这个缩写已包含该短语")
	}
	switch {
	case text == "":
		list = slices.Delete(list, index, index+1)
	case index >= 0:
		list[index] = text
	case len(list) < pageSize:
		list = append(list, text)
	default:
		return errors.New("同一缩写最多保存五条短语")
	}
	if len(list) == 0 {
		delete(entries, key)
	} else {
		entries[key] = list
	}
	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded)+1 > 64*1024 {
		return errors.New("自定义短语文件已满")
	}
	if err := writeLocalJSON(path, entries); err != nil {
		return err
	}
	phraseFile.entries = entries
	phraseFile.checkAfter = time.Now().Add(time.Second)
	return nil
}
