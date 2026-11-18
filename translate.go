package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Lo uses this Edge endpoint for its default online translation engine.
const translateURL = "https://edge.microsoft.com/translate/translatetext?isEnterpriseClient=false&to=en"

var translationClient = &http.Client{
	Timeout: 8 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func translate(ctx context.Context, source string) (string, error) {
	body, err := json.Marshal([]string{source})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, translateURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := translationClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("translation service returned HTTP %d", response.StatusCode)
	}
	var result []struct {
		Translations []struct{ Text string }
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return "", err
	}
	if len(result) > 0 && len(result[0].Translations) > 0 {
		if text := strings.TrimSpace(result[0].Translations[0].Text); text != "" {
			return text, nil
		}
	}
	return "", errors.New("empty translation")
}
