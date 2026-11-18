package main

import (
	"strings"
	"unicode/utf8"
)

// Reparse the whole buffer so a pending n or consonant changes naturally as
// the next key arrives, without keeping a second composition state machine.
func romajiKana(input string) string {
	input = strings.ToLower(input)
	var out strings.Builder
	for i := 0; i < len(input); {
		c := input[i]
		if c == 'n' {
			next := byte(0)
			if i+1 < len(input) {
				next = input[i+1]
			}
			if next == '\'' {
				out.WriteRune('ん')
				i += 2
				continue
			}
			if next == 'n' {
				out.WriteRune('ん')
				i++
				if i+1 == len(input) || !strings.ContainsRune("aiueoy", rune(input[i+1])) {
					i++
				}
				continue
			}
			if next == 0 || !strings.ContainsRune("aiueoy", rune(next)) {
				out.WriteRune('ん')
				i++
				continue
			}
		}
		if i+1 < len(input) && strings.ContainsRune("bcdfghjkmpqrstvwxyz", rune(c)) &&
			(c == input[i+1] || c == 't' && strings.HasPrefix(input[i+1:], "ch")) {
			out.WriteRune('っ')
			i++
			continue
		}
		matched := false
		for size := min(4, len(input)-i); size > 0; size-- {
			if kana, ok := romajiSyllables[input[i:i+size]]; ok {
				out.WriteString(kana)
				i += size
				matched = true
				break
			}
		}
		if !matched {
			r, size := utf8.DecodeRuneInString(input[i:])
			out.WriteRune(r)
			i += size
		}
	}
	return out.String()
}

var romajiSyllables = map[string]string{
	"a": "あ", "i": "い", "u": "う", "e": "え", "o": "お",
	"ka": "か", "ki": "き", "ku": "く", "ke": "け", "ko": "こ",
	"sa": "さ", "shi": "し", "si": "し", "su": "す", "se": "せ", "so": "そ",
	"ta": "た", "chi": "ち", "ti": "ち", "tsu": "つ", "tu": "つ", "te": "て", "to": "と",
	"na": "な", "ni": "に", "nu": "ぬ", "ne": "ね", "no": "の",
	"ha": "は", "hi": "ひ", "fu": "ふ", "hu": "ふ", "he": "へ", "ho": "ほ",
	"ma": "ま", "mi": "み", "mu": "む", "me": "め", "mo": "も",
	"ya": "や", "yu": "ゆ", "yo": "よ", "ye": "いぇ",
	"ra": "ら", "ri": "り", "ru": "る", "re": "れ", "ro": "ろ",
	"wa": "わ", "wi": "うぃ", "we": "うぇ", "wo": "を", "wyi": "ゐ", "wye": "ゑ",
	"ga": "が", "gi": "ぎ", "gu": "ぐ", "ge": "げ", "go": "ご",
	"za": "ざ", "ji": "じ", "zi": "じ", "zu": "ず", "ze": "ぜ", "zo": "ぞ",
	"da": "だ", "di": "ぢ", "du": "づ", "de": "で", "do": "ど",
	"ba": "ば", "bi": "び", "bu": "ぶ", "be": "べ", "bo": "ぼ",
	"pa": "ぱ", "pi": "ぴ", "pu": "ぷ", "pe": "ぺ", "po": "ぽ",
	"va": "ゔぁ", "vi": "ゔぃ", "vu": "ゔ", "ve": "ゔぇ", "vo": "ゔぉ",
	"kya": "きゃ", "kyu": "きゅ", "kyo": "きょ", "kye": "きぇ",
	"gya": "ぎゃ", "gyu": "ぎゅ", "gyo": "ぎょ", "gye": "ぎぇ",
	"sha": "しゃ", "shu": "しゅ", "sho": "しょ", "she": "しぇ",
	"sya": "しゃ", "syu": "しゅ", "syo": "しょ", "sye": "しぇ",
	"cha": "ちゃ", "chu": "ちゅ", "cho": "ちょ", "che": "ちぇ",
	"cya": "ちゃ", "cyu": "ちゅ", "cyo": "ちょ", "cye": "ちぇ",
	"tya": "ちゃ", "tyu": "ちゅ", "tyo": "ちょ", "tye": "ちぇ",
	"ja": "じゃ", "ju": "じゅ", "jo": "じょ", "je": "じぇ",
	"jya": "じゃ", "jyu": "じゅ", "jyo": "じょ", "jye": "じぇ",
	"zya": "じゃ", "zyu": "じゅ", "zyo": "じょ", "zye": "じぇ",
	"dya": "ぢゃ", "dyu": "ぢゅ", "dyo": "ぢょ", "dye": "ぢぇ",
	"nya": "にゃ", "nyu": "にゅ", "nyo": "にょ", "nye": "にぇ",
	"hya": "ひゃ", "hyu": "ひゅ", "hyo": "ひょ", "hye": "ひぇ",
	"mya": "みゃ", "myu": "みゅ", "myo": "みょ", "mye": "みぇ",
	"rya": "りゃ", "ryu": "りゅ", "ryo": "りょ", "rye": "りぇ",
	"bya": "びゃ", "byu": "びゅ", "byo": "びょ", "bye": "びぇ",
	"pya": "ぴゃ", "pyu": "ぴゅ", "pyo": "ぴょ", "pye": "ぴぇ",
	"fa": "ふぁ", "fi": "ふぃ", "fe": "ふぇ", "fo": "ふぉ",
	"fya": "ふゃ", "fyu": "ふゅ", "fyo": "ふょ", "fye": "ふぇ",
	"vya": "ゔゃ", "vyu": "ゔゅ", "vyo": "ゔょ",
	"tsa": "つぁ", "tsi": "つぃ", "tse": "つぇ", "tso": "つぉ",
	"tha": "てゃ", "thi": "てぃ", "thu": "てゅ", "the": "てぇ", "tho": "てょ",
	"dha": "でゃ", "dhi": "でぃ", "dhu": "でゅ", "dhe": "でぇ", "dho": "でょ",
	"twa": "とぁ", "twi": "とぃ", "twu": "とぅ", "twe": "とぇ", "two": "とぉ",
	"dwa": "どぁ", "dwi": "どぃ", "dwu": "どぅ", "dwe": "どぇ", "dwo": "どぉ",
	"kwa": "くぁ", "kwi": "くぃ", "kwe": "くぇ", "kwo": "くぉ",
	"gwa": "ぐぁ", "gwi": "ぐぃ", "gwe": "ぐぇ", "gwo": "ぐぉ",
	"qa": "くぁ", "qi": "くぃ", "qu": "く", "qe": "くぇ", "qo": "くぉ",
	"qya": "くゃ", "qyu": "くゅ", "qyo": "くょ",
	"xa": "ぁ", "xi": "ぃ", "xu": "ぅ", "xe": "ぇ", "xo": "ぉ",
	"la": "ぁ", "li": "ぃ", "lu": "ぅ", "le": "ぇ", "lo": "ぉ",
	"xya": "ゃ", "xyu": "ゅ", "xyo": "ょ", "xye": "ぇ",
	"lya": "ゃ", "lyu": "ゅ", "lyo": "ょ", "lye": "ぇ",
	"xtu": "っ", "xtsu": "っ", "ltu": "っ", "ltsu": "っ",
	"xwa": "ゎ", "lwa": "ゎ", "xka": "ゕ", "lka": "ゕ", "xke": "ゖ", "lke": "ゖ",
	"-": "ー",
}

func katakana(input string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'ぁ' && r <= 'ゖ' {
			return r + ('ァ' - 'ぁ')
		}
		return r
	}, input)
}

func japaneseChoices(input string) []choice {
	if input == "" {
		return nil
	}
	kana := romajiKana(input)
	return joinChoices([]choice{{kana, len(input)}, {katakana(kana), len(input)}})
}

func mergeJapaneseChoices(input string, words []string) []choice {
	if input == "" {
		return nil
	}
	system := make([]choice, 0, len(words))
	for _, word := range words {
		if word = strings.TrimSpace(word); word != "" {
			system = append(system, choice{word, len(input)})
		}
	}
	return joinChoices(system, japaneseChoices(input))
}
