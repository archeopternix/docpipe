package docpipe

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"strings"
	"unicode"
)

type RenderOptions struct {
	AnchorifyHeadings bool
	RewriteImageURLs  func(orig string) (string, bool)
	SplitSections     bool
}

type Rendered struct {
	TitleHTML       string
	FrontmatterHTML string
	BodyHTML        string
}

type HeadingNode struct {
	Level    int
	Text     string
	AnchorID string
	Children []HeadingNode
}

// RenderHTML renders the markdown body to HTML. When SplitSections is true,
// title and frontmatter are returned separately from the markdown body.
func (m *Markdown) RenderHTML(opt RenderOptions) (Rendered, error) {
	if m == nil {
		return Rendered{}, fmt.Errorf("markdown is nil")
	}

	body, err := m.GetMarkdownBody()
	if err != nil {
		return Rendered{}, err
	}
	bodyHTML, err := markdownRenderHTML(body, opt)
	if err != nil {
		return Rendered{}, err
	}

	rendered := Rendered{BodyHTML: bodyHTML}
	if opt.SplitSections {
		rendered.TitleHTML = markdownRenderTitleHTML(m.metaData.Title, opt.AnchorifyHeadings)
		rendered.FrontmatterHTML = markdownRenderFrontmatterHTML(m.metaData)
	}
	return rendered, nil
}

// RenderFrontmatter returns the current metadata in a web-layer friendly form.
func (m *Markdown) RenderFrontmatter() (MetaData, error) {
	if m == nil {
		return MetaData{}, fmt.Errorf("markdown is nil")
	}
	return m.metaData, nil
}

// GetMarkdownBody returns the markdown content without YAML frontmatter.
func (m *Markdown) GetMarkdownBody() (string, error) {
	body, err := m.currentMarkdownString()
	if err != nil {
		return "", err
	}
	_, markdownBody, ok := mdSplitFrontmatterContent(body)
	if !ok {
		return body, nil
	}
	return markdownBody, nil
}

// SetFrontmatter replaces metadata and stores a versioned markdown update.
func (m *Markdown) SetFrontmatter(md MetaData) error {
	if m == nil {
		return fmt.Errorf("markdown is nil")
	}
	body, err := m.GetMarkdownBody()
	if err != nil {
		return err
	}
	if md.Version == "" {
		md.Version = m.metaData.Version
	}
	_, err = m.changeMarkdown(mdComposeMarkdownWithMeta(md, body))
	return err
}

// HeadingIndex extracts a deterministic heading tree from the markdown body.
func (m *Markdown) HeadingIndex(maxLevel int) ([]HeadingNode, error) {
	if m == nil {
		return nil, fmt.Errorf("markdown is nil")
	}
	if maxLevel <= 0 {
		maxLevel = 3
	}
	if maxLevel > 6 {
		maxLevel = 6
	}

	body, err := m.GetMarkdownBody()
	if err != nil {
		return nil, err
	}

	type treeNode struct {
		Level    int
		Text     string
		AnchorID string
		Children []*treeNode
	}

	var roots []*treeNode
	var stack []*treeNode
	anchors := newMarkdownAnchorGenerator()
	for _, heading := range markdownExtractHeadings(body, maxLevel) {
		node := &treeNode{
			Level:    heading.Level,
			Text:     heading.Text,
			AnchorID: anchors.Anchor(heading.Text),
		}

		for len(stack) > 0 && stack[len(stack)-1].Level >= node.Level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, node)
		} else {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
		}
		stack = append(stack, node)
	}

	var convert func(*treeNode) HeadingNode
	convert = func(node *treeNode) HeadingNode {
		out := HeadingNode{
			Level:    node.Level,
			Text:     node.Text,
			AnchorID: node.AnchorID,
		}
		for _, child := range node.Children {
			out.Children = append(out.Children, convert(child))
		}
		return out
	}

	index := make([]HeadingNode, 0, len(roots))
	for _, root := range roots {
		index = append(index, convert(root))
	}
	return index, nil
}

func mdSplitFrontmatterContent(markdown string) (frontmatter, body string, ok bool) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", normalized, false
	}

	tail := normalized[4:]
	if idx := strings.Index(tail, "\n---\n"); idx >= 0 {
		return tail[:idx], strings.TrimLeft(tail[idx+5:], "\n"), true
	}
	if strings.HasSuffix(tail, "\n---") {
		return strings.TrimSuffix(tail, "\n---"), "", true
	}
	return "", normalized, false
}

func mdComposeMarkdownWithMeta(meta MetaData, body string) string {
	doc := &Markdown{
		markdownFile: bytes.NewBufferString(body),
		metaData:     meta,
	}
	mdApplyMetaDataFrontmatter(doc)
	if doc.markdownFile == nil {
		return ""
	}
	return doc.markdownFile.String()
}

func markdownRenderTitleHTML(title string, anchorify bool) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if !anchorify {
		return "<h1>" + stdhtml.EscapeString(title) + "</h1>\n"
	}
	anchors := newMarkdownAnchorGenerator()
	return fmt.Sprintf("<h1 id=%q>%s</h1>\n", anchors.Anchor(title), stdhtml.EscapeString(title))
}

func markdownRenderFrontmatterHTML(meta MetaData) string {
	fields := []struct {
		Name  string
		Value string
	}{
		{"Author", meta.Author},
		{"Subtitle", meta.Subtitle},
		{"Date", meta.Date},
		{"Changed date", meta.ChangedDate},
		{"Original document", meta.OriginalDocument},
		{"Original format", meta.OriginalFormat},
		{"Version", meta.Version},
		{"Language", meta.Language},
		{"Abstract", meta.Abstract},
	}
	if len(meta.Keywords) > 0 {
		fields = append(fields, struct {
			Name  string
			Value string
		}{"Keywords", strings.Join(meta.Keywords, ", ")})
	}

	var builder strings.Builder
	builder.WriteString("<dl>\n")
	for _, field := range fields {
		if strings.TrimSpace(field.Value) == "" {
			continue
		}
		builder.WriteString("<dt>")
		builder.WriteString(stdhtml.EscapeString(field.Name))
		builder.WriteString("</dt><dd>")
		builder.WriteString(stdhtml.EscapeString(field.Value))
		builder.WriteString("</dd>\n")
	}
	builder.WriteString("</dl>\n")
	return builder.String()
}

func markdownRenderHTML(body string, opt RenderOptions) (string, error) {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"), "\n")
	anchors := newMarkdownAnchorGenerator()

	var out strings.Builder
	var paragraph []string
	listKind := ""
	inCode := false
	codeFence := ""

	closeList := func() {
		if listKind != "" {
			out.WriteString("</")
			out.WriteString(listKind)
			out.WriteString(">\n")
			listKind = ""
		}
	}
	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		out.WriteString("<p>")
		out.WriteString(markdownRenderInline(strings.Join(paragraph, " "), opt))
		out.WriteString("</p>\n")
		paragraph = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inCode {
			if strings.HasPrefix(trimmed, codeFence) {
				out.WriteString("</code></pre>\n")
				inCode = false
				codeFence = ""
				continue
			}
			out.WriteString(stdhtml.EscapeString(line))
			out.WriteByte('\n')
			continue
		}

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			flushParagraph()
			closeList()
			codeFence = trimmed[:3]
			out.WriteString("<pre><code>")
			inCode = true
			continue
		}
		if trimmed == "" {
			flushParagraph()
			closeList()
			continue
		}
		if level, text, ok := markdownParseHeading(line); ok {
			flushParagraph()
			closeList()
			out.WriteString(fmt.Sprintf("<h%d", level))
			if opt.AnchorifyHeadings {
				out.WriteString(fmt.Sprintf(" id=%q", anchors.Anchor(text)))
			}
			out.WriteString(">")
			out.WriteString(markdownRenderInline(text, opt))
			out.WriteString(fmt.Sprintf("</h%d>\n", level))
			continue
		}
		if item, ok := markdownUnorderedListItem(trimmed); ok {
			flushParagraph()
			if listKind != "ul" {
				closeList()
				out.WriteString("<ul>\n")
				listKind = "ul"
			}
			out.WriteString("<li>")
			out.WriteString(markdownRenderInline(item, opt))
			out.WriteString("</li>\n")
			continue
		}
		if item, ok := markdownOrderedListItem(trimmed); ok {
			flushParagraph()
			if listKind != "ol" {
				closeList()
				out.WriteString("<ol>\n")
				listKind = "ol"
			}
			out.WriteString("<li>")
			out.WriteString(markdownRenderInline(item, opt))
			out.WriteString("</li>\n")
			continue
		}

		closeList()
		paragraph = append(paragraph, trimmed)
	}

	flushParagraph()
	closeList()
	if inCode {
		out.WriteString("</code></pre>\n")
	}
	return out.String(), nil
}

type markdownHeading struct {
	Level int
	Text  string
}

func markdownExtractHeadings(body string, maxLevel int) []markdownHeading {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"), "\n")
	var headings []markdownHeading
	inCode := false
	codeFence := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inCode {
			if strings.HasPrefix(trimmed, codeFence) {
				inCode = false
				codeFence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCode = true
			codeFence = trimmed[:3]
			continue
		}
		level, text, ok := markdownParseHeading(line)
		if ok && level <= maxLevel {
			headings = append(headings, markdownHeading{Level: level, Text: markdownPlainText(text)})
		}
	}
	return headings
}

func markdownParseHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	level := 0
	for level < len(trimmed) && level < 6 && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trimmed) {
		return 0, "", false
	}
	if trimmed[level] != ' ' && trimmed[level] != '\t' {
		return 0, "", false
	}
	text := strings.TrimSpace(trimmed[level:])
	for strings.HasSuffix(text, "#") {
		withoutHashes := strings.TrimRight(text, "#")
		if withoutHashes == "" {
			break
		}
		last := withoutHashes[len(withoutHashes)-1]
		if last != ' ' && last != '\t' {
			break
		}
		text = strings.TrimSpace(withoutHashes)
	}
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}

func markdownUnorderedListItem(trimmed string) (string, bool) {
	if len(trimmed) < 3 {
		return "", false
	}
	if (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && (trimmed[1] == ' ' || trimmed[1] == '\t') {
		return strings.TrimSpace(trimmed[2:]), true
	}
	return "", false
}

func markdownOrderedListItem(trimmed string) (string, bool) {
	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(trimmed) {
		return "", false
	}
	if (trimmed[i] == '.' || trimmed[i] == ')') && (trimmed[i+1] == ' ' || trimmed[i+1] == '\t') {
		return strings.TrimSpace(trimmed[i+2:]), true
	}
	return "", false
}

func markdownRenderInline(text string, opt RenderOptions) string {
	return markdownRenderInlineDepth(text, opt, 0)
}

func markdownRenderInlineDepth(text string, opt RenderOptions, depth int) string {
	if text == "" {
		return ""
	}
	if depth > 8 {
		return stdhtml.EscapeString(text)
	}

	var out strings.Builder
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], "![") {
			if alt, dest, end, ok := markdownParseInlineDestination(text, i, true); ok {
				src := strings.TrimSpace(dest)
				if opt.RewriteImageURLs != nil {
					if rewritten, changed := opt.RewriteImageURLs(src); changed {
						src = rewritten
					}
				}
				out.WriteString(`<img src="`)
				out.WriteString(stdhtml.EscapeString(src))
				out.WriteString(`" alt="`)
				out.WriteString(stdhtml.EscapeString(markdownPlainText(alt)))
				out.WriteString(`">`)
				i = end
				continue
			}
		}
		if text[i] == '[' {
			if label, dest, end, ok := markdownParseInlineDestination(text, i, false); ok {
				out.WriteString(`<a href="`)
				out.WriteString(stdhtml.EscapeString(strings.TrimSpace(dest)))
				out.WriteString(`">`)
				out.WriteString(markdownRenderInlineDepth(label, opt, depth+1))
				out.WriteString(`</a>`)
				i = end
				continue
			}
		}
		if text[i] == '`' {
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				code := text[i+1 : i+1+end]
				out.WriteString("<code>")
				out.WriteString(stdhtml.EscapeString(code))
				out.WriteString("</code>")
				i += end + 2
				continue
			}
		}
		if strings.HasPrefix(text[i:], "**") {
			if end := strings.Index(text[i+2:], "**"); end >= 0 {
				out.WriteString("<strong>")
				out.WriteString(markdownRenderInlineDepth(text[i+2:i+2+end], opt, depth+1))
				out.WriteString("</strong>")
				i += end + 4
				continue
			}
		}
		if text[i] == '*' {
			if end := strings.IndexByte(text[i+1:], '*'); end >= 0 {
				out.WriteString("<em>")
				out.WriteString(markdownRenderInlineDepth(text[i+1:i+1+end], opt, depth+1))
				out.WriteString("</em>")
				i += end + 2
				continue
			}
		}

		next := markdownNextInlineSpecial(text, i+1)
		out.WriteString(stdhtml.EscapeString(text[i:next]))
		i = next
	}
	return out.String()
}

func markdownParseInlineDestination(text string, start int, image bool) (label, dest string, end int, ok bool) {
	labelStart := start + 1
	if image {
		labelStart = start + 2
	}
	closeLabel := strings.IndexByte(text[labelStart:], ']')
	if closeLabel < 0 {
		return "", "", 0, false
	}
	closeLabel += labelStart
	if closeLabel+1 >= len(text) || text[closeLabel+1] != '(' {
		return "", "", 0, false
	}
	destStart := closeLabel + 2
	closeDest := strings.IndexByte(text[destStart:], ')')
	if closeDest < 0 {
		return "", "", 0, false
	}
	closeDest += destStart
	dest = strings.TrimSpace(text[destStart:closeDest])
	if idx := strings.IndexAny(dest, " \t"); idx >= 0 {
		dest = dest[:idx]
	}
	return text[labelStart:closeLabel], strings.Trim(dest, `"'`), closeDest + 1, true
}

func markdownNextInlineSpecial(text string, start int) int {
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '!', '[', '`', '*':
			return i
		}
	}
	return len(text)
}

func markdownPlainText(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], "![") {
			if alt, _, end, ok := markdownParseInlineDestination(text, i, true); ok {
				out.WriteString(alt)
				i = end
				continue
			}
		}
		if text[i] == '[' {
			if label, _, end, ok := markdownParseInlineDestination(text, i, false); ok {
				out.WriteString(label)
				i = end
				continue
			}
		}
		if text[i] == '`' {
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				out.WriteString(text[i+1 : i+1+end])
				i += end + 2
				continue
			}
		}
		if strings.HasPrefix(text[i:], "**") {
			i += 2
			continue
		}
		if text[i] == '*' || text[i] == '_' {
			i++
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

type markdownAnchorGenerator struct {
	seen map[string]int
}

func newMarkdownAnchorGenerator() *markdownAnchorGenerator {
	return &markdownAnchorGenerator{seen: make(map[string]int)}
}

func (g *markdownAnchorGenerator) Anchor(text string) string {
	base := markdownSlugify(text)
	g.seen[base]++
	if g.seen[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, g.seen[base])
}

func markdownSlugify(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			if !lastHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
				lastHyphen = true
			}
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
			builder.WriteRune(r)
			lastHyphen = false
		case r == '-':
			if !lastHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "section"
	}
	return slug
}
