package main

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

const (
	translationActionSize = 26
	translationActionGap  = 3
)

func copyTranslationText(text string) error {
	if text == "" {
		return errors.New("暂无可复制的译文")
	}
	return mygo.Clipboard.Write(transfer.TextData(text))
}

type displayedTranslation struct {
	owner                  uint64
	source, pinyin, result string
	language               int
}

func (p *panel) displayedTranslation() displayedTranslation {
	return displayedTranslation{p.owner, p.state.Chinese, p.state.Pinyin, p.state.English, p.input.language}
}

type translationPanelActions struct {
	copyOf, speechOf           displayedTranslation
	copyLabel, speechError     string
	copyPending                bool
	copyAttempt, speechAttempt uint64
	stopSpeech                 context.CancelFunc
}

func (p *panel) copyDisplayedTranslation(value displayedTranslation) {
	a := &p.translationActions
	if a.copyPending && a.copyOf == value {
		return
	}
	a.copyAttempt++
	attempt := a.copyAttempt
	a.copyOf, a.copyLabel, a.copyPending = value, "", true
	go func() {
		err := copyTranslationText(value.result)
		p.updateTranslationActions(func() {
			if a.copyAttempt != attempt {
				return
			}
			a.copyPending = false
			if p.displayedTranslation() != value {
				return
			}
			a.copyLabel = "已复制译文"
			if err != nil {
				a.copyLabel = "无法访问剪贴板，点击重试"
			}
			time.AfterFunc(1700*time.Millisecond, func() {
				p.updateTranslationActions(func() {
					if a.copyAttempt == attempt {
						a.copyLabel = ""
					}
				})
			})
		})
	}()
}

func (p *panel) speakDisplayedTranslation(value displayedTranslation) {
	a := &p.translationActions
	wasSpeaking := a.stopSpeech != nil && a.speechOf == value
	p.stopTranslationSpeech()
	a.speechOf, a.speechError = value, ""
	if wasSpeaking {
		return
	}
	a.speechAttempt++
	attempt := a.speechAttempt
	ctx, cancel := context.WithCancel(context.Background())
	a.stopSpeech = cancel
	locale := targetLanguages[value.language].code
	go func() {
		defer cancel()
		err := speakTranslationText(ctx, value.result, locale)
		p.updateTranslationActions(func() {
			if a.speechAttempt != attempt {
				return
			}
			a.stopSpeech = nil
			if err == nil || errors.Is(err, context.Canceled) || p.displayedTranslation() != value {
				return
			}
			a.speechError = "无法朗读，点击重试"
			if errors.Is(err, errNoSpeechVoice) {
				a.speechError = "未安装对应的系统语音"
			}
		})
	}()
}

func (p *panel) stopTranslationSpeech() {
	a := &p.translationActions
	if a.stopSpeech != nil {
		a.stopSpeech()
		a.stopSpeech = nil
		a.speechAttempt++
	}
}

func (p *panel) cancelObsoleteSpeech() {
	if a := &p.translationActions; a.stopSpeech != nil && (p.input.chinese || p.input.direct || a.speechOf != p.displayedTranslation()) {
		p.stopTranslationSpeech()
	}
}

type translationIcon uint8

const (
	translationCopy translationIcon = iota
	translationCopied
	translationSpeak
	translationStop
)

func (p *panel) drawTranslationActions(c *ui.Context, value displayedTranslation, colors panelPalette) {
	a := &p.translationActions
	copyLabel, speechLabel := "复制译文", "朗读译文"
	copyIcon, speechIcon := translationCopy, translationSpeak
	copyFailed, speechFailed := false, false
	if a.copyOf == value && a.copyLabel != "" {
		copyLabel = a.copyLabel
		if copyLabel == "已复制译文" {
			copyIcon = translationCopied
		} else {
			copyFailed = true
		}
	}
	if a.speechOf == value {
		switch {
		case a.stopSpeech != nil:
			speechLabel, speechIcon = "停止朗读", translationStop
		case a.speechError != "":
			speechLabel, speechFailed = a.speechError, true
		}
	}
	if drawTranslationAction(c, copyLabel, copyIcon, copyFailed, colors).Clicked() {
		p.copyDisplayedTranslation(value)
	}
	if drawTranslationAction(c, speechLabel, speechIcon, speechFailed, colors).Clicked() {
		p.speakDisplayedTranslation(value)
		c.AnimationFrame()
	}
}

func drawTranslationAction(c *ui.Context, label string, icon translationIcon, failed bool, colors panelPalette) ui.Element {
	button := ui.ButtonBase(c).KeepFocus().Label(label).Tooltip(label).
		Size(translationActionSize, translationActionSize).Shrink(0).Radius(5).Cursor(ui.CursorPointer)
	color := colors.muted
	if button.Hovered() {
		button.Background(colors.hover)
		color = colors.ink
	}
	if failed {
		color = colors.errorText
	} else if icon == translationCopied || icon == translationStop {
		color = colors.accent
	}
	button.Draw(func(p *ui.Painter, r ui.Rect) {
		x, y := r.X+r.W/2, r.Y+r.H/2
		stroke := func(points ...[2]float32) { p.StrokePolyline(points, 1.25, color) }
		switch icon {
		case translationCopy:
			stroke([2]float32{x - 6, y + 3}, [2]float32{x - 6, y - 6}, [2]float32{x + 3, y - 6})
			stroke([2]float32{x - 3, y - 3}, [2]float32{x + 6, y - 3}, [2]float32{x + 6, y + 6}, [2]float32{x - 3, y + 6}, [2]float32{x - 3, y - 3})
		case translationCopied:
			stroke([2]float32{x - 5, y}, [2]float32{x - 1, y + 4}, [2]float32{x + 6, y - 4})
		case translationSpeak:
			stroke([2]float32{x - 6, y - 2}, [2]float32{x - 3, y - 2}, [2]float32{x, y - 5}, [2]float32{x, y + 5}, [2]float32{x - 3, y + 2}, [2]float32{x - 6, y + 2}, [2]float32{x - 6, y - 2})
			stroke([2]float32{x + 3, y - 3}, [2]float32{x + 5, y - 2}, [2]float32{x + 5, y + 2}, [2]float32{x + 3, y + 3})
		case translationStop:
			stroke([2]float32{x - 4, y - 4}, [2]float32{x + 4, y - 4}, [2]float32{x + 4, y + 4}, [2]float32{x - 4, y + 4}, [2]float32{x - 4, y - 4})
		}
	})
	return button
}
