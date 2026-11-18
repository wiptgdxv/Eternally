package main

import (
	"strings"
	"unicode"
)

const (
	keyBackspace        = 8
	keyEnter            = 13
	keyEscape           = 27
	keySpace            = 32
	keyPageUp           = 33
	keyPageDown         = 34
	keyLeft             = 37
	keyUp               = 38
	keyRight            = 39
	keyDown             = 40
	keyDelete           = 46
	pageSize            = 5
	symbolKeyBase       = 0x10000 // Matches SYMBOL_KEY_BASE in the TSF service.
	editKeyBase         = 0x20000 // Matches EDIT_KEY_BASE in the TSF service.
	keyLiteral          = editKeyBase + keySpace
	keyControlPeriod    = editKeyBase + 0xbe
	keyToggleCandidates = editKeyBase + 0x100
	expandedPageSize    = pageSize * 4
)

var sampleWords = strings.NewReplacer("你好", "Hello ", "拟好", "Drafted ", "世界", "world ", "时节", "season ", "视界", "field of view ")
var knownWords = strings.NewReplacer("你好", "", "拟好", "", "世界", "", "时节", "", "视界", "")

type draft struct {
	LanguageCode      string
	Chinese           string
	Pinyin            string
	English           string
	Selected          int
	Submitted         string
	TranslationStatus string
	CommitStatus      string
	WindowsChoices    []choice
	Navigated         bool
	Expanded          bool
	PinyinTail        int
	SourceTail        int // Rune count after the cursor for non-pinyin source text.
	Literal           bool
	autoLiteral       bool
	literals          []literalRange
	quotes            quoteState
	words             *selectedWord
}

type selectedWord struct {
	before, after, spelling, separator, word string
	previous                                 *selectedWord
	literal                                  bool
}

func (d *draft) selectWord(c choice) {
	spelling := d.Pinyin[:c.consume]
	before := d.Chinese
	literal := (d.LanguageCode == "" || d.LanguageCode == "zh-Hans") && isInlineEnglishCandidate(d.Pinyin, c)
	if literal {
		d.appendLiteral(c.text)
	} else {
		d.Chinese += c.text
	}
	rest := d.Pinyin[c.consume:]
	d.Pinyin = strings.TrimLeft(rest, "'")
	d.words = &selectedWord{before: before, after: d.Chinese, spelling: spelling, separator: rest[:len(rest)-len(d.Pinyin)], word: c.text, previous: d.words, literal: literal}
	d.PinyinTail, d.Selected = 0, 0
	d.Expanded = false
	d.WindowsChoices = nil
}

func (d *draft) finishPinyin() {
	for d.Pinyin != "" {
		choices := d.candidates()
		if len(choices) == 0 {
			d.Chinese += d.Pinyin
			d.Pinyin = ""
			break
		}
		d.selectWord(choices[min(d.Selected, len(choices)-1)])
	}
	d.PinyinTail, d.Selected = 0, 0
}

func (d draft) pinyinCursor() int { return len(d.Pinyin) - d.PinyinTail }

func (d draft) candidates() []choice {
	if d.LanguageCode == "ja" {
		if len(d.WindowsChoices) > 0 {
			return d.WindowsChoices
		}
		return japaneseChoices(d.Pinyin)
	}
	if symbolCode(d.Pinyin) {
		symbols := []rune(symbolGroups[d.Pinyin[1]-'1'])
		choices := make([]choice, len(symbols))
		for i, symbol := range symbols {
			choices[i] = choice{string(symbol), len(d.Pinyin)}
		}
		return choices
	}
	if numericCode(d.Pinyin) {
		return numberChoices(d.Pinyin)
	}
	if hasASCIIUpper(d.Pinyin) {
		return withEnglishCandidate(d.Pinyin, nil)
	}
	custom := customPhraseChoices(d.Pinyin)
	if len(d.WindowsChoices) > 0 {
		if len(custom) == 0 {
			return withEnglishCandidate(d.Pinyin, d.WindowsChoices)
		}
		return joinChoices(custom, withEnglishCandidate(d.Pinyin, d.WindowsChoices))
	}
	return joinChoices(custom, withEnglishCandidate(d.Pinyin, joinChoices(custom, preferLearned(d.Pinyin, lookupChoices(d.Pinyin)))))
}

func hasASCIIUpper(input string) bool {
	for i := range input {
		if input[i] >= 'A' && input[i] <= 'Z' {
			return true
		}
	}
	return false
}
func (d draft) active() bool { return d.Chinese != "" || d.Pinyin != "" || d.Literal }

func (d draft) pageStart() int { return d.Selected / pageSize * pageSize }

func (d draft) pagingKey(key uint32, paging string) uint32 {
	if d.Literal || d.Pinyin == "" || numericCode(d.Pinyin) {
		return key
	}
	var up, down uint32
	switch paging {
	case "brackets":
		up, down = '[', ']'
	case "minus-equals":
		up, down = '-', '='
	default:
		return key
	}
	if key == symbolKeyBase+up && len(d.candidates()) > pageSize {
		return keyPageUp
	}
	if key == symbolKeyBase+down && len(d.candidates()) > pageSize {
		return keyPageDown
	}
	return key
}

func (d draft) handles(key uint32) bool {
	if key == keyToggleCandidates {
		return len(d.candidates()) > pageSize
	}
	if key == keyLiteral {
		return true
	}
	if key == editKeyBase+keyLeft || key == editKeyBase+keyRight || key == keyDelete {
		return d.Pinyin != ""
	}
	if key >= symbolKeyBase {
		if symbol := draftSymbol(key); symbol >= 'A' && symbol <= 'Z' && (d.LanguageCode == "" || d.LanguageCode == "zh-Hans") {
			return true
		}
		return d.active() && draftSymbol(key) != 0
	}
	if key >= 'A' && key <= 'Z' {
		return true
	}
	if key >= '0' && key <= '9' {
		return d.active()
	}
	switch key {
	case keyEnter, keyEscape, keyBackspace:
		return d.active()
	case keySpace:
		return d.Literal || len(d.candidates()) > 0
	case keyLeft, keyRight, keyUp, keyDown, keyPageUp, keyPageDown:
		return len(d.candidates()) > 0
	}
	return false
}

func (d *draft) press(key uint32, repeated bool) (commit string) {
	if !d.handles(key) || (key == keyEnter && repeated) {
		return ""
	}
	d.Submitted = ""
	d.CommitStatus = ""
	previousChinese := d.Chinese
	previousPinyin := d.Pinyin
	cursor := d.pinyinCursor()
	symbol := draftSymbol(key)
	numberChoice := !d.Literal && key >= '1' && key <= '5' && d.pageStart()+int(key-'1') < len(d.candidates())
	numericInput := !d.Literal && (d.Pinyin == "v" || numericCode(d.Pinyin))
	switch {
	case key == keyToggleCandidates || (key == keyDown && !d.Expanded && len(d.candidates()) > pageSize):
		if !repeated {
			d.Expanded = !d.Expanded
			if d.Expanded {
				d.WindowsChoices = d.candidates()
				d.Navigated = true
			}
		}
	case numericInput && key >= '0' && key <= '9' && !(d.Navigated && numberChoice):
		if len(d.Pinyin) < 96 {
			d.Pinyin = d.Pinyin[:cursor] + string(rune(key)) + d.Pinyin[cursor:]
		}
		d.Selected = 0
	case numericCode(d.Pinyin) && symbol == '.':
		if len(d.Pinyin) < 96 && strings.Count(d.Pinyin, ".") < 2 {
			d.Pinyin = d.Pinyin[:cursor] + "." + d.Pinyin[cursor:]
		}
		d.Selected = 0
	case key == keyLiteral:
		if !repeated {
			d.finishPinyin()
			d.Literal, d.autoLiteral = !d.Literal, false
		}
	case !d.Literal && symbol != 0 && literalPrefix(d.Pinyin, symbol):
		d.appendLiteral(d.Pinyin + string(symbol))
		d.Pinyin = ""
		d.PinyinTail, d.Selected = 0, 0
		d.Literal, d.autoLiteral = true, true
	case d.Literal && key >= 'A' && key <= 'Z':
		d.appendLiteral(string(rune(key + 'a' - 'A')))
	case d.Literal && key == keySpace:
		if d.autoLiteral {
			d.Literal, d.autoLiteral = false, false
			d.Chinese += " "
		} else {
			d.appendLiteral(" ")
		}
	case d.Literal && (symbol != 0 || key >= '0' && key <= '9'):
		if symbol == 0 {
			symbol = rune(key)
		}
		d.appendLiteral(string(symbol))
	case key >= '0' && key <= '9' && !numberChoice:
		d.finishPinyin()
		d.Chinese += string(rune(key))
	case key == editKeyBase+keyLeft:
		d.PinyinTail = min(len(d.Pinyin), d.PinyinTail+1)
	case key == editKeyBase+keyRight:
		d.PinyinTail = max(0, d.PinyinTail-1)
	case symbol == '\'' && d.Pinyin != "":
		if cursor > 0 && d.Pinyin[cursor-1] != '\'' &&
			(cursor == len(d.Pinyin) || d.Pinyin[cursor] != '\'') && len(d.Pinyin) < 96 {
			d.Pinyin = d.Pinyin[:cursor] + "'" + d.Pinyin[cursor:]
			d.Selected = 0
		}
	case symbol >= 'A' && symbol <= 'Z' && (d.LanguageCode == "" || d.LanguageCode == "zh-Hans"):
		if len(d.Pinyin) < 96 {
			d.Pinyin = d.Pinyin[:cursor] + string(symbol) + d.Pinyin[cursor:]
		}
		d.Selected = 0
	case symbol != 0:
		d.finishPinyin()
		d.Chinese += string(d.localizedSymbol(symbol))
		d.PinyinTail = 0
		d.Selected = 0
	case key >= 'A' && key <= 'Z':
		if numericCode(d.Pinyin) {
			d.finishPinyin()
			cursor = 0
		}
		if len(d.Pinyin) < 96 {
			d.Pinyin = d.Pinyin[:cursor] + string(rune(key+'a'-'A')) + d.Pinyin[cursor:]
		}
		d.Selected = 0
	case key == keyEscape:
		if d.Expanded {
			d.Expanded = false
		} else if numericCode(d.Pinyin) {
			d.Pinyin = ""
			d.PinyinTail, d.Selected = 0, 0
		} else {
			*d = draft{LanguageCode: d.LanguageCode, quotes: d.quotes}
		}
	case key == keyBackspace:
		if cursor > 0 {
			d.Pinyin = d.Pinyin[:cursor-1] + d.Pinyin[cursor:]
		} else if d.words != nil && !d.Literal && d.words.after == d.Chinese {
			spelling := d.words.spelling
			tail := len(d.Pinyin)
			if d.Pinyin != "" {
				spelling += d.words.separator + d.Pinyin
				tail += len(d.words.separator)
			}
			d.Chinese, d.Pinyin, d.words = d.words.before, spelling, d.words.previous
			d.trimLiterals()
			d.PinyinTail = min(tail, len(spelling))
		} else if r := []rune(d.Chinese); d.Pinyin == "" && len(r) > 0 {
			d.Chinese = string(r[:len(r)-1])
			d.trimLiterals()
			for d.words != nil && !strings.HasPrefix(d.Chinese, d.words.after) {
				d.words = d.words.previous
			}
		}
		d.Selected = 0
	case key == keyDelete:
		if cursor < len(d.Pinyin) {
			d.Pinyin = d.Pinyin[:cursor] + d.Pinyin[cursor+1:]
			d.PinyinTail--
			d.Selected = 0
		}
	case key == keyPageUp || key == keyPageDown:
		choices := d.candidates()
		n := len(choices)
		size := pageSize
		if d.Expanded {
			size = expandedPageSize
		}
		pages := (n + size - 1) / size
		step := 1
		if key == keyPageUp {
			step = -1
		}
		d.Selected = (d.Selected/size + step + pages) % pages * size
		d.Navigated = d.Navigated || pages > 1
		if d.Navigated {
			d.WindowsChoices = choices
		}
	case key == keyLeft || key == keyRight || key == keyUp || key == keyDown:
		step := 1
		if d.Expanded && (key == keyUp || key == keyDown) {
			step = pageSize
		}
		if key == keyLeft || key == keyUp {
			step = -step
		}
		choices := d.candidates()
		n := len(choices)
		if d.Expanded && (key == keyUp || key == keyDown) {
			if next := d.Selected + step; next >= 0 && next < n {
				d.Selected = next
			}
		} else {
			d.Selected = (d.Selected + step + n) % n
		}
		d.Navigated = d.Navigated || n > 1
		if d.Navigated {
			d.WindowsChoices = choices
		}
	default:
		choices := d.candidates()
		if len(choices) > 0 {
			index := min(d.Selected, len(choices)-1)
			if numberChoice {
				index = d.pageStart() + int(key-'1')
			}
			if len([]rune(d.Chinese)) < 80 {
				d.selectWord(choices[index])
			}
		} else if key == keyEnter && d.Pinyin == "" && d.English != "" {
			commit := d.English
			*d = draft{LanguageCode: d.LanguageCode, Submitted: commit, quotes: d.quotes.after(d.Chinese)}
			return commit
		}
	}
	if d.Chinese != previousChinese {
		d.English = sampleTranslation(d.Chinese)
		if d.words != nil && numericCode(d.words.spelling) && d.words.word == d.Chinese {
			d.English = d.Chinese
		}
		d.TranslationStatus = ""
	}
	if d.Pinyin != previousPinyin {
		d.WindowsChoices = nil
		d.Navigated = false
		d.Expanded = false
	}
	return ""
}

var chinesePunctuation = map[rune]rune{
	',': '，', '.': '。', '?': '？', '!': '！', ':': '：', ';': '；',
	'(': '（', ')': '）', '[': '【', ']': '】', '<': '《', '>': '》', '\\': '、',
}

type quoteState struct {
	singleOpen, doubleOpen bool
}

func (q quoteState) after(text string) quoteState {
	if at := strings.LastIndexAny(text, "‘’『』"); at >= 0 {
		q.singleOpen = strings.HasPrefix(text[at:], "‘") || strings.HasPrefix(text[at:], "『")
	}
	if at := strings.LastIndexAny(text, "“”「」"); at >= 0 {
		q.doubleOpen = strings.HasPrefix(text[at:], "“") || strings.HasPrefix(text[at:], "「")
	}
	return q
}

func (d draft) afterCommit() draft {
	return draft{LanguageCode: d.LanguageCode, quotes: d.quotes.after(d.Chinese)}
}

func (d draft) localizedSymbol(symbol rune) rune {
	if d.Literal || (d.LanguageCode != "" && d.LanguageCode != "zh-Hans" && d.LanguageCode != "ja") {
		return symbol
	}
	if symbol == '.' && len(d.Chinese) > 0 {
		if last := d.Chinese[len(d.Chinese)-1]; last >= '0' && last <= '9' {
			return symbol
		}
	}
	if d.LanguageCode == "ja" {
		switch symbol {
		case ',':
			return '、'
		case '.':
			return '。'
		case '-':
			return 'ー'
		case '[':
			return '「'
		case ']':
			return '」'
		case '"':
			if d.quotes.after(d.Chinese).doubleOpen {
				return '」'
			}
			return '「'
		case '\'':
			if d.quotes.after(d.Chinese).singleOpen {
				return '』'
			}
			return '『'
		}
	}
	switch symbol {
	case '"':
		if d.quotes.after(d.Chinese).doubleOpen {
			return '”'
		}
		return '“'
	case '\'':
		if d.quotes.after(d.Chinese).singleOpen {
			return '’'
		}
		return '‘'
	}
	if converted, ok := chinesePunctuation[symbol]; ok {
		return converted
	}
	return symbol
}

func draftSymbol(key uint32) rune {
	if key < symbolKeyBase+'!' || key > symbolKeyBase+'~' {
		return 0
	}
	r := rune(key - symbolKeyBase)
	if unicode.IsPunct(r) || unicode.IsSymbol(r) || r >= 'A' && r <= 'Z' {
		return r
	}
	return 0
}
