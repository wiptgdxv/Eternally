package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfflineLearningFrequency(t *testing.T) {
	const stageVariable = "CONTEXTIME_LEARNING_TEST_STAGE"
	if stage := os.Getenv(stageVariable); stage != "" {
		start := preferLearned("nihao", lookupChoices("nihao"))
		assertRank := func(got []choice, first, second string) {
			t.Helper()
			if len(got) < 2 || got[0].text != first || got[1].text != second {
				t.Fatalf("unexpected ranking: %v", got)
			}
		}
		if stage == "learn" {
			assertRank(start, "你好", "拟好")
			for _, word := range []string{"拟好", "拟好", "你好"} {
				d := draft{Pinyin: "nihao"}
				d.selectWord(choice{word, 5})
				rememberDraft(d)
			}
		}
		assertRank(preferLearned("nihao", lookupChoices("nihao")), "拟好", "你好")
		assertRank(mergeWindowsChoices("nihao", []string{"你好", "拟好"}), "拟好", "你好")
		assertRank((draft{Pinyin: "nihao"}).candidates(), "优先测试短语", "拟好")
		deadline := time.Now().Add(time.Second)
		for {
			data, _ := os.ReadFile(learningPath())
			var saved map[string]wordFrequencies
			if json.Unmarshal(data, &saved) == nil && len(saved["nihao"]) >= 2 &&
				saved["nihao"][0] == (learnedWord{Text: "拟好", Count: 3}) &&
				saved["nihao"][1] == (learnedWord{Text: "你好", Count: 2}) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("learning file did not contain the updated counts: %s", data)
			}
			time.Sleep(10 * time.Millisecond)
		}
		return
	}

	profile := t.TempDir()
	dir := filepath.Join(profile, "ContextIME")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"words.json":   `{"nihao":["你好","拟好"]}`,
		"phrases.json": `{"nihao":["优先测试短语"]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}

	for _, stage := range []string{"learn", "reload"} {
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
		cmd.Env = append(cmd.Environ(), "APPDATA="+profile, "XDG_CONFIG_HOME="+profile, stageVariable+"="+stage)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", stage, err, strings.TrimSpace(string(output)))
		}
	}
}
