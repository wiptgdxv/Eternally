package translation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type translationTransport func(*http.Request) (*http.Response, error)

func (f translationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestOnlineLanguagePairRequest(t *testing.T) {
	cases := []struct {
		name, source, target, wantSource string
	}{
		{"English to Chinese", "en", "zh-Hans", "en"},
		{"Japanese to French", "ja", "fr", "ja"},
		{"Auto source", "auto", "zh-Hans", ""},
		{"Empty auto source", "", "en", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			previous := translationClient
			t.Cleanup(func() { translationClient = previous })
			input := []string{"Hello", "Good morning"}
			translationClient = &http.Client{Transport: translationTransport(func(request *http.Request) (*http.Response, error) {
				query := request.URL.Query()
				if query.Get("from") != tc.wantSource || query.Get("to") != tc.target {
					t.Fatalf("language pair query = %v", query)
				}
				if tc.wantSource == "" && query.Has("from") {
					t.Fatal("automatic source request included from")
				}
				if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
					t.Fatal("translation request lost its POST JSON format")
				}
				var body []string
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil || !reflect.DeepEqual(body, input) {
					t.Fatalf("translation body = %#v, error %v", body, err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`[{"translations":[{"text":" 你好 "}]},{"translations":[{"text":"早上好"}]}]`,
					)),
				}, nil
			})}
			got, err := TranslatePartsBetween(context.Background(), input, tc.source, tc.target)
			if err != nil || !reflect.DeepEqual(got, []string{"你好", "早上好"}) {
				t.Fatalf("translation = %#v, error %v", got, err)
			}
		})
	}
}

func TestLegacyOnlineTranslationUsesChinese(t *testing.T) {
	previous := translationClient
	t.Cleanup(func() { translationClient = previous })
	translationClient = &http.Client{Transport: translationTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("from") != "zh-Hans" || request.URL.Query().Get("to") != "en" {
			t.Fatalf("legacy request pair = %v", request.URL.Query())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[{"translations":[{"text":"Hello"}]}]`))}, nil
	})}
	got, err := Translate(context.Background(), "你好", "en")
	if err != nil || got != "Hello" {
		t.Fatalf("legacy translation = %q, error %v", got, err)
	}
}
