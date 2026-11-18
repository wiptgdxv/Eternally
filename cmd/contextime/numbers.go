package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func numericCode(input string) bool {
	return len(input) >= 2 && input[0] == 'v' && input[1] >= '0' && input[1] <= '9'
}

func numberChoices(input string) []choice {
	raw := input[1:]
	parts := strings.Split(raw, ".")
	if len(parts) == 3 && len(parts[0]) == 4 {
		year, yErr := strconv.Atoi(parts[0])
		month, mErr := strconv.Atoi(parts[1])
		day, dErr := strconv.Atoi(parts[2])
		if yErr == nil && mErr == nil && dErr == nil && year > 0 {
			date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
			if date.Year() == year && int(date.Month()) == month && date.Day() == day {
				return []choice{
					{fmt.Sprintf("%d年%d月%d日", year, month, day), len(input)},
					{date.Format("2006-01-02"), len(input)},
				}
			}
		}
		return nil
	}
	if len(parts) > 2 || len(parts[len(parts)-1]) == 0 {
		return nil
	}
	digitText := []rune("零一二三四五六七八九")
	upperText := []rune("零壹贰叁肆伍陆柒捌玖")
	var common, upper strings.Builder
	for _, r := range raw {
		if r == '.' {
			common.WriteString("点")
			upper.WriteString("点")
		} else if r >= '0' && r <= '9' {
			common.WriteRune(digitText[r-'0'])
			upper.WriteRune(upperText[r-'0'])
		} else {
			return nil
		}
	}
	return []choice{{common.String(), len(input)}, {upper.String(), len(input)}, {raw, len(input)}}
}
