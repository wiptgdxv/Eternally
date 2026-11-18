package main

func ordinaryPair(source, target string) (string, string) {
	if source == "" {
		source = "zh-Hans"
	}
	if source == "auto" {
		source = "en"
	}
	return source, target
}

func ordinaryLanguageChoice(source, target, selected string, legacyChinese bool) string {
	source, target = ordinaryPair(source, target)
	if selected == source || selected == target {
		return selected
	}
	if legacyChinese && (source == "zh-Hans" || target == "zh-Hans") {
		return "zh-Hans"
	}
	if source == "en" || target == "en" {
		return "en"
	}
	return target
}

func (m inputMode) ordinaryCode() string {
	return ordinaryLanguageChoice(m.sourceCode(), targetLanguages[m.language].code, m.ordinaryLanguage, m.ordinaryUsesChinese)
}

func (m inputMode) compositionCode() string {
	if m.chinese {
		return m.ordinaryCode()
	}
	return m.sourceCode()
}

func (m *inputMode) syncOrdinaryLanguage() {
	m.ordinaryLanguage = m.ordinaryCode()
	m.ordinaryUsesChinese = m.ordinaryLanguage == "zh-Hans"
	m.direct = m.chinese && m.ordinaryLanguage != "zh-Hans" && m.ordinaryLanguage != "ja"
}

func (m *inputMode) toggleOrdinaryLanguage() bool {
	if !m.chinese {
		return false
	}
	source, target := ordinaryPair(m.sourceCode(), targetLanguages[m.language].code)
	selected := m.ordinaryCode()
	if selected == source {
		m.ordinaryLanguage = target
	} else {
		m.ordinaryLanguage = source
	}
	m.syncOrdinaryLanguage()
	return source != target
}

func (m *inputMode) toggleTranslation() {
	m.chinese = !m.chinese
	m.syncOrdinaryLanguage()
}

func ordinaryLanguageLabel(code string) string {
	switch code {
	case "zh-Hans":
		return "中文拼音"
	case "ja":
		return "日语罗马字"
	default:
		return sourceLanguageName(code) + "直输"
	}
}
