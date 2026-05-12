package edgar

import (
	"html"
	"regexp"
	"strings"
)

var (
	inlineXBRLPattern = regexp.MustCompile(`(?is)<ix:header\b[\s\S]*?</ix:header>|<ix:hidden\b[\s\S]*?</ix:hidden>|<ix:resources\b[\s\S]*?</ix:resources>`)
	removeTagPattern  = regexp.MustCompile(`(?is)<script\b[\s\S]*?</script>|<style\b[\s\S]*?</style>|<noscript\b[\s\S]*?</noscript>|<iframe\b[\s\S]*?</iframe>|<canvas\b[\s\S]*?</canvas>`)
	tablePattern      = regexp.MustCompile(`(?is)<table\b[\s\S]*?</table>`)
	rowPattern        = regexp.MustCompile(`(?is)<tr\b[\s\S]*?</tr>`)
	cellPattern       = regexp.MustCompile(`(?is)<t[dh]\b[^>]*>[\s\S]*?</t[dh]>`)
	thPattern         = regexp.MustCompile(`(?is)<th\b`)
	tagPattern        = regexp.MustCompile(`(?is)<[^>]+>`)
	headingPattern    = regexp.MustCompile(`(?is)<h([1-6])\b[^>]*>([\s\S]*?)</h[1-6]>`)
	brPattern         = regexp.MustCompile(`(?is)<br\s*/?>`)
	blockTagPattern   = regexp.MustCompile(`(?is)</?(p|div|section|article|header|footer|main|li|ul|ol|blockquote|pre|hr)\b[^>]*>`)
)

func extractMarkdownFromHTML(content string) string {
	sanitized := stripInlineXBRLHeaders(content)
	sanitized = removeTagPattern.ReplaceAllString(sanitized, "")
	sanitized = tablePattern.ReplaceAllStringFunc(sanitized, convertHTMLTable)
	sanitized = headingPattern.ReplaceAllStringFunc(sanitized, convertHTMLHeading)
	sanitized = brPattern.ReplaceAllString(sanitized, "\n")
	sanitized = blockTagPattern.ReplaceAllString(sanitized, "\n")
	sanitized = tagPattern.ReplaceAllString(sanitized, "")
	sanitized = html.UnescapeString(sanitized)
	sanitized = strings.ReplaceAll(sanitized, "\u00a0", " ")
	sanitized = strings.ReplaceAll(sanitized, "\r", "")

	lines := strings.Split(sanitized, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = regexp.MustCompile(`[ \t]+`).ReplaceAllString(line, " ")
		out = append(out, line)
	}
	return collapseLayoutTables(strings.TrimSpace(collapseBlankLines(strings.Join(out, "\n"))))
}

func stripInlineXBRLHeaders(content string) string {
	return inlineXBRLPattern.ReplaceAllString(content, "")
}

func convertHTMLHeading(match string) string {
	parts := headingPattern.FindStringSubmatch(match)
	if len(parts) != 3 {
		return ""
	}
	level := int(parts[1][0] - '0')
	text := markdownCellText(parts[2])
	if text == "" {
		return ""
	}
	return "\n" + strings.Repeat("#", level) + " " + text + "\n"
}

func convertHTMLTable(tableHTML string) string {
	rows := rowPattern.FindAllString(tableHTML, -1)
	if len(rows) == 0 {
		return ""
	}

	var rendered []string
	headerWritten := false
	for rowIdx, row := range rows {
		cellMatches := cellPattern.FindAllString(row, -1)
		if len(cellMatches) == 0 {
			continue
		}
		cells := make([]string, 0, len(cellMatches))
		for _, cell := range cellMatches {
			cells = append(cells, markdownCellText(cell))
		}
		rendered = append(rendered, "| "+strings.Join(cells, " | ")+" |")
		if !headerWritten && (rowIdx == 0 || thPattern.MatchString(row)) {
			rendered = append(rendered, "| "+strings.Join(repeatString("---", len(cells)), " | ")+" |")
			headerWritten = true
		}
	}

	if len(rendered) == 0 {
		return ""
	}
	return "\n" + strings.Join(rendered, "\n") + "\n"
}

func markdownCellText(value string) string {
	value = brPattern.ReplaceAllString(value, " ")
	value = blockTagPattern.ReplaceAllString(value, " ")
	value = tagPattern.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	return strings.TrimSpace(regexp.MustCompile(`[ \t\r\n]+`).ReplaceAllString(value, " "))
}

func repeatString(value string, count int) []string {
	out := make([]string, 0, count)
	for idx := 0; idx < count; idx++ {
		out = append(out, value)
	}
	return out
}

func collapseBlankLines(value string) string {
	for strings.Contains(value, "\n\n\n") {
		value = strings.ReplaceAll(value, "\n\n\n", "\n\n")
	}
	return value
}

func splitMarkdownTableCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	rawCells := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(rawCells))
	for _, cell := range rawCells {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

func isMarkdownTableSeparatorLine(line string) bool {
	cells := splitMarkdownTableCells(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		normalized := strings.ReplaceAll(cell, " ", "")
		if !regexp.MustCompile(`^:?-{3,}:?$`).MatchString(normalized) {
			return false
		}
	}
	return true
}

func collapseLayoutTables(markdown string) string {
	lines := strings.Split(markdown, "\n")
	output := []string{}

	for idx := 0; idx < len(lines); idx++ {
		line := lines[idx]
		if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "|") {
			output = append(output, line)
			continue
		}

		tableBlock := []string{line}
		for idx+1 < len(lines) && strings.HasPrefix(strings.TrimLeft(lines[idx+1], " \t"), "|") {
			idx++
			tableBlock = append(tableBlock, lines[idx])
		}

		hasSeparator := false
		for _, row := range tableBlock {
			if isMarkdownTableSeparatorLine(row) {
				hasSeparator = true
				break
			}
		}
		if !hasSeparator {
			output = append(output, tableBlock...)
			continue
		}

		dataRows := []string{}
		for _, row := range tableBlock {
			if !isMarkdownTableSeparatorLine(row) {
				dataRows = append(dataRows, row)
			}
		}
		nonEmptyCounts := []int{}
		for _, row := range dataRows {
			count := 0
			for _, cell := range splitMarkdownTableCells(row) {
				if cell != "" {
					count++
				}
			}
			nonEmptyCounts = append(nonEmptyCounts, count)
		}
		maxCount := 0
		sum := 0
		for _, count := range nonEmptyCounts {
			if count > maxCount {
				maxCount = count
			}
			sum += count
		}
		avg := 0.0
		if len(nonEmptyCounts) > 0 {
			avg = float64(sum) / float64(len(nonEmptyCounts))
		}
		isLayoutTable := maxCount <= 1 || avg <= 1.2
		if !isLayoutTable {
			output = append(output, tableBlock...)
			continue
		}

		for _, row := range dataRows {
			cells := []string{}
			for _, cell := range splitMarkdownTableCells(row) {
				if cell != "" {
					cells = append(cells, cell)
				}
			}
			flattened := strings.TrimSpace(regexp.MustCompile(`[ \t]+`).ReplaceAllString(strings.Join(cells, " "), " "))
			if flattened != "" {
				output = append(output, flattened)
			}
		}
		output = append(output, "")
	}

	return strings.TrimSpace(collapseBlankLines(strings.Join(output, "\n")))
}
