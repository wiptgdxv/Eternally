package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Lo uses this Edge endpoint for its default online translation engine.
const translateURL = "https://edge.microsoft.com/translate/translatetext"

var translationClient = &http.Client{
	Timeout: 8 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func Translate(ctx context.Context, source, target string) (string, error) {
	return TranslateBetween(ctx, source, "zh-Hans", target)
}

// TranslateBetween translates text between an explicit pair of languages.
// An empty source language or "auto" lets the online provider detect it.
func TranslateBetween(ctx context.Context, text, sourceLanguage, targetLanguage string) (string, error) {
	result, err := TranslatePartsBetween(ctx, []string{text}, sourceLanguage, targetLanguage)
	if err != nil {
		return "", err
	}
	return result[0], nil
}

func TranslateParts(ctx context.Context, sources []string, target string) ([]string, error) {
	return TranslatePartsBetween(ctx, sources, "zh-Hans", target)
}

// TranslatePartsBetween preserves part order while translating one language pair.
// An empty source language or "auto" requests automatic source detection.
func TranslatePartsBetween(ctx context.Context, sources []string, sourceLanguage, targetLanguage string) ([]string, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	parameters := url.Values{"isEnterpriseClient": {"false"}, "to": {targetLanguage}}
	if sourceLanguage != "" && sourceLanguage != "auto" {
		parameters.Set("from", sourceLanguage)
	}
	body, err := json.Marshal(sources)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, translateURL+"?"+parameters.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := translationClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("translation service returned HTTP %d", response.StatusCode)
	}
	var result []struct {
		Translations []struct{ Text string }
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, err
	}
	if len(result) != len(sources) {
		return nil, errors.New("unexpected number of translation results")
	}
	translated := make([]string, len(result))
	for i, part := range result {
		if len(part.Translations) > 0 {
			translated[i] = strings.TrimSpace(part.Translations[0].Text)
		}
		if translated[i] == "" {
			return nil, errors.New("empty translation")
		}
	}
	return translated, nil
}
