package io

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

var escapedSpecialCharPattern = regexp.MustCompile(`\\([.,()\-:;!?&/\[\]{}#+=])`)
var hTMLFigurePattern = regexp.MustCompile(`(?is)</?figure[^>]*>`)
var hTMLFigcaptionPattern = regexp.MustCompile(`(?is)<figcaption[^>]*>.*?</figcaption>`)
var hTMLImagePattern = regexp.MustCompile(`(?is)<img[^>]*src="([^"]+)"[^>]*alt="([^"]*)"[^>]*/?>`)
var markdownImagePathPattern = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
var tablePattern = regexp.MustCompile(`(?is)<table\b[^>]*>.*?</table>`)
var scriptPattern = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
var stylePattern = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
var tagPattern = regexp.MustCompile(`(?is)<[^>]+>`)

// CleanupMarkdownContentStandalone normalizes markdown-like content in one
// fully self-contained pass:
// - normalizes newlines
// - removes escaped punctuation
// - removes figcaption/figure HTML
// - converts HTML images into markdown images
// - converts simple HTML tables into markdown tables
// - rewrites markdown image paths to /media/<sanitized-name>
// - removes remaining HTML tags
func CleanupMarkdownContent(input string) string {
	text := normalizeNewlines(input)
	text = hTMLFigcaptionPattern.ReplaceAllString(text, "")
	text = hTMLFigurePattern.ReplaceAllString(text, "")
	text = convertHTMLTables(text)
	text = hTMLImagePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := hTMLImagePattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + strings.TrimSpace(parts[2]) + "](" + cleanupNormalizeMediaPath(parts[1]) + ")"
	})
	text = markdownImagePathPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := markdownImagePathPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + parts[1] + "](" + cleanupNormalizeMediaPath(parts[2]) + ")"
	})
	text = cleanupStripHTML(text)
	return escapedSpecialCharPattern.ReplaceAllString(text, "$1")
}

func normalizeNewlines(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	return strings.ReplaceAll(input, "\r", "\n")
}

func cleanupNormalizeMediaPath(pathValue string) string {
	pathValue = strings.TrimSpace(strings.Trim(pathValue, `"'`))
	pathValue = strings.ReplaceAll(pathValue, "\\", "/")
	segments := strings.Split(pathValue, "/")
	last := ""
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		last = cleanupSanitizeMediaSegment(segment)
	}
	if last == "" {
		last = "image"
	}
	return "/media/" + last
}

func cleanupSanitizeMediaSegment(segment string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range segment {
		switch {
		case r > unicode.MaxASCII || unicode.IsSpace(r):
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastUnderscore = false
		case strings.ContainsRune("._-", r):
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	sanitized := strings.Trim(builder.String(), "_")
	if sanitized == "" {
		return "file"
	}
	return sanitized
}

func convertHTMLTables(input string) string {
	matches := tablePattern.FindAllStringIndex(input, -1)
	if len(matches) == 0 {
		return input
	}

	var out bytes.Buffer
	last := 0
	for _, match := range matches {
		out.WriteString(input[last:match[0]])
		tableHTML := input[match[0]:match[1]]
		if markdownTable, ok := cleanupTableHTMLToMarkdown(tableHTML); ok {
			out.WriteString(markdownTable)
		} else {
			out.WriteString(tableHTML)
		}
		last = match[1]
	}
	out.WriteString(input[last:])
	return out.String()
}

func cleanupTableHTMLToMarkdown(tableHTML string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(tableHTML))
	if err != nil {
		return "", false
	}
	table := hTMLTableFindFirst(doc, "table")
	if table == nil || hTMLTableHasTableSpanAttrs(table) {
		return "", false
	}

	rows := hTMLTableHasFindAll(hTMLTableFindFirst(table, "thead"), "tr")
	rows = append(rows, hTMLTableHasFindAll(hTMLTableFindFirst(table, "tbody"), "tr")...)
	if len(rows) == 0 {
		rows = hTMLTableHasFindAll(table, "tr")
	}
	if len(rows) == 0 {
		return "", false
	}

	headerCells := hTMLTableHasExtractCells(rows[0])
	if len(headerCells) == 0 {
		return "", false
	}

	colCount := len(headerCells)
	hasTH := hTMLTableHaspRowHasTH(rows[0])
	header := make([]string, 0, colCount)
	for _, cell := range headerCells {
		header = append(header, hTMLTableHasCellText(cell))
	}

	bodyStart := 1
	if !hasTH {
		header = make([]string, colCount)
		for i := range header {
			header[i] = "Col" + cleanupIntString(i+1)
		}
		bodyStart = 0
	}

	var builder strings.Builder
	builder.WriteString(RenderMarkdownRow(header))
	builder.WriteString("\n")
	builder.WriteString(RenderMarkdownSeparator(colCount))
	builder.WriteString("\n")

	rowCount := 0
	for i := bodyStart; i < len(rows); i++ {
		cells := hTMLTableHasExtractCells(rows[i])
		if len(cells) == 0 {
			continue
		}
		row := make([]string, colCount)
		for j := 0; j < colCount; j++ {
			if j < len(cells) {
				row[j] = hTMLTableHasCellText(cells[j])
			}
		}
		builder.WriteString(RenderMarkdownRow(row))
		builder.WriteString("\n")
		rowCount++
	}

	if rowCount == 0 && hasTH {
		values := make([]string, colCount)
		for i, cell := range headerCells {
			full := hTMLTableHasCellText(cell)
			prefix := strings.TrimSpace(header[i]) + " "
			values[i] = strings.TrimSpace(strings.TrimPrefix(full, prefix))
		}
		builder.WriteString(RenderMarkdownRow(values))
		builder.WriteString("\n")
	}

	return builder.String(), true
}

func cleanupStripHTML(input string) string {
	text := scriptPattern.ReplaceAllString(input, "")
	text = stylePattern.ReplaceAllString(text, "")
	text = tagPattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	return text
}

func hTMLTableFindFirst(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, tag) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := hTMLTableFindFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func hTMLTableHasFindAll(n *html.Node, tag string) []*html.Node {
	if n == nil {
		return nil
	}
	var nodes []*html.Node
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur == nil {
			return
		}
		if cur.Type == html.ElementNode && strings.EqualFold(cur.Data, tag) {
			nodes = append(nodes, cur)
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return nodes
}

func hTMLTableHasTableSpanAttrs(n *html.Node) bool {
	if n == nil {
		return false
	}
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			key := strings.ToLower(attr.Key)
			if key == "rowspan" || key == "colspan" {
				return true
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hTMLTableHasTableSpanAttrs(c) {
			return true
		}
	}
	return false
}

func hTMLTableHasExtractCells(tr *html.Node) []*html.Node {
	var cells []*html.Node
	if tr == nil {
		return cells
	}
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (strings.EqualFold(c.Data, "th") || strings.EqualFold(c.Data, "td")) {
			cells = append(cells, c)
		}
	}
	return cells
}

func hTMLTableHaspRowHasTH(tr *html.Node) bool {
	if tr == nil {
		return false
	}
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, "th") {
			return true
		}
	}
	return false
}

func hTMLTableHasCellText(cell *html.Node) string {
	if cell == nil {
		return ""
	}
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			text := strings.TrimSpace(n.Data)
			if text != "" {
				parts = append(parts, text)
			}
		case html.ElementNode:
			if strings.EqualFold(n.Data, "br") || strings.EqualFold(n.Data, "p") {
				parts = append(parts, " ")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	for c := cell.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func RenderMarkdownRow(cells []string) string {
	escaped := make([]string, len(cells))
	for i, cell := range cells {
		cell = strings.ReplaceAll(cell, "|", `\|`)
		escaped[i] = strings.TrimSpace(cell)
	}
	return "| " + strings.Join(escaped, " | ") + " |"
}

func RenderMarkdownSeparator(n int) string {
	if n <= 0 {
		return "| --- |"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "---"
	}
	return "| " + strings.Join(parts, " | ") + " |"
}

func cleanupIntString(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	return string(buf[i:])
}
