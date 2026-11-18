package main

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"lo-windows/internal/translation"
)

func TestTranslationMemoryIsBoundedAndSeparatesRoutes(t *testing.T) {
	var memory translationMemory
	online := translationRoute{language: "en", engine: engineOnline}
	offline := translationRoute{language: "en", engine: engineOffline}
	japanese := translationRoute{language: "ja", engine: engineOnline}
	const source = "周三见"
	memory.set(online.cacheKey(source), "See you on Wednesday.")
	memory.set(offline.cacheKey(source), "See you Wednesday.")
	memory.set(japanese.cacheKey(source), "水曜日に会いましょう。")
	for i := range translationCacheEntries - 3 {
		memory.set(online.cacheKey(strconv.Itoa(i)), "result")
	}
	if got := memory.get(online.cacheKey(source)); got != "See you on Wednesday." {
		t.Fatalf("recent online result was lost: %q", got)
	}
	memory.set(online.cacheKey("another entry"), "result")
	if got := memory.get(offline.cacheKey(source)); got != "" {
		t.Fatalf("oldest entry remained after eviction: %q", got)
	}
	if got := memory.get(japanese.cacheKey(source)); got != "水曜日に会いましょう。" {
		t.Fatalf("wrong language result: %q", got)
	}
	memory.set(online.cacheKey(source), "")
	if got := memory.get(online.cacheKey(source)); got == "" {
		t.Fatal("an empty response replaced a successful result")
	}
}

func TestCachedDraftPreservesLiteralBoundaries(t *testing.T) {
	d := draft{Chinese: "请在周三前完成 "}
	d.appendLiteral(`C:\Work\test.txt`)
	d.Chinese += "。"
	route := translationRoute{language: "en", engine: engineOnline}
	sessionTranslations.set(route.cacheKey("请在周三前完成"), "Please finish by Wednesday")
	if got := cachedTranslation(d, route); got != `Please finish by Wednesday C:\Work\test.txt.` {
		t.Fatalf("cached mixed result = %q", got)
	}
	if got := cachedTranslation(d, translationRoute{language: "en", engine: engineOffline}); got != "" {
		t.Fatalf("offline mode reused an online result: %q", got)
	}
}

func TestOfflineFallbackRequiresOptIn(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	d := draft{Chinese: "会议将在下周一上午召开"}
	online := translationRoute{language: "en", engine: engineOnline}
	offline := translationRoute{language: "en", engine: engineOffline}
	const want = "The meeting will be held next Monday morning."
	sessionTranslations.set(online.cacheKey(d.Chinese), want)
	if got, err := translateDraft(context.Background(), d, offline); got != "" || !errors.Is(err, translation.ErrOfflineUnavailable) {
		t.Fatalf("offline mode used an online result without opt-in: %q, %v", got, err)
	}
	offline.onlineFallback = true
	if got, err := translateDraft(context.Background(), d, offline); got != want || err != nil {
		t.Fatalf("opt-in fallback did not use the existing online result: %q, %v", got, err)
	}
	if got := cachedTranslation(d, offline); got != "" {
		t.Fatalf("online fallback result was saved as an offline result: %q", got)
	}
}
