package main

import (
	"strings"
	"unicode"
)

var englishPunctuation = map[rune]rune{
	'，': ',', '、': ',', '。': '.', '．': '.', '！': '!', '？': '?', '：': ':', '；': ';',
	'（': '(', '）': ')', '［': '[', '］': ']',
}

func normalizeEnglishPunctuation(text string) string {
	if strings.IndexFunc(text, func(r rune) bool {
		_, ok := englishPunctuation[r]
		return ok
	}) < 0 {
		return text
	}
	return formatEnglishPunctuation(text, false)
}

func sampleTranslation(source string) string {
	remainder := knownWords.Replace(source)
	if remainder == source || strings.TrimFunc(remainder, samplePunctuation) != "" {
		return ""
	}
	// Fixed entries exercise the key flow without relying on a cloud service.
	translated := sampleWords.Replace(source)
	if strings.TrimFunc(source, samplePunctuation) == "世界" {
		translated = strings.Replace(source, "世界", "World", 1)
	}
	return strings.TrimSpace(formatEnglishPunctuation(translated, true))
}

func samplePunctuation(r rune) bool {
	_, fullwidth := englishPunctuation[r]
	return unicode.IsSpace(r) || fullwidth || strings.ContainsRune(`,.!?:;()[]“”‘’"'`, r)
}

func formatEnglishPunctuation(text string, sample bool) string {
	input := []rune(text)
	out := make([]rune, 0, len(input))
	doubleQuote, singleQuote, sentenceStart := false, false, false
	for i, r := range input {
		mark, fullwidth := englishPunctuation[r]
		if !fullwidth {
			mark = r
		}
		if !fullwidth && !sample {
			out = append(out, r)
			continue
		}

		sentenceMark := strings.ContainsRune(",.!?:;", mark)
		closing := strings.ContainsRune(")]”’", mark)
		if sample {
			if sentenceStart && unicode.IsLetter(mark) {
				mark = unicode.ToUpper(mark)
				sentenceStart = false
			}
			if strings.ContainsRune(".!?", mark) {
				sentenceStart = true
			}
			if horizontalSpace(mark) && len(out) > 0 && horizontalSpace(out[len(out)-1]) {
				continue
			}
			switch mark {
			case '"':
				closing = doubleQuote
				doubleQuote = !doubleQuote
			case '\'':
				closing = singleQuote
				singleQuote = !singleQuote
			}
		}
		if sentenceMark || closing {
			for len(out) > 0 && horizontalSpace(out[len(out)-1]) {
				out = out[:len(out)-1]
			}
		}
		out = append(out, mark)
		if !sentenceMark || i+1 >= len(input) {
			continue
		}
		next := input[i+1]
		if !unicode.IsLetter(next) && !unicode.IsDigit(next) {
			continue
		}
		// Decimal points, thousands separators and times stay together.
		if strings.ContainsRune(".,:", mark) && len(out) > 1 && unicode.IsDigit(out[len(out)-2]) && unicode.IsDigit(next) {
			continue
		}
		out = append(out, ' ')
	}
	return string(out)
}

func horizontalSpace(r rune) bool {
	return r == '\t' || unicode.Is(unicode.Zs, r)
}
