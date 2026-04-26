package clean

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

type Options struct {
	CleanTables bool
}

func Normalize(input string, opts Options) string {
	return normalizeMarkdownContent(input, opts.CleanTables)
}

var (
	escapedSpecialCharPattern = regexp.MustCompile(`\\([.,()\-:;!?&/\[\]{}#+=])`)
	htmlFigurePattern         = regexp.MustCompile(`(?is)</?figure[^>]*>`)
	htmlFigcaptionPattern     = regexp.MustCompile(`(?is)<figcaption[^>]*>.*?</figcaption>`)
	htmlImagePattern          = regexp.MustCompile(`(?is)<img[^>]*src="([^"]+)"[^>]*alt="([^"]*)"[^>]*/?>`)
	markdownImagePathPattern  = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	tablePattern              = regexp.MustCompile(`(?is)<table\b[^>]*>.*?</table>`)
	scriptPattern             = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	stylePattern              = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	tagPattern                = regexp.MustCompile(`(?is)<[^>]+>`)
)

func normalizeMarkdownContent(input string, cleanTables bool) string {
	text := strings.ReplaceAll(strings.ReplaceAll(input, "\r\n", "\n"), "\r", "\n")
	text = htmlFigcaptionPattern.ReplaceAllString(text, "")
	text = htmlFigurePattern.ReplaceAllString(text, "")
	if cleanTables {
		text = convertHTMLTables(text)
	}
	text = htmlImagePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := htmlImagePattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + strings.TrimSpace(parts[2]) + "](" + normalizeMediaPath(parts[1]) + ")"
	})
	text = markdownImagePathPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := markdownImagePathPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + parts[1] + "](" + normalizeMediaPath(parts[2]) + ")"
	})
	text = stripHTML(text)
	return escapedSpecialCharPattern.ReplaceAllString(text, "$1")
}

func normalizeMediaPath(pathValue string) string {
	pathValue = strings.TrimSpace(strings.Trim(pathValue, `"'`))
	pathValue = strings.ReplaceAll(pathValue, "%5C", "/")
	pathValue = strings.ReplaceAll(pathValue, "%5c", "/")
	pathValue = strings.ReplaceAll(pathValue, "_5C", "/")
	pathValue = strings.ReplaceAll(pathValue, "_5c", "/")
	pathValue = strings.ReplaceAll(pathValue, "\\", "/")
	segments := strings.Split(pathValue, "/")
	last := ""
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		last = sanitizeMediaSegment(segment)
	}
	if last == "" {
		last = "image"
	}
	return "/media/" + last
}

func sanitizeMediaSegment(segment string) string {
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
		if markdownTable, ok := tableHTMLToMarkdown(tableHTML); ok {
			out.WriteString(markdownTable)
		} else {
			out.WriteString(tableHTML)
		}
		last = match[1]
	}
	out.WriteString(input[last:])
	return out.String()
}

func tableHTMLToMarkdown(tableHTML string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(tableHTML))
	if err != nil {
		return "", false
	}
	table := htmlFindFirst(doc, "table")
	if table == nil || htmlTableHasSpanAttrs(table) {
		return "", false
	}

	rows := htmlFindAll(htmlFindFirst(table, "thead"), "tr")
	rows = append(rows, htmlFindAll(htmlFindFirst(table, "tbody"), "tr")...)
	if len(rows) == 0 {
		rows = htmlFindAll(table, "tr")
	}
	if len(rows) == 0 {
		return "", false
	}

	headerCells := htmlExtractCells(rows[0])
	if len(headerCells) == 0 {
		return "", false
	}

	colCount := len(headerCells)
	hasTH := htmlRowHasTH(rows[0])
	header := make([]string, 0, colCount)
	for _, cell := range headerCells {
		header = append(header, htmlCellText(cell))
	}

	bodyStart := 1
	if !hasTH {
		header = make([]string, colCount)
		for i := range header {
			header[i] = fmt.Sprintf("Col%d", i+1)
		}
		bodyStart = 0
	}

	var builder strings.Builder
	builder.WriteString(renderMarkdownRow(header))
	builder.WriteString("\n")
	builder.WriteString(renderMarkdownSeparator(colCount))
	builder.WriteString("\n")

	rowCount := 0
	for i := bodyStart; i < len(rows); i++ {
		cells := htmlExtractCells(rows[i])
		if len(cells) == 0 {
			continue
		}
		row := make([]string, colCount)
		for j := 0; j < colCount; j++ {
			if j < len(cells) {
				row[j] = htmlCellText(cells[j])
			}
		}
		builder.WriteString(renderMarkdownRow(row))
		builder.WriteString("\n")
		rowCount++
	}

	if rowCount == 0 && hasTH {
		values := make([]string, colCount)
		for i, cell := range headerCells {
			full := htmlCellText(cell)
			prefix := strings.TrimSpace(header[i]) + " "
			values[i] = strings.TrimSpace(strings.TrimPrefix(full, prefix))
		}
		builder.WriteString(renderMarkdownRow(values))
		builder.WriteString("\n")
	}

	return builder.String(), true
}

func stripHTML(input string) string {
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

func htmlFindFirst(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, tag) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := htmlFindFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func htmlFindAll(n *html.Node, tag string) []*html.Node {
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

func htmlTableHasSpanAttrs(n *html.Node) bool {
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
		if htmlTableHasSpanAttrs(c) {
			return true
		}
	}
	return false
}

func htmlExtractCells(tr *html.Node) []*html.Node {
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

func htmlRowHasTH(tr *html.Node) bool {
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

func htmlCellText(cell *html.Node) string {
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

func renderMarkdownRow(cells []string) string {
	escaped := make([]string, len(cells))
	for i, cell := range cells {
		cell = strings.ReplaceAll(cell, "|", `\|`)
		escaped[i] = strings.TrimSpace(cell)
	}
	return "| " + strings.Join(escaped, " | ") + " |"
}

func renderMarkdownSeparator(n int) string {
	if n <= 0 {
		return "| --- |"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "---"
	}
	return "| " + strings.Join(parts, " | ") + " |"
}
