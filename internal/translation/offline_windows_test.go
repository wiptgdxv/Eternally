package translation

import (
	"context"
	"testing"
)

func TestOfflineRejectsUnsupportedLanguagePair(t *testing.T) {
	for _, pair := range [][2]string{{"en", "zh-Hans"}, {"ja", "en"}, {"zh-Hans", "ja"}, {"auto", "en"}} {
		got, err := TranslateOfflinePartsBetween(context.Background(), []string{"Hello"}, pair[0], pair[1])
		if err == nil || got != nil || err.Error() != "本地模型仅支持中文转英语" {
			t.Fatalf("unsupported offline pair %v = %#v, error %v", pair, got, err)
		}
	}
}
