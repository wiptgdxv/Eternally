package main

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

const (
	settingsRailWidth     = 164
	settingsFrameInset    = 12
	settingsGutter        = 26
	settingsCaptionHeight = 44
)

var settingsPages = [...]struct{ label, title string }{
	{"输入", "输入与快捷键"},
	{"翻译", "翻译"},
	{"候选窗", "候选窗"},
	{"词库", "词库"},
}

var settingsLogo, _ = ui.DecodeBitmap(settingsTrayIcon)

func settingsTheme(c *ui.Context, appearance string) (*ui.Theme, panelPalette, ui.Color) {
	if c.Preferences().HighContrast {
		t := c.Theme()
		colors := panelPalette{t.Background, t.Text, t.Text, t.Text, t.SurfacePressed, t.Border, t.SurfaceHover, t.Text}
		return t, colors, t.Background
	}

	dark := appearance == "深色" || (appearance == "跟随系统" && c.Theme().Dark)
	colors := panelPalette{
		surface: ui.Hex("#EFF2F5"), ink: ui.Hex("#222A34"), muted: ui.Hex("#53606C"),
		accent: ui.Hex("#176057"), selected: ui.Hex("#DCEBE8"), border: ui.Hex("#DAE0E6"),
		hover: ui.Hex("#E4E9EE"), errorText: ui.Hex("#9B392C"),
	}
	base := ui.LightTheme()
	panel, control := ui.Hex("#FCFDFE"), ui.Hex("#F4F6F8")
	if dark {
		base = ui.DarkTheme()
		colors = panelPalette{
			surface: ui.Hex("#191D23"), ink: ui.Hex("#E8EDF3"), muted: ui.Hex("#B6C0CC"),
			accent: ui.Hex("#96D4C7"), selected: ui.Hex("#2A403F"), border: ui.Hex("#3B4652"),
			hover: ui.Hex("#303946"), errorText: ui.Hex("#F1C0B0"),
		}
		panel, control = ui.Hex("#222831"), ui.Hex("#2B333E")
	}
	prefs := c.Preferences()
	prefs.Accent = ui.Color{}
	t := base.WithPreferences(prefs).WithAccent(colors.accent)
	t.Background, t.Surface = colors.surface, control
	t.SurfaceHover, t.SurfacePressed, t.Border = colors.hover, colors.selected, colors.border
	t.Text, t.TextMuted, t.Danger = colors.ink, colors.muted, colors.errorText
	t.InactiveAccent = colors.accent
	t.Radius = 7
	return t, colors, panel
}

func settingsBodyWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	return max(0, width-settingsRailWidth-settingsFrameInset-2*settingsGutter)
}

func settingsTitleBar(c *ui.Context, colors panelPalette) {
	ui.Row(c).FillWidth().Height(settingsCaptionHeight).Shrink(0).
		Padding(0, 18+c.TitleBar().Right, 0, 18).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Image(c, settingsLogo).Size(24, 24).Shrink(0)
		ui.Text(c, productName).FontSize(15).FontWeight(650).TextColor(colors.ink).NoWrap()
	})
}

func (v *settingsView) navigation(c *ui.Context, colors panelPalette) {
	tabs := ui.TabsBase(c, &v.tab, len(settingsPages))
	tabs.List.Column().FocusGroup(ui.Vertical).Label("设置导航").Width(settingsRailWidth).Shrink(0).
		AlignSelf(ui.Start).Padding(13, 14).Gap(5).Children(func() {
		for i, page := range settingsPages {
			item := tabs.Tab(i).Label(page.label).FillWidth().Height(39).Padding(0, 12).
				Gap(10).Radius(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).FocusRingOffset(-2)
			tint, weight := colors.muted, 500
			switch {
			case v.tab == i:
				tint, weight = colors.accent, 650
				item.Background(colors.selected)
			case item.Hovered():
				item.Background(colors.hover)
			}
			item.Children(func() {
				drawSettingsNavIcon(c, i, tint)
				ui.Text(c, page.label).FontSize(14).FontWeight(weight).TextColor(tint).NoWrap()
			})
		}
	})
}

func drawSettingsNavIcon(c *ui.Context, page int, color ui.Color) {
	ui.Box(c).Size(18, 18).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		line := func(points ...[2]float32) {
			for i := range points {
				points[i][0] += r.X
				points[i][1] += r.Y
			}
			p.StrokePolyline(points, 1.35, color)
		}
		switch page {
		case 0:
			line([2]float32{2, 5}, [2]float32{16, 5}, [2]float32{16, 14}, [2]float32{2, 14}, [2]float32{2, 5})
			line([2]float32{5, 8}, [2]float32{7, 8})
			line([2]float32{10, 8}, [2]float32{13, 8})
			line([2]float32{6, 11}, [2]float32{12, 11})
		case 1:
			line([2]float32{2, 6}, [2]float32{15, 6}, [2]float32{12, 3})
			line([2]float32{15, 6}, [2]float32{12, 9})
			line([2]float32{16, 12}, [2]float32{3, 12}, [2]float32{6, 9})
			line([2]float32{3, 12}, [2]float32{6, 15})
		case 2:
			line([2]float32{2, 3}, [2]float32{16, 3}, [2]float32{16, 15}, [2]float32{2, 15}, [2]float32{2, 3})
			line([2]float32{2, 8}, [2]float32{16, 8})
			line([2]float32{5, 11.5}, [2]float32{9, 11.5})
		case 3:
			line([2]float32{9, 5}, [2]float32{6, 3}, [2]float32{2, 3}, [2]float32{2, 14}, [2]float32{6, 14}, [2]float32{9, 15}, [2]float32{12, 14}, [2]float32{16, 14}, [2]float32{16, 3}, [2]float32{12, 3}, [2]float32{9, 5}, [2]float32{9, 15})
		}
	})
}

func settingsCard(c *ui.Context, colors panelPalette) ui.Element {
	return ui.Column(c).FillWidth().Background(c.Theme().Surface).Border(1, colors.border).Radius(12)
}

func settingsSection(c *ui.Context, colors panelPalette, title string, body func()) {
	ui.Column(c).FillWidth().Shrink(0).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(650).TextColor(colors.muted).Padding(0, 0, 7)
		settingsDivider(c, colors)
		ui.Column(c).FillWidth().Children(body)
	})
}

func settingsDivider(c *ui.Context, colors panelPalette) {
	ui.Box(c).FillWidth().Height(1).Shrink(0).Background(colors.border)
}

func settingsOption(c *ui.Context, colors panelPalette, label string, value *string, options []string) {
	width := min(float32(184), settingsBodyWidth(c)*0.55)
	_, height := c.Size()
	rowHeight := float32(43)
	if height < 510 {
		rowHeight = 34
	}
	ui.Row(c).FillWidth().MinHeight(rowHeight).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, label).FontSize(14).TextColor(colors.ink).Grow(1)
		selectBox := ui.Select(c, value, options).Width(width).Shrink(0).Label(label)
		if height < 510 {
			selectBox.Padding(4, 11)
		}
	})
}

func (v *settingsView) inputSettings(c *ui.Context, colors panelPalette) {
	settingsSection(c, colors, "输入方式", func() {
		codes := v.ordinaryLanguageCodes()
		options := make([]string, len(codes))
		for i, code := range codes {
			options[i] = ordinaryLanguageLabel(code)
		}
		valid := false
		for _, label := range options {
			valid = valid || label == v.ordinary
		}
		if !valid {
			v.ordinary = options[len(options)-1]
		}
		settingsOption(c, colors, "普通模式默认语言", &v.ordinary, options)
		settingsDivider(c, colors)
		settingsOption(c, colors, "模糊拼音", &v.fuzzy, []string{"关闭", "开启"})
	})
	settingsSection(c, colors, "快捷键", func() {
		settingsOption(c, colors, "切换翻译模式", &v.toggle, keyboardLabels)
		settingsDivider(c, colors)
		settingsOption(c, colors, "切换下一种目标语言", &v.nextLanguage, keyboardLabels)
		settingsDivider(c, colors)
		settingsOption(c, colors, "复制最近译文", &v.copyTranslation, copyKeyboardLabels)
	})
}

func (v *settingsView) candidateSettings(c *ui.Context, colors panelPalette) {
	settingsSection(c, colors, "外观与操作", func() {
		settingsOption(c, colors, "颜色模式", &v.appearance, appearanceLabels)
		settingsDivider(c, colors)
		settingsOption(c, colors, "候选词字号", &v.font, fontOptions)
		settingsDivider(c, colors)
		settingsOption(c, colors, "候选翻页键", &v.paging, pagingLabels)
	})
	fontSize, _ := strconv.Atoi(v.font)
	ui.Column(c).FillWidth().Shrink(0).Gap(8).Children(func() {
		ui.Text(c, "候选字号预览").FontSize(13).FontWeight(650).TextColor(colors.muted)
		settingsCard(c, colors).Width(min(float32(300), settingsBodyWidth(c))).
			Padding(10, 12).Gap(7).Children(func() {
			ui.Text(c, "ni'hao").FontSize(15).TextColor(colors.ink)
			ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "1  你好").FontSize(float32(fontSize)).TextColor(colors.accent).
					Background(colors.selected).Padding(5, 7).Radius(5).NoWrap()
				ui.Text(c, "2  拟好").FontSize(float32(fontSize)).TextColor(colors.ink).NoWrap()
			})
		})
	})
}
