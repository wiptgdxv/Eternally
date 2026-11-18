package main

import (
	"slices"
	"strings"
	"unicode"
)

type literalRange struct{ start, end int }

func literalPrefix(pinyin string, delimiter rune) bool {
	if pinyin == "" || strings.ContainsRune(pinyin, '\'') {
		return false
	}
	switch delimiter {
	case '@':
		// A standalone @ can still follow a selected Chinese word.
		return !numericCode(pinyin)
	case '.':
		return pinyin == "www"
	case ':':
		return pinyin == "http" || pinyin == "https"
	}
	return false
}

func (d *draft) appendLiteral(value string) {
	start := len(d.Chinese)
	d.Chinese += value
	d.literals = slices.Clone(d.literals)
	if n := len(d.literals); n > 0 && d.literals[n-1].end == start {
		d.literals[n-1].end = len(d.Chinese)
	} else {
		d.literals = append(d.literals, literalRange{start, len(d.Chinese)})
	}
}

func (d *draft) trimLiterals() {
	n := len(d.Chinese)
	spans := d.literals
	for len(spans) > 0 && spans[len(spans)-1].start >= n {
		spans = spans[:len(spans)-1]
	}
	if len(spans) > 0 && spans[len(spans)-1].end > n {
		spans = slices.Clone(spans)
		spans[len(spans)-1].end = n
	}
	d.literals = spans
}

type translationPart struct {
	text               string
	translate, literal bool
}

func (d draft) translationParts() []translationPart {
	// Preserve tagged literal spans byte for byte, including their punctuation and case.
	if len(d.literals) == 0 {
		return []translationPart{{text: d.Chinese, translate: true}}
	}
	parts := make([]translationPart, 0, 2*len(d.literals)+1)
	position := 0
	for _, span := range d.literals {
		if span.start > position {
			parts = append(parts, languagePart(d.Chinese[position:span.start]))
		}
		parts = append(parts, translationPart{text: d.Chinese[span.start:span.end], literal: true})
		position = span.end
	}
	if position < len(d.Chinese) {
		parts = append(parts, languagePart(d.Chinese[position:]))
	}
	return parts
}

func languagePart(text string) translationPart {
	return translationPart{text: text, translate: strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) })}
}
