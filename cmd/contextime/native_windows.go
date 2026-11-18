package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"github.com/Microsoft/go-winio"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"golang.org/x/sys/windows"
)

type nativeUpdate struct {
	update panelUpdate
	owner  uint64
	pick   func(string, choice)
	browse func(string, uint32)
}

// Only the last control's inactive draft survives a native pipe reconnect.
var parkedDrafts struct {
	sync.Mutex
	target target
	drafts [2]draft
}

type candidateClick struct {
	pinyin string
	item   choice
	key    uint32
}

const (
	commitRetry          = "上屏失败，按回车重试"
	commitPartial        = "文字可能只写入了一部分，请检查输入框"
	keyToggleTranslation = editKeyBase + 0x77 // Ctrl+, or Ctrl+F8
	keyNextLanguage      = editKeyBase + 0x78 // Ctrl+F9
	keyCopyTranslation   = editKeyBase + 0x7c // Internal action, never a physical key.
	keyPasteSource       = editKeyBase + 0x56
)

var targetLanguages = [...]struct{ code, name string }{
	{"en", "英语"}, {"ja", "日语"}, {"ko", "韩语"}, {"fr", "法语"},
	{"de", "德语"}, {"es", "西班牙语"}, {"pt", "葡萄牙语（巴西）"}, {"ru", "俄语"},
	{"en-GB", "英式英语"}, {"it", "意大利语"}, {"pt-PT", "葡萄牙语（葡萄牙）"},
	{"ar", "阿拉伯语"}, {"hi", "印地语"}, {"th", "泰语"}, {"vi", "越南语"},
	{"id", "印尼语"}, {"nl", "荷兰语"}, {"pl", "波兰语"}, {"tr", "土耳其语"},
	{"sv", "瑞典语"}, {"uk", "乌克兰语"}, {"zh-Hans", "中文"},
}

type inputMode struct {
	language              int
	sourceLanguage        string
	translationEngine     string
	offlineOnlineFallback bool
	chinese               bool
	direct                bool
	ordinaryUsesChinese   bool
	ordinaryLanguage      string
}

func (m inputMode) route() translationRoute {
	return translationRoute{language: targetLanguages[m.language].code, sourceLanguage: m.sourceCode(), engine: m.translationEngine, onlineFallback: m.offlineOnlineFallback}
}

func sourceLanguageName(code string) string {
	if code == "auto" {
		return "自动检测"
	}
	if code == "" {
		code = "zh-Hans"
	}
	for _, language := range targetLanguages {
		if language.code == code {
			return language.name
		}
	}
	return code
}

func (m inputMode) sourceCode() string {
	if m.sourceLanguage == "" || m.translationEngine == engineOffline {
		return "zh-Hans"
	}
	return m.sourceLanguage
}

func (m inputMode) rawSource() bool {
	return !m.chinese && !m.direct && m.sourceCode() != "zh-Hans" && m.sourceCode() != "ja"
}

func (m inputMode) targetName() string {
	return targetLanguages[m.language].name
}

func (m inputMode) label() string {
	if m.chinese {
		return sourceLanguageName(m.ordinaryCode()) + " · 普通输入"
	}
	return sourceLanguageName(m.sourceCode()) + " → " + m.targetName()
}

// Keep the user's selection across applications while the backend is running.
var currentInputMode atomic.Pointer[inputMode]
var lastTranslation atomic.Pointer[string]

func nativeReply(handled bool, units []uint16) []byte {
	reply := make([]byte, 8+len(units)*2)
	if handled {
		binary.LittleEndian.PutUint32(reply[:4], 1)
	}
	binary.LittleEndian.PutUint32(reply[4:8], uint32(len(units)))
	for i, unit := range units {
		binary.LittleEndian.PutUint16(reply[8+i*2:], unit)
	}
	return reply
}

func modeQueryReply(mode *inputMode) []byte {
	if mode == nil {
		return nativeReply(false, nil)
	}
	// The legacy flag only controls direct input. New TSF clients read the
	// translation badge and tooltip from the optional text payload.
	badge := []rune(mode.targetName())[0]
	if mode.chinese {
		badge = []rune(sourceLanguageName(mode.ordinaryCode()))[0]
	}
	payload := string(badge) + "\x00" + mode.label()
	if mode.rawSource() {
		payload += "\x001"
	} else if !mode.chinese {
		payload += "\x002"
	}
	return nativeReply(mode.direct, utf16.Encode([]rune(payload)))
}

func latestTranslation() string {
	if text := lastTranslation.Load(); text != nil {
		return *text
	}
	return ""
}

// One stream corresponds to one native text service focus session. Closing
// that stream discards its draft, including any unacknowledged commit.
func nativeListener() (net.Listener, error) {
	var session uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &session); err != nil {
		return nil, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(`\\.\pipe\ContextIME-TSF-v1-`+strconv.FormatUint(uint64(session), 10), &winio.PipeConfig{
		SecurityDescriptor: fmt.Sprintf("D:P(A;;GA;;;%s)", user.User.Sid.String()),
		InputBufferSize:    4096, OutputBufferSize: 4096,
	})
}

func serveNative(listener net.Listener, publish func(nativeUpdate)) error {
	var owner uint64
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		owner++
		go nativeSession(conn, owner, publish)
	}
}

func nativeSession(conn net.Conn, owner uint64, deliver func(nativeUpdate)) {
	defer conn.Close()
	defer deliver(nativeUpdate{owner: owner})
	done := make(chan struct{})
	defer close(done)
	clicks := make(chan candidateClick, 1)
	sendClick := func(click candidateClick) {
		select {
		case clicks <- click:
		case <-done:
		default:
		}
	}
	publish := func(update nativeUpdate) {
		update.pick = func(pinyin string, item choice) {
			sendClick(candidateClick{pinyin: pinyin, item: item})
		}
		update.browse = func(pinyin string, key uint32) {
			sendClick(candidateClick{pinyin: pinyin, key: key})
		}
		deliver(update)
	}
	requests := make(chan [16]byte)
	go func() {
		defer close(requests)
		for {
			var request [16]byte
			if _, err := io.ReadFull(conn, request[:]); err != nil {
				return
			}
			select {
			case requests <- request:
			case <-done:
				return
			}
		}
	}()
	var state draft
	var parked [2]draft
	ordinaryDrafts := make(map[string]draft)
	var autoSubmit bool
	var mutex sync.Mutex
	var cancel context.CancelFunc
	var cancelCandidates context.CancelFunc
	var closed bool
	defer func() {
		mutex.Lock()
		closed = true
		if cancel != nil {
			cancel()
		}
		if cancelCandidates != nil {
			cancelCandidates()
		}
		mutex.Unlock()
	}()
	var awaitingAck bool
	var afterRawDigit bool
	var pendingText string
	var mode inputMode
	if saved := currentInputMode.Load(); saved != nil {
		mode = *saved
	}
	var original target
	var caret anchor
	var draftAnchor anchor
	var accessibleCaret anchor
	var lastAccessibilityProbe time.Time
	var waitingForAccessibility bool
	publishDraft := func() {
		if waitingForAccessibility && !draftAnchor.caret && state.active() {
			return
		}
		publish(nativeUpdate{owner: owner, update: panelUpdate{state: state, anchor: draftAnchor, input: mode}})
	}
	probeAccessibility := func(destination target) {
		const interval = 200 * time.Millisecond
		const firstFrameDelay = 100 * time.Millisecond
		started := time.Now()
		if started.Sub(lastAccessibilityProbe) < interval || !automationBusy.CompareAndSwap(false, true) {
			return
		}
		lastAccessibilityProbe = started
		waitingForAccessibility = !draftAnchor.caret
		if waitingForAccessibility {
			time.AfterFunc(firstFrameDelay, func() {
				mutex.Lock()
				defer mutex.Unlock()
				if !closed && !mode.direct && original == destination && lastAccessibilityProbe == started && waitingForAccessibility {
					waitingForAccessibility = false
					publishDraft()
				}
			})
		}
		go func() {
			position, found := accessibilityCaret(destination)
			automationBusy.Store(false)
			mutex.Lock()
			defer mutex.Unlock()
			if closed || original != destination || lastAccessibilityProbe != started {
				return
			}
			waited := waitingForAccessibility
			waitingForAccessibility = false
			if mode.direct || !state.active() || foreground() != destination {
				return
			}
			if found && (!draftAnchor.caret || draftAnchor.accessible) && draftAnchor != position {
				accessibleCaret, draftAnchor = position, position
				publishDraft()
			} else if waited {
				publishDraft()
			}
		}()
	}
	changeMode := func(next inputMode) {
		if cancel != nil {
			cancel()
			cancel = nil
		}
		if mode.chinese {
			ordinaryDrafts[mode.ordinaryCode()] = state
		}
		if mode.compositionCode() != next.compositionCode() && cancelCandidates != nil {
			cancelCandidates()
			cancelCandidates = nil
		}
		if mode.chinese != next.chinese {
			if cancelCandidates != nil {
				cancelCandidates()
				cancelCandidates = nil
			}
			parkedDrafts.Lock()
			if parkedDrafts.target == original {
				parked = parkedDrafts.drafts
			}
			if mode.chinese {
				parked[1], state = state, parked[0]
				parked[0] = draft{}
			} else {
				parked[0], state = state, parked[1]
				parked[1] = draft{}
			}
			parkedDrafts.target, parkedDrafts.drafts = original, parked
			parkedDrafts.Unlock()
		}
		if next.chinese && (!mode.chinese || mode.ordinaryCode() != next.ordinaryCode()) {
			if saved, ok := ordinaryDrafts[next.ordinaryCode()]; ok {
				state = saved
			} else if state.LanguageCode != next.ordinaryCode() {
				state = draft{}
			}
		}
		if mode.sourceCode() != next.sourceCode() {
			state.finishPinyin()
			state.SourceTail = 0
			state.Literal, state.autoLiteral = false, false
			state.literals, state.words = nil, nil
		}
		mode = next
		state.LanguageCode = mode.compositionCode()
		autoSubmit, pendingText = false, ""
		state.English, state.TranslationStatus = "", ""
		if state.CommitStatus != commitPartial {
			state.CommitStatus = ""
		}
	}
	submit := func(value string, destination target) {
		if len(utf16.Encode([]rune(value))) > 512 {
			state.CommitStatus = "文字过长，请分段输入"
			return
		}
		pendingText, awaitingAck = value, true
		state.CommitStatus = "正在上屏…"
		go func() {
			deadline := time.Now().Add(750 * time.Millisecond)
			for modifiersDown() && time.Now().Before(deadline) {
				time.Sleep(15 * time.Millisecond)
			}
			mutex.Lock()
			defer mutex.Unlock()
			if closed || original != destination || !awaitingAck || pendingText != value {
				return
			}
			switch insertUnicode(value, destination) {
			case 1:
				rememberDraft(state)
				state = state.afterCommit()
			case -1:
				state.CommitStatus = commitPartial
			default:
				state.CommitStatus = commitRetry
			}
			awaitingAck = false
			publishDraft()
		}()
	}
	for {
		var request [16]byte
		var clicked *candidateClick
		select {
		case next, ok := <-requests:
			if !ok {
				return
			}
			request = next
		case selection := <-clicks:
			clicked = &selection
		}
		method := binary.LittleEndian.Uint32(request[:4])
		if method == 9 || method == 11 { // Mode query and settings from the TSF taskbar item.
			reply := nativeReply(true, nil)
			if method == 9 {
				reply = modeQueryReply(currentInputMode.Load())
			}
			_ = conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
			if _, err := conn.Write(reply); err != nil {
				return
			}
			if method == 11 {
				mygo.RunOnMain(showSettingsWindow)
			}
			continue
		}
		key := binary.LittleEndian.Uint32(request[4:8])
		if (method == 1 || method == 2) && configurableShortcut(key) {
			key = configuredShortcut(key)
			if key == 0 {
				_ = conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
				if _, err := conn.Write(make([]byte, 8)); err != nil {
					return
				}
				continue
			}
		}
		repeated := binary.LittleEndian.Uint32(request[8:12]) != 0
		location := currentAnchor()
		mutex.Lock()
		focusChanged := location.target != original
		if clicked != nil && (focusChanged || mode.direct || awaitingAck || state.CommitStatus == commitPartial ||
			clicked.pinyin != state.Pinyin || (clicked.key == 0 && !slices.Contains(state.candidates(), clicked.item))) {
			mutex.Unlock()
			continue
		}
		if focusChanged {
			if saved := currentInputMode.Load(); saved != nil {
				mode = *saved
			}
			if cancel != nil {
				cancel()
				cancel = nil
			}
			if cancelCandidates != nil {
				cancelCandidates()
				cancelCandidates = nil
			}
			state, original, awaitingAck = draft{}, location.target, false
			afterRawDigit = false
			parked, autoSubmit, pendingText = [2]draft{}, false, ""
			ordinaryDrafts = make(map[string]draft)
			caret, draftAnchor, accessibleCaret = anchor{}, anchor{}, anchor{}
			lastAccessibilityProbe, waitingForAccessibility = time.Time{}, false
		}
		if !state.active() || location.caret || draftAnchor.target != original {
			draftAnchor = location
		}
		if caret.caret {
			draftAnchor = caret
		} else if state.active() && !location.caret && accessibleCaret.caret {
			draftAnchor = accessibleCaret
		}
		if !state.active() {
			accessibleCaret = anchor{}
			lastAccessibilityProbe, waitingForAccessibility = time.Time{}, false
		}
		handled := false
		commit := ""
		retry := ""
		var redraw bool
		var notice string
		var clipboardText string
		previous := state.Chinese
		previousPinyin := state.Pinyin
		retranslate := false
		if saved := currentInputMode.Load(); saved != nil && !awaitingAck {
			if *saved != mode {
				changeMode(*saved)
				retranslate, redraw = true, true
			}
		}
		state.LanguageCode = mode.compositionCode()
		if (method == 1 || method == 2) && !mode.direct && !mode.rawSource() {
			key = state.pagingKey(key, settingsSnapshot().Paging)
		}
		symbol := draftSymbol(key)
		punctuation := state.localizedSymbol(symbol)
		idleChinesePunctuation := mode.chinese && !state.active() && punctuation != symbol &&
			!(symbol == '.' && afterRawDigit)
		handles := func() bool {
			switch key {
			case keyToggleTranslation:
				return true
			case keyNextLanguage:
				return nextTargetLanguage(mode.language, settingsSnapshot()) != mode.language
			case keyCopyTranslation:
				return !mode.chinese && !mode.direct && (state.English != "" || latestTranslation() != "")
			case keyPasteSource:
				return !mode.chinese && !mode.direct
			}
			if mode.rawSource() {
				return state.handlesSource(key)
			}
			return !mode.direct && (idleChinesePunctuation || state.handles(key))
		}
		switch method {
		case 0: // Mouse selection is local to the candidate window.
			if clicked == nil {
				mutex.Unlock()
				return
			}
			if clicked.key != 0 {
				state.press(clicked.key, false)
				redraw = true
			} else if len([]rune(state.Chinese)) < 80 {
				state.selectWord(clicked.item)
				state.Navigated = false
				state.CommitStatus, state.TranslationStatus = "", ""
				autoSubmit = false
				if mode.chinese && state.Pinyin == "" {
					pendingText, retry = state.Chinese, state.Chinese
				}
				redraw = true
			}
		case 1:
			handled = original.window != 0 && (awaitingAck && key != keyCopyTranslation || handles())
			// Plain digits pass through TSF, including the decimal point after them.
			if symbol != '.' {
				afterRawDigit = !handled && key >= '0' && key <= '9'
			}
		case 2:
			if key == keyCopyTranslation {
				handled = original.window != 0 && handles()
				if handled && !repeated {
					clipboardText = state.English
					if clipboardText == "" {
						clipboardText = latestTranslation()
					}
				}
				break
			}
			handled = original.window != 0 && (awaitingAck || handles())
			if key == keyEnter && state.CommitStatus == commitPartial {
				break
			}
			if handled && !awaitingAck && !(repeated && (key == keyEnter || key == keyToggleTranslation || key == keyNextLanguage)) {
				if key == keyToggleTranslation || key == keyNextLanguage {
					selected := mode
					if key == keyToggleTranslation {
						selected.toggleTranslation()
					} else {
						selected.language = nextTargetLanguage(mode.language, settingsSnapshot())
						selected.syncOrdinaryLanguage()
					}
					if selected == mode {
						break
					}
					changeMode(selected)
					currentInputMode.Store(&selected)
					rememberModeSettings(selected)
					afterRawDigit = false
					retranslate, redraw, notice = !mode.direct, state.active() && !mode.direct, mode.label()
					break
				}
				if !mode.chinese && key == keyEnter && state.Pinyin == "" && state.Chinese != "" && state.English == "" && state.CommitStatus == "" {
					// Remember an early Enter while the request is in flight.
					autoSubmit = true
					retranslate = retranslate || (state.TranslationStatus != translationPending && state.TranslationStatus != translationSlow)
					redraw = true
					break
				}
				next := state
				switch {
				case state.CommitStatus == commitRetry && key == keyEnter:
					commit = pendingText
				case idleChinesePunctuation:
					commit = string(punctuation)
					state.Chinese = commit
				case mode.chinese && state.Pinyin == "" && (key == keyEnter || (key == keySpace && !state.Literal)):
					commit = state.Chinese
				case !mode.chinese && key == keyPasteSource:
					data, err := mygo.Clipboard.ReadFormat(transfer.Text)
					if err != nil || len(data) == 0 {
						next.CommitStatus = "剪贴板中没有可粘贴的文字"
					} else {
						next.finishPinyin()
						next.insertSource(string(data))
					}
				case mode.rawSource():
					commit = next.pressSource(key, repeated)
				default:
					commit = next.press(key, repeated)
					if mode.chinese && !next.Literal && next.Pinyin == "" && len(next.Chinese) > len(state.Chinese) {
						state, commit = next, next.Chinese
					}
				}
				if commit == "" {
					state = next
					if state.Chinese != previous || state.Pinyin != previousPinyin || key == keyEscape {
						autoSubmit = false
					}
				} else {
					autoSubmit = false
					pendingText = commit
					awaitingAck = true
					state.CommitStatus = "正在上屏…"
				}
				redraw = true
				if commit == "" && state.CommitStatus != commitRetry {
					pendingText = ""
				}
			}
		case 3, 4, 8:
			if awaitingAck {
				if method == 3 {
					rememberDraft(state)
					state = state.afterCommit()
				} else if method == 4 {
					retry = pendingText
				} else {
					state.CommitStatus = commitPartial
				}
				awaitingAck, redraw = false, true
			}
		case 5, 6, 10: // Shift changes languages only within ordinary input.
			if awaitingAck || original.window == 0 || !mode.chinese {
				break
			}
			if cancel != nil {
				cancel()
				cancel = nil
			}
			afterRawDigit = false
			autoSubmit = false
			next := mode
			if !next.toggleOrdinaryLanguage() {
				break
			}
			changeMode(next)
			selected := mode
			currentInputMode.Store(&selected)
			rememberModeSettings(selected)
			handled, notice = true, mode.label()
			retranslate, redraw = true, !mode.direct
		case 7:
			updated := anchor{target: original, point: point{int32(key), int32(binary.LittleEndian.Uint32(request[12:16]))}, top: int32(binary.LittleEndian.Uint32(request[8:12])), caret: true, tsf: true}
			if original.window != 0 && updated.point.y > updated.top {
				handled = true
				if updated != caret {
					caret, draftAnchor = updated, updated
					redraw = !mode.direct && state.active()
				}
			}
		default:
			mutex.Unlock()
			return
		}
		if os.Getenv("CONTEXTIME_TRACE") == "1" && method != 1 {
			log.Printf("method=%d handled=%t repeat=%t pinyin=%d draft=%d result=%d caret=%t tsf=%t uia=%t anchor=(%d,%d)-%d",
				method, handled, repeated, len(state.Pinyin), len(state.Chinese), len(commit),
				draftAnchor.caret, draftAnchor.tsf, draftAnchor.accessible, draftAnchor.point.x, draftAnchor.point.y, draftAnchor.top)
		}
		units := utf16.Encode([]rune(commit))
		if len(units) > 512 {
			units = nil
			awaitingAck = false
			state.CommitStatus = "文字过长，请分段输入"
		}
		reply := nativeReply(handled, units)
		if clicked == nil {
			_ = conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
			if _, err := conn.Write(reply); err != nil {
				mutex.Unlock()
				return
			}
		}
		if clipboardText != "" {
			go func(text string) {
				if err := copyTranslationText(text); err != nil {
					log.Printf("copy translation: %v", err)
				}
			}(clipboardText)
		}
		if notice != "" {
			publish(nativeUpdate{owner: owner, update: panelUpdate{anchor: location, mode: notice, state: state, input: mode}})
		}
		if retry != "" {
			submit(retry, original)
		}
		translationChanged := previous != state.Chinese || retranslate
		if mode.chinese || mode.direct {
			state.English, state.TranslationStatus = "", ""
		}
		if previousPinyin != state.Pinyin || retranslate {
			if cancelCandidates != nil {
				cancelCandidates()
				cancelCandidates = nil
			}
			if input := state.Pinyin; !mode.direct && len(input) > 1 && !numericCode(input) && !hasASCIIUpper(input) {
				ctx, stop := context.WithTimeout(context.Background(), 350*time.Millisecond)
				cancelCandidates = stop
				compositionLanguage := mode.compositionCode()
				go func() {
					defer stop()
					words := windowsCandidatesForLanguage(ctx, input, compositionLanguage)
					if len(words) == 0 {
						return
					}
					choices := joinChoices(customPhraseChoices(input), mergeWindowsChoices(input, words))
					if compositionLanguage == "ja" {
						choices = mergeJapaneseChoices(input, words)
					}
					mutex.Lock()
					defer mutex.Unlock()
					if ctx.Err() == nil && !closed && !mode.direct && original == location.target && state.Pinyin == input && mode.compositionCode() == compositionLanguage {
						if state.Navigated {
							choices = joinChoices(state.candidates(), choices)
						}
						state.WindowsChoices = choices
						publishDraft()
					}
				}()
			}
		}
		if translationChanged {
			if cancel != nil {
				cancel()
				cancel = nil
			}
			if source := state.Chinese; !mode.direct && !mode.chinese && source != "" && !awaitingAck {
				route := mode.route()
				state.English, state.TranslationStatus = cachedTranslation(state, route), ""
				if state.English != "" {
					text := state.English
					lastTranslation.Store(&text)
					if autoSubmit && state.Pinyin == "" {
						autoSubmit = false
						submit(state.English, original)
					}
				} else {
					state.TranslationStatus = translationPending
					composition, destination := state, original
					ctx, stop := context.WithCancel(context.Background())
					cancel = stop
					current := func() bool {
						if saved := currentInputMode.Load(); saved != nil && (saved.chinese || saved.direct || saved.route() != route) {
							return false
						}
						return !closed && ctx.Err() == nil && !mode.chinese && !mode.direct && mode.route() == route &&
							state.Chinese == source && original == destination && !awaitingAck
					}
					go func() {
						defer stop()
						timer := time.NewTimer(300 * time.Millisecond)
						defer timer.Stop()
						select {
						case <-ctx.Done():
							return
						case <-timer.C:
						}
						slow := time.AfterFunc(1100*time.Millisecond, func() {
							mutex.Lock()
							defer mutex.Unlock()
							if current() && state.TranslationStatus == translationPending {
								state.TranslationStatus = translationSlow
								publishDraft()
							}
						})
						defer slow.Stop()
						result, err := translateDraft(ctx, composition, route)
						mutex.Lock()
						defer mutex.Unlock()
						if !current() {
							return
						}
						state.English = result
						state.TranslationStatus = ""
						if result != "" {
							lastTranslation.Store(&result)
						}
						if err != nil {
							autoSubmit = false
							state.TranslationStatus = translationErrorStatus(err)
						} else if autoSubmit && state.Pinyin == "" {
							autoSubmit = false
							submit(result, destination)
						}
						publishDraft()
					}()
				}
			}
		}
		if redraw && !mode.direct {
			if state.active() && !draftAnchor.tsf && !location.caret {
				probeAccessibility(original)
			}
			publishDraft()
		}
		mutex.Unlock()
	}
}
