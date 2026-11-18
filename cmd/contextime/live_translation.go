package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"unicode"
	"unicode/utf8"

	"lo-windows/internal/translation"
)

const (
	translationPending = "翻译中…"
	translationSlow    = "翻译响应较慢…"
	translationOffline = "网络连接失败，按回车重试"
	translationTimeout = "翻译超时，按回车重试"
	translationFailed  = "翻译失败，按回车重试"
	translationModel   = "离线模型不可用，请在设置中下载或校验"
	translationRuntime = "离线引擎无法启动，请检查系统运行库"
)

type translationRoute struct {
	language, engine string
	onlineFallback   bool
	sourceLanguage   string
}

func (r translationRoute) sourceCode() string {
	if r.sourceLanguage == "" {
		return "zh-Hans"
	}
	return r.sourceLanguage
}

func (r translationRoute) cacheKey(source string) translationCacheKey {
	engine := r.engine
	if engine == "" {
		engine = engineOnline
	}
	return translationCacheKey{source: source, language: r.language, engine: engine, sourceLanguage: r.sourceCode()}
}

func translationErrorStatus(err error) string {
	var timeout interface{ Timeout() bool }
	var network *net.OpError
	var dns *net.DNSError
	switch {
	case errors.Is(err, translation.ErrOfflineRuntime):
		return translationRuntime
	case errors.Is(err, translation.ErrOfflineUnavailable):
		return translationModel
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return translationTimeout
	case errors.As(err, &network), errors.As(err, &dns):
		return translationOffline
	default:
		return translationFailed
	}
}

func cachedTranslation(d draft, route translationRoute) string {
	if d.words != nil && numericCode(d.words.spelling) && d.words.word == d.Chinese {
		return d.Chinese
	}
	parts := d.translationParts()
	resolved, pending := resolveTranslation(parts, route)
	if len(pending) != 0 {
		return ""
	}
	return formatTranslationParts(parts, resolved, route.language)
}

func resolveTranslation(parts []translationPart, route translationRoute) (resolved []string, pending []int) {
	resolved = make([]string, len(parts))
	for i, part := range parts {
		if !part.translate {
			continue
		}
		source := strings.TrimSpace(part.text)
		if route.language == route.sourceCode() {
			resolved[i] = source
		} else if route.language == "en" && route.sourceCode() == "zh-Hans" {
			resolved[i] = sampleTranslation(source)
		}
		if resolved[i] == "" {
			resolved[i] = sessionTranslations.get(route.cacheKey(source))
		}
		if resolved[i] == "" {
			pending = append(pending, i)
		}
	}
	return resolved, pending
}

func translateDraft(ctx context.Context, d draft, route translationRoute) (string, error) {
	parts := d.translationParts()
	result, err := translateDraftParts(ctx, parts, route)
	if errors.Is(err, translation.ErrOfflineUnavailable) && route.engine == engineOffline && route.onlineFallback && ctx.Err() == nil {
		return translateDraftParts(ctx, parts, translationRoute{language: route.language, sourceLanguage: route.sourceCode(), engine: engineOnline})
	}
	return result, err
}

func translateDraftParts(ctx context.Context, parts []translationPart, route translationRoute) (string, error) {
	resolved, pending := resolveTranslation(parts, route)
	if len(pending) == 0 {
		return formatTranslationParts(parts, resolved, route.language), nil
	}
	sources := make([]string, len(pending))
	for i, index := range pending {
		sources[i] = strings.TrimSpace(parts[index].text)
	}
	translate := translation.TranslatePartsBetween
	if route.engine == engineOffline {
		translate = translation.TranslateOfflinePartsBetween
	}
	results, err := translate(ctx, sources, route.sourceCode(), route.language)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(results) != len(pending) {
		return "", errors.New("unexpected number of translation results")
	}
	for _, result := range results {
		if strings.TrimSpace(result) == "" {
			return "", errors.New("empty translation")
		}
	}
	for i, index := range pending {
		resolved[index] = results[i]
		sessionTranslations.set(route.cacheKey(sources[i]), results[i])
	}
	return formatTranslationParts(parts, resolved, route.language), nil
}

func formatTranslationParts(parts []translationPart, translated []string, language string) string {
	var output strings.Builder
	var last rune
	for i, part := range parts {
		value := part.text
		if part.translate {
			value = preserveBoundarySpace(part.text, translated[i])
		}
		if (language == "en" || language == "en-GB") && !part.literal {
			value = normalizeEnglishPunctuation(value)
		}
		first, _ := utf8.DecodeRuneInString(value)
		if i > 0 && (part.translate || parts[i-1].translate) && wordEdge(last) && wordEdge(first) {
			output.WriteByte(' ')
		}
		output.WriteString(value)
		last, _ = utf8.DecodeLastRuneInString(value)
	}
	return output.String()
}

func preserveBoundarySpace(source, translated string) string {
	left := strings.TrimLeftFunc(source, unicode.IsSpace)
	right := strings.TrimRightFunc(source, unicode.IsSpace)
	return source[:len(source)-len(left)] + translated + source[len(right):]
}

func wordEdge(r rune) bool {
	return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r))
}
