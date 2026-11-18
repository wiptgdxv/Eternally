package main

import "testing"

func TestSampleTranslationPunctuation(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"你好，世界！", "Hello, world!"},
		{"你好, 世界!", "Hello, world!"},
		{"你好。世界！", "Hello. World!"},
		{"世界。", "World."},
		{"“你好”", "“Hello”"},
		{"\"你好\"", "\"Hello\""},
		{"（你好）", "(Hello)"},
		{"你好，代码", ""},
		{"！？", ""},
	} {
		if got := sampleTranslation(tc.source); got != tc.want {
			t.Errorf("sampleTranslation(%q) = %q, want %q", tc.source, got, tc.want)
		}
	}
}

func TestNormalizeEnglishPunctuation(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"Hello ，world！", "Hello, world!"},
		{"Before。\nAfter？", "Before.\nAfter?"},
		{"Read at 12：30, use 1，000 or 3．14。", "Read at 12:30, use 1,000 or 3.14."},
		{"Read main.go, then type 3.14.", "Read main.go, then type 3.14."},
	} {
		if got := normalizeEnglishPunctuation(tc.source); got != tc.want {
			t.Errorf("normalizeEnglishPunctuation(%q) = %q, want %q", tc.source, got, tc.want)
		}
	}
}
