package main

import (
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var settingsWindow *mygo.Window

func showSettingsWindow() {
	refreshOfflineModelStatus()
	if settingsWindow != nil {
		settingsWindow.Show()
		settingsWindow.Focus()
		settingsWindow.Invalidate()
		return
	}
	view := newSettingsView(settingsSnapshot())
	window := mygo.NewWindow(mygo.WindowOptions{
		Title: settingsTitle, Width: 790, Height: 610, MinWidth: 560, MinHeight: 465,
		TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: settingsCaptionHeight,
		BackgroundColor: "light-dark(#EFF2F5, #191D23)", Content: ui.View(view.render),
	})
	settingsWindow, view.window = window, window
	window.OnClosed(func() { settingsWindow = nil })
	if err := window.SetIcon(settingsTrayIcon); err != nil {
		log.Printf("settings icon: %v", err)
	}
	window.Show()
	// A hidden backend launch can suppress its first ShowWindow call.
	const showUnchanged = 0x0040 | 0x0001 | 0x0002 | 0x0004 // show, no size, no move, no Z order
	setPos.Call(window.NativeHandle(), 0, 0, 0, 0, 0, showUnchanged)
	window.Focus()
}

type settingsView struct {
	window       *mygo.Window
	buttonsTheme mygo.ThemeSource
	tab          int

	language, sourceLanguage, ordinary, font, appearance, paging, fuzzy string
	toggle, nextLanguage, copyTranslation                               string
	engine                                                              string
	quickLanguages                                                      [len(targetLanguages)]bool
	offlineOnlineFallback                                               bool
	initial                                                             imeSettings
	saved                                                               bool
	saveError                                                           string
	failedSettings                                                      imeSettings
	dictStatus                                                          string
	dictError                                                           bool

	dictionaryTab                    int
	filter                           string
	words                            []personalWord
	lastFilter                       string
	wordsLoaded                      bool
	phrases                          map[string][]string
	phraseKey, phraseText, phraseOld string
}

var keyboardLabels = []string{"Ctrl + F6", "Ctrl + F7", "Ctrl + F8", "Ctrl + F9", "Ctrl + F10", "Ctrl + F11", "Ctrl + F12", "Ctrl + ,", "Ctrl + ."}
var keyboardCodes = []uint32{editKeyBase + 0x75, editKeyBase + 0x76, keyToggleTranslation, keyNextLanguage, editKeyBase + 0x79, editKeyBase + 0x7a, editKeyBase + 0x7b, shortcutComma, keyControlPeriod}
var copyKeyboardLabels = append([]string{"关闭"}, keyboardLabels...)
var copyKeyboardCodes = append([]uint32{0}, keyboardCodes...)
var appearanceLabels = []string{"跟随系统", "浅色", "深色"}
var appearanceValues = []string{"system", "light", "dark"}
var pagingLabels = []string{"Page Up / Page Down", "[ / ]", "- / ="}
var pagingValues = []string{"page", "brackets", "minus-equals"}
var fontOptions = []string{"12", "13", "14", "15", "16", "17", "18", "19", "20"}
var engineLabels = []string{"在线翻译", "离线中译英"}

var languageLabels = func() []string {
	labels := make([]string, len(targetLanguages))
	for i, language := range targetLanguages {
		labels[i] = language.name
	}
	return labels
}()

var sourceLanguageLabels = append([]string{"自动检测"}, languageLabels...)

func selectedSourceLanguage(label string) string {
	if index := slices.Index(languageLabels, label); index >= 0 {
		return targetLanguages[index].code
	}
	return "auto"
}

func (v *settingsView) swapTranslationLanguages() {
	if v.sourceLanguage != sourceLanguageLabels[0] {
		v.sourceLanguage, v.language = v.language, v.sourceLanguage
	}
}

func newSettingsView(s imeSettings) *settingsView {
	v := &settingsView{
		initial: s, language: languageLabels[s.Language], sourceLanguage: sourceLanguageName(s.SourceLanguage),
		font:       strconv.Itoa(s.CandidateFontSize),
		appearance: appearanceLabels[slices.Index(appearanceValues, s.Appearance)],
		paging:     pagingLabels[slices.Index(pagingValues, s.Paging)],
		ordinary:   ordinaryLanguageLabel(s.effectiveOrdinaryLanguage()), fuzzy: "关闭",
		toggle:                keyboardLabels[slices.Index(keyboardCodes, s.Hotkeys.ToggleTranslation)],
		nextLanguage:          keyboardLabels[slices.Index(keyboardCodes, s.Hotkeys.NextLanguage)],
		copyTranslation:       copyKeyboardLabels[slices.Index(copyKeyboardCodes, s.Hotkeys.CopyTranslation)],
		engine:                engineLabels[0],
		offlineOnlineFallback: s.OfflineOnlineFallback,
	}
	for i := range v.quickLanguages {
		v.quickLanguages[i] = s.QuickLanguages&(1<<i) != 0
	}
	if s.TranslationEngine == engineOffline {
		v.engine = engineLabels[1]
	}
	if s.FuzzyPinyin {
		v.fuzzy = "开启"
	}
	return v
}

func (v *settingsView) render(c *ui.Context) {
	theme, colors, panel := settingsTheme(c, v.appearance)
	buttonsTheme := mygo.ThemeLight
	if theme.Dark {
		buttonsTheme = mygo.ThemeDark
	}
	if v.window != nil && v.buttonsTheme != buttonsTheme {
		v.buttonsTheme = buttonsTheme
		v.window.SetWindowButtonsTheme(buttonsTheme)
	}
	_, height := c.Size()
	topInset, panelGap, sectionGap := float32(21), float32(15), float32(22)
	if height < 510 {
		topInset, panelGap, sectionGap = 11, 9, 12
	}
	ui.Themed(c, theme).Fill().Children(func() {
		ui.Column(c).Fill().Background(colors.surface).Children(func() {
			settingsTitleBar(c, colors)
			ui.Row(c).Grow(1).FillWidth().Padding(0, settingsFrameInset, settingsFrameInset, 0).Children(func() {
				v.navigation(c, colors)
				ui.Column(c).Grow(1).FillHeight().Role(ui.RoleTabPanel).Label(settingsPages[v.tab].label).
					Background(panel).Border(1, colors.border).Radius(12).
					Padding(topInset, settingsGutter, 13).Gap(panelGap).Children(func() {
					ui.Text(c, settingsPages[v.tab].title).Role(ui.RoleHeading).FontSize(24).
						FontWeight(620).TextColor(colors.ink).Shrink(0)
					if v.tab == 3 {
						v.dictionary(c, colors)
						return
					}
					ui.Scroll(c).Key(settingsPages[v.tab].label).Grow(1).FillWidth().Children(func() {
						ui.Column(c).FillWidth().Gap(sectionGap).Children(func() {
							switch v.tab {
							case 0:
								v.inputSettings(c, colors)
							case 1:
								v.translationSettings(c, colors)
							case 2:
								v.candidateSettings(c, colors)
							}
						})
					})
					settingsDivider(c, colors)
					v.saveFooter(c, colors)
				})
			})
		})
	})
}

func settingsStatusColor(colors panelPalette, failed bool) ui.Color {
	if failed {
		return colors.errorText
	}
	return colors.muted
}

func (v *settingsView) saveFooter(c *ui.Context, colors panelPalette) {
	edited := v.editedSettings()
	dirty := edited != v.initial
	ui.Row(c).FillWidth().MinHeight(36).Shrink(0).AlignItems(ui.Center).Justify(ui.End).Gap(10).Children(func() {
		label := "保存设置"
		button := ui.Button
		if dirty {
			button = ui.PrimaryButton
			if v.saveError != "" && edited == v.failedSettings {
				ui.Text(c, v.saveError).TextColor(colors.errorText).Grow(1).FontSize(13)
				label = "重试保存"
			}
		} else if v.saved {
			label = "已保存"
		}
		button(c, label).MinWidth(108).Shrink(0).Disabled(!dirty).DimDisabled(false).OnClick(v.saveSettings)
	})
}

func (v *settingsView) editedSettings() imeSettings {
	next := v.initial
	next.Language = slices.Index(languageLabels, v.language)
	next.SourceLanguage = selectedSourceLanguage(v.sourceLanguage)
	next.CandidateFontSize, _ = strconv.Atoi(v.font)
	next.Appearance = appearanceValues[slices.Index(appearanceLabels, v.appearance)]
	next.Paging = pagingValues[slices.Index(pagingLabels, v.paging)]
	next.FuzzyPinyin = v.fuzzy == "开启"
	next.QuickLanguages = 0
	for i, enabled := range v.quickLanguages {
		if enabled {
			next.QuickLanguages |= 1 << i
		}
	}
	next.TranslationEngine = engineOnline
	if v.engine == engineLabels[1] {
		next.TranslationEngine = engineOffline
	}
	next.OfflineOnlineFallback = v.offlineOnlineFallback
	options := v.ordinaryLanguageCodes()
	selected := ""
	for _, code := range options {
		if ordinaryLanguageLabel(code) == v.ordinary {
			selected = code
		}
	}
	if selected == "" {
		selected = options[len(options)-1]
	}
	// Keep an unmodified legacy preference byte-for-byte when opening settings.
	if selected != v.initial.effectiveOrdinaryLanguage() || next.SourceLanguage != v.initial.SourceLanguage || next.Language != v.initial.Language || next.TranslationEngine != v.initial.TranslationEngine {
		next.OrdinaryLanguage = selected
		next.OrdinaryUsesChinese = selected == "zh-Hans"
	}
	next.Hotkeys = shortcutSettings{
		ToggleTranslation: keyboardCodes[slices.Index(keyboardLabels, v.toggle)],
		NextLanguage:      keyboardCodes[slices.Index(keyboardLabels, v.nextLanguage)],
		CopyTranslation:   copyKeyboardCodes[slices.Index(copyKeyboardLabels, v.copyTranslation)],
	}
	return next
}

func (v *settingsView) saveSettings() {
	edited := v.editedSettings()
	if edited == v.initial {
		return
	}
	current := settingsSnapshot()
	next := edited
	// Unchanged controls keep preferences which may have changed by hotkey.
	if edited.Language == v.initial.Language {
		next.Language = current.Language
	}
	if edited.SourceLanguage == v.initial.SourceLanguage {
		next.SourceLanguage = current.SourceLanguage
	}
	if edited.OrdinaryUsesChinese == v.initial.OrdinaryUsesChinese {
		next.OrdinaryUsesChinese = current.OrdinaryUsesChinese
	}
	if edited.OrdinaryLanguage == v.initial.OrdinaryLanguage {
		next.OrdinaryLanguage = current.OrdinaryLanguage
	}
	if next.TranslationEngine == engineOffline && current.TranslationEngine != engineOffline {
		model := currentOfflineModelStatus()
		if !model.installed || model.operation != modelIdle {
			v.saveError, v.failedSettings = "请先下载中译英模型。", edited
			return
		}
	}
	if err := saveIMEConfig(next); err != nil {
		v.saveError, v.failedSettings = fmt.Sprintf("保存失败。%v", err), edited
		return
	}
	applyInputSettings()
	v.initial, v.language = next, languageLabels[next.Language]
	v.sourceLanguage = sourceLanguageName(next.SourceLanguage)
	v.ordinary = ordinaryLanguageLabel(next.effectiveOrdinaryLanguage())
	v.saved, v.saveError = true, ""
}

func (v *settingsView) ordinaryLanguageCodes() []string {
	source := selectedSourceLanguage(v.sourceLanguage)
	target := targetLanguages[slices.Index(languageLabels, v.language)].code
	if v.engine == engineLabels[1] {
		source, target = "zh-Hans", "en"
	}
	source, target = ordinaryPair(source, target)
	if source == target {
		return []string{source}
	}
	return []string{source, target}
}

func (v *settingsView) translationSettings(c *ui.Context, colors panelPalette) {
	settingsSection(c, colors, "引擎与语言", func() {
		settingsOption(c, colors, "翻译引擎", &v.engine, engineLabels)
		settingsDivider(c, colors)
		if v.engine == engineLabels[1] {
			ui.Row(c).FillWidth().Height(40).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Text(c, "翻译方向").FontSize(14).TextColor(colors.ink)
				ui.Text(c, "中文 → 英语").FontSize(14).TextColor(colors.muted)
			})
			ui.Column(c).Padding(3, 0, 9).Gap(5).Children(func() {
				ui.Checkbox(c, &v.offlineOnlineFallback, "离线模型不可用时改用在线翻译")
				ui.Text(c, "开启后，回退时会将当前草稿发送给微软 Edge 翻译服务。").
					FillWidth().FontSize(12).TextColor(colors.muted)
			})
		} else {
			settingsOption(c, colors, "输入语言", &v.sourceLanguage, sourceLanguageLabels)
			settingsDivider(c, colors)
			settingsOption(c, colors, "目标语言", &v.language, languageLabels)
			ui.Row(c).FillWidth().Padding(4, 0, 7).AlignItems(ui.Center).Gap(10).Children(func() {
				direction := v.sourceLanguage + " → " + v.language
				if v.sourceLanguage == sourceLanguageLabels[0] {
					direction = "自动识别输入语言"
				}
				ui.Text(c, direction).Grow(1).FontSize(12).TextColor(colors.muted).
					NoWrap().MaxLines(1).Ellipsis("…").Tooltip(direction)
				ui.Button(c, "交换语言").Shrink(0).Disabled(v.sourceLanguage == sourceLanguageLabels[0]).
					Tooltip("交换输入语言与目标语言；自动检测时请先选定输入语言").OnClick(v.swapTranslationLanguages)
			})
		}
	})
	settingsSection(c, colors, "离线模型", func() {
		offlineModelControls(c, colors)
	})
	if v.engine != engineLabels[0] {
		return
	}
	settingsSection(c, colors, "快捷切换目标语言", func() {
		ui.Column(c).FillWidth().Padding(8, 0).Gap(10).Children(func() {
			ui.Text(c, v.nextLanguage+" 轮换勾选的语言。全部取消勾选即可停用轮换。").
				FillWidth().FontSize(12).TextColor(colors.muted)
			columns := 3
			if settingsBodyWidth(c) < 440 {
				columns = 2
			}
			ui.Grid(c).Columns(columns).FillWidth().Gap(7).Children(func() {
				for i, label := range languageLabels {
					ui.Checkbox(c, &v.quickLanguages[i], label).FillWidth()
				}
			})
		})
	})
}

func (v *settingsView) dictionary(c *ui.Context, colors panelPalette) {
	ui.Tabs(c, &v.dictionaryTab, "常用词", "自定义短语").FillWidth()
	if v.dictionaryTab == 0 {
		v.wordList(c, colors)
	} else {
		v.phraseList(c, colors)
	}
	if v.dictStatus != "" {
		ui.Text(c, v.dictStatus).TextColor(settingsStatusColor(colors, v.dictError)).FontSize(13).FillWidth().Shrink(0)
	}
}

func (v *settingsView) wordList(c *ui.Context, colors panelPalette) {
	if !v.wordsLoaded || v.filter != v.lastFilter {
		v.words = personalWords(v.filter)
		v.lastFilter, v.wordsLoaded = v.filter, true
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Shrink(0).Children(func() {
		ui.TextInput(c, &v.filter).Grow(1).Label("搜索常用词").Placeholder("输入拼音或汉字")
		ui.Button(c, "导入").Shrink(0).OnClick(func() { v.wordFileDialog(false) })
		ui.Button(c, "导出").Shrink(0).OnClick(func() { v.wordFileDialog(true) })
	})
	ui.Column(c).FillWidth().Grow(1).Clip().Children(func() {
		settingsDivider(c, colors)
		if len(v.words) == 0 {
			title, message := "暂时没有常用词", "选词并上屏后，记录会出现在这里。"
			if v.filter != "" {
				title, message = "没有匹配的词", "可以换一个拼音或汉字试试。"
			}
			settingsDictionaryEmpty(c, colors, title, message)
			return
		}
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 12).Children(func() {
				for i, word := range v.words[:min(len(v.words), 120)] {
					if i > 0 {
						settingsDivider(c, colors)
					}
					ui.Row(c).FillWidth().Padding(8, 1).Gap(7).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Grow(1).Gap(3).Clip().Children(func() {
							ui.Text(c, word.Text).TextColor(colors.ink).FontWeight(600).NoWrap().
								MaxLines(1).Ellipsis("…").Tooltip(word.Text).FillWidth()
							ui.Text(c, fmt.Sprintf("%s · %d 次", word.key, word.Count)).FontSize(12).
								TextColor(colors.muted).NoWrap().MaxLines(1).Ellipsis("…").FillWidth()
						})
						pin := "置顶"
						if word.Pinned {
							pin = "取消置顶"
						}
						ui.Button(c, pin).MinWidth(77).Shrink(0).OnClick(func() { v.updateWord(word, false) })
						ui.Button(c, "移除").Shrink(0).OnClick(func() { v.updateWord(word, true) })
					})
				}
				if len(v.words) > 120 {
					ui.Text(c, fmt.Sprintf("共 %d 条，搜索可定位其余词条。", len(v.words))).
						FontSize(12).TextColor(colors.muted).Padding(8, 0).FillWidth()
				}
			})
		})
	})
}

func settingsDictionaryEmpty(c *ui.Context, colors panelPalette, title, message string) {
	ui.Column(c).FillWidth().Padding(24, 0).Gap(7).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(colors.ink)
		ui.Text(c, message).FontSize(13).TextColor(colors.muted).FillWidth()
	})
}

func (v *settingsView) updateWord(word personalWord, remove bool) {
	message := "已更新置顶"
	if remove {
		message = "已移除"
	}
	v.setDictionaryStatus(message, editPersonalWord(word, remove))
	v.wordsLoaded = false
}

func (v *settingsView) wordFileDialog(export bool) {
	window := v.window
	filters := []mygo.FileFilter{{Name: "个人词库 JSON", Extensions: []string{"json"}}}
	go func() {
		var filename, message string
		var err error
		if export {
			filename, err = mygo.Dialog.Save(mygo.SaveDialogOptions{
				Parent: window, Title: "导出个人词库", DefaultPath: "ContextIME-words.json", Filters: filters,
			})
			if filename != "" && err == nil {
				// The live database has its own save cycle. Export is for backups.
				if current, pathErr := localConfigPath("words.json"); pathErr == nil && sameDictionaryPath(filename, current) {
					err = fmt.Errorf("请选择不同的备份位置")
				} else {
					err = exportPersonalWords(filename)
				}
			}
			message = "已导出个人词库"
		} else {
			var paths []string
			paths, err = mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: "导入个人词库", Filters: filters})
			if len(paths) > 0 && err == nil {
				filename = paths[0]
				err = importPersonalWords(filename)
			}
			message = "已合并个人词库"
		}
		if filename == "" && err == nil {
			return
		}
		window.Update(func() {
			v.setDictionaryStatus(message, err)
			v.wordsLoaded = false
		})
	}()
}

func (v *settingsView) phraseList(c *ui.Context, colors panelPalette) {
	if v.phrases == nil {
		v.refreshPhrases()
	}
	settingsCard(c, colors).Shrink(0).Padding(10, 12).Gap(7).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			if v.phraseOld == "" {
				ui.TextInput(c, &v.phraseKey).Width(102).Shrink(0).Label("拼音或缩写").Placeholder("拼音或缩写")
			} else {
				ui.Text(c, v.phraseKey).Width(102).Shrink(0).FontSize(14).TextColor(colors.ink).
					NoWrap().MaxLines(1).Ellipsis("…").Tooltip(v.phraseKey)
			}
			ui.TextInput(c, &v.phraseText).Grow(1).Label("短语内容").Placeholder("短语内容")
			if v.phraseOld == "" {
				ui.PrimaryButton(c, "添加").Shrink(0).OnClick(v.savePhrase)
			}
		})
		if v.phraseOld != "" {
			ui.Row(c).FillWidth().Gap(7).Justify(ui.End).Children(func() {
				ui.Button(c, "取消").OnClick(v.resetPhraseEditor)
				ui.PrimaryButton(c, "更新").OnClick(v.savePhrase)
			})
		}
	})
	ui.Column(c).FillWidth().Grow(1).Clip().Children(func() {
		settingsDivider(c, colors)
		if len(v.phrases) == 0 {
			settingsDictionaryEmpty(c, colors, "还没有自定义短语", "添加缩写和短语后，输入缩写就能选到这条短语。")
			return
		}
		keys := make([]string, 0, len(v.phrases))
		for key := range v.phrases {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 12).Children(func() {
				first := true
				for _, key := range keys {
					for _, phrase := range v.phrases[key] {
						if !first {
							settingsDivider(c, colors)
						}
						first = false
						ui.Row(c).FillWidth().Padding(8, 0).Gap(7).AlignItems(ui.Center).Children(func() {
							ui.Column(c).Grow(1).Gap(3).Clip().Children(func() {
								ui.Text(c, phrase).TextColor(colors.ink).FontWeight(600).NoWrap().
									MaxLines(1).Ellipsis("…").Tooltip(phrase).FillWidth()
								ui.Text(c, key).FontSize(12).TextColor(colors.muted).NoWrap().
									MaxLines(1).Ellipsis("…").Tooltip(key).FillWidth()
							})
							ui.Button(c, "编辑").Shrink(0).OnClick(func() {
								v.phraseKey, v.phraseText, v.phraseOld = key, phrase, phrase
							})
							ui.Button(c, "删除").Shrink(0).OnClick(func() { v.deletePhrase(key, phrase) })
						})
					}
				}
			})
		})
	})
}

func (v *settingsView) refreshPhrases() {
	var err error
	v.phrases, err = phraseEntries()
	if err != nil {
		v.phrases = make(map[string][]string)
		v.setDictionaryStatus("", err)
	}
}

func (v *settingsView) savePhrase() {
	if strings.TrimSpace(v.phraseText) == "" {
		v.setDictionaryStatus("", fmt.Errorf("请输入短语"))
		return
	}
	err := editPhrase(v.phraseKey, v.phraseOld, v.phraseText)
	v.setDictionaryStatus("已保存短语", err)
	if err == nil {
		v.resetPhraseEditor()
		v.refreshPhrases()
	}
}

func (v *settingsView) deletePhrase(key, phrase string) {
	err := editPhrase(key, phrase, "")
	v.setDictionaryStatus("已删除短语", err)
	if err == nil {
		if v.phraseKey == key && v.phraseOld == phrase {
			v.resetPhraseEditor()
		}
		v.refreshPhrases()
	}
}

func (v *settingsView) resetPhraseEditor() { v.phraseKey, v.phraseText, v.phraseOld = "", "", "" }

func (v *settingsView) setDictionaryStatus(message string, err error) {
	v.dictError = err != nil
	if err != nil {
		message = fmt.Sprintf("操作失败。%v", err)
	}
	v.dictStatus = message
}
