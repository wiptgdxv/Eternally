package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

const candidateToggleWidth = 30

func drawExpandedCandidates(c *ui.Context, width, fontSize, rowHeight float32, s draft, all []choice, colors panelPalette, pick func(string, choice), browse func(string, uint32)) float32 {
	start := s.Selected / expandedPageSize * expandedPageSize
	end := min(start+expandedPageSize, len(all))
	numberedRow := s.pageStart()
	cellWidth := max(1, (width-candidateGap*(pageSize-1)-candidateToggleWidth-candidateGap)/pageSize)
	var height float32
	for row := start; row < end; row += pageSize {
		height += panelGap + rowHeight
		ui.Row(c).Width(width).Height(rowHeight).Gap(candidateGap).Shrink(0).AlignItems(ui.Center).Children(func() {
			for index := row; index < min(row+pageSize, end); index++ {
				item := all[index]
				label := item.text
				if row == numberedRow {
					label = fmt.Sprintf("%d  %s", index-row+1, label)
				}
				cell := ui.ButtonBase(c).Label(label).KeepFocus().Size(cellWidth, rowHeight).Shrink(0).Radius(4)
				tint := colors.ink
				if index == s.Selected {
					cell.Background(colors.selected)
					tint = colors.accent
				}
				if pick != nil {
					cell.Cursor(ui.CursorPointer).Tooltip(item.text)
					if index != s.Selected && cell.Hovered() {
						cell.Background(colors.hover)
					}
				}
				cell.Children(func() {
					ui.Text(c, label).FontSize(fontSize).TextColor(tint).MaxWidth(max(1, cellWidth-8)).NoWrap().MaxLines(1).Ellipsis("…")
				})
				if pick != nil && cell.Clicked() {
					pick(s.Pinyin, item)
				}
			}
			if row == start && browse != nil {
				ui.Box(c).Grow(1)
				if candidateArrow(c, "收起候选", keyUp, true, colors).Expanded(true).Clicked() {
					browse(s.Pinyin, keyToggleCandidates)
				}
			}
		})
	}
	if len(all) > expandedPageSize && browse != nil {
		height += panelGap + candidateRow
		ui.Row(c).Width(width).Height(candidateRow).Gap(candidateGap).Justify(ui.End).Shrink(0).Children(func() {
			if candidateArrow(c, "上一组候选", keyLeft, start > 0, colors).Clicked() {
				browse(s.Pinyin, keyPageUp)
			}
			if candidateArrow(c, "下一组候选", keyRight, end < len(all), colors).Clicked() {
				browse(s.Pinyin, keyPageDown)
			}
		})
	}
	return height
}

func candidateArrow(c *ui.Context, label string, direction uint32, enabled bool, colors panelPalette) ui.Element {
	button := ui.ButtonBase(c).Label(label).Tooltip(label).KeepFocus().Size(candidateToggleWidth, candidateRow).Shrink(0).Radius(5).Disabled(!enabled)
	color := colors.muted
	if enabled {
		button.Cursor(ui.CursorPointer)
		if button.Hovered() {
			button.Background(colors.hover)
			color = colors.ink
		}
	}
	button.Draw(func(p *ui.Painter, r ui.Rect) {
		x, y := r.X+r.W/2, r.Y+r.H/2
		left, tip, right := [2]float32{x - 4, y - 2}, [2]float32{x, y + 2}, [2]float32{x + 4, y - 2}
		switch direction {
		case keyUp:
			left, tip, right = [2]float32{x - 4, y + 2}, [2]float32{x, y - 2}, [2]float32{x + 4, y + 2}
		case keyLeft:
			left, tip, right = [2]float32{x + 2, y - 4}, [2]float32{x - 2, y}, [2]float32{x + 2, y + 4}
		case keyRight:
			left, tip, right = [2]float32{x - 2, y - 4}, [2]float32{x + 2, y}, [2]float32{x - 2, y + 4}
		}
		p.StrokePolyline([][2]float32{left, tip, right}, 1.5, color)
	})
	return button
}
