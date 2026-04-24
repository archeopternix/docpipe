package docpipe

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	stdio "io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

var (
	markdownOfficeEscapedSpecialCharPattern = regexp.MustCompile(`\\([.,()\-:;!?&/\[\]{}#+=])`)
	markdownOfficeHTMLFigurePattern         = regexp.MustCompile(`(?is)</?figure[^>]*>`)
	markdownOfficeHTMLFigcaptionPattern     = regexp.MustCompile(`(?is)<figcaption[^>]*>.*?</figcaption>`)
	markdownOfficeHTMLImagePattern          = regexp.MustCompile(`(?is)<img[^>]*src="([^"]+)"[^>]*alt="([^"]*)"[^>]*/?>`)
	markdownOfficeImagePathPattern          = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	markdownOfficeTablePattern              = regexp.MustCompile(`(?is)<table\b[^>]*>.*?</table>`)
	markdownOfficeScriptPattern             = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	markdownOfficeStylePattern              = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	markdownOfficeTagPattern                = regexp.MustCompile(`(?is)<[^>]+>`)
)

type markdownOfficeCoreProperties struct {
	XMLName     xml.Name `xml:"coreProperties"`
	Title       string   `xml:"title"`
	Subject     string   `xml:"subject"`
	Creator     string   `xml:"creator"`
	Keywords    string   `xml:"keywords"`
	Description string   `xml:"description"`
	Language    string   `xml:"language"`
	Revision    string   `xml:"revision"`
}

func CreateFromWord(path string, params *WordParams) (*Markdown, error) {
	if params == nil {
		params = &WordParams{
			IncludeImages: true,
		}
	}

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	if markdownMDNormalizeExtension(filepath.Ext(path)) != ".docx" {
		return nil, fmt.Errorf("word conversion not supported for %q", filepath.Ext(path))
	}

	doc, err := markdownOfficeNewDocument(path)
	if err != nil {
		return nil, err
	}
	if err := markdownDocxConvertToMarkdown(path, doc, params); err != nil {
		return nil, err
	}
	doc.fileName = markdownMDZipFileName(markdownMDFileName(doc.metaData))

	return doc, nil
}

func markdownOfficeNewDocument(path string) (*Markdown, error) {
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	doc := &Markdown{
		originalFile:     bytes.NewBuffer(append([]byte(nil), original...)),
		extractedImages:  make(map[string]*bytes.Buffer),
		extractedSlides:  make(map[string]*bytes.Buffer),
		markdownVersions: make(map[string]*bytes.Buffer),
		metaData:         *markdownMDDefaultMetaData(path),
	}

	if err := markdownOfficeReadMetaData(path, &doc.metaData); err != nil {
		return nil, err
	}

	return doc, nil
}

func markdownDocxConvertToMarkdown(path string, doc *Markdown, params *WordParams) error {
	var mediaDir string
	if params.IncludeImages {
		var err error
		mediaDir, err = os.MkdirTemp("", "docx-media-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(mediaDir) }()
	}

	args := []string{
		path,
		"-t", "gfm",
		"--wrap=none",
	}
	if params.IncludeImages {
		args = append(args, "--extract-media="+mediaDir)
	}

	cmd := exec.Command("pandoc", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	body, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("pandoc failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return err
	}

	if params.IncludeImages {
		files, err := markdownDocxCollectExtractedMediaFiles(mediaDir)
		if err != nil {
			return err
		}
		for name, content := range files {
			doc.extractedImages[name] = bytes.NewBuffer(content)
		}
	}

	doc.markdownFile = bytes.NewBufferString(markdownOfficeCleanupMarkdownContent(string(body)))
	markdownMDApplyMetaDataFrontmatter(doc)

	return nil
}

func markdownDocxCollectExtractedMediaFiles(mediaDir string) (map[string][]byte, error) {
	files := map[string][]byte{}

	if err := filepath.Walk(mediaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(mediaDir, path)
		if err != nil {
			return err
		}
		relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "media/")

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(filepath.Join("media", relPath))] = body
		return nil
	}); err != nil {
		return nil, err
	}

	return files, nil
}

func markdownOfficeReadMetaData(path string, meta *MetaData) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()

	props, err := markdownOfficeReadCorePropertiesFromFiles(reader.File)
	if err != nil {
		return err
	}
	markdownOfficeApplyCoreProperties(meta, props)
	return nil
}

func markdownOfficeReadCorePropertiesFromFiles(files []*zip.File) (markdownOfficeCoreProperties, error) {
	for _, file := range files {
		if file.Name != "docProps/core.xml" {
			continue
		}
		return markdownOfficeReadCoreProperties(file)
	}
	return markdownOfficeCoreProperties{}, nil
}

func markdownOfficeReadCoreProperties(file *zip.File) (markdownOfficeCoreProperties, error) {
	var props markdownOfficeCoreProperties
	rc, err := file.Open()
	if err != nil {
		return props, err
	}
	defer rc.Close()

	decoder := xml.NewDecoder(rc)
	if err := decoder.Decode(&props); err != nil && err != stdio.EOF {
		return props, err
	}
	return props, nil
}

func markdownOfficeApplyCoreProperties(meta *MetaData, props markdownOfficeCoreProperties) {
	if meta == nil {
		return
	}
	if author := strings.TrimSpace(props.Creator); author != "" {
		meta.Author = author
	}
	if title := strings.TrimSpace(props.Title); title != "" {
		meta.Title = title
	}
	if meta.Version == "" {
		meta.Version = markdownMDNormalizeVersion(props.Revision)
	}
	if meta.Language == "" {
		meta.Language = markdownMDNormalizeLanguageCode(props.Language)
	}

	description := strings.TrimSpace(props.Description)
	if description != "" {
		meta.Abstract = description
	} else {
		meta.Abstract = strings.TrimSpace(props.Subject)
	}
	meta.Keywords = markdownMDNormalizeKeywords(props.Keywords)
}

func markdownOfficeCleanupMarkdownContent(input string) string {
	text := markdownOfficeNormalizeNewlines(input)
	text = markdownOfficeHTMLFigcaptionPattern.ReplaceAllString(text, "")
	text = markdownOfficeHTMLFigurePattern.ReplaceAllString(text, "")
	text = markdownOfficeConvertHTMLTables(text)
	text = markdownOfficeHTMLImagePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := markdownOfficeHTMLImagePattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + strings.TrimSpace(parts[2]) + "](" + markdownOfficeCleanupNormalizeMediaPath(parts[1]) + ")"
	})
	text = markdownOfficeImagePathPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := markdownOfficeImagePathPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + parts[1] + "](" + markdownOfficeCleanupNormalizeMediaPath(parts[2]) + ")"
	})
	text = markdownOfficeCleanupStripHTML(text)
	return markdownOfficeEscapedSpecialCharPattern.ReplaceAllString(text, "$1")
}

func markdownOfficeNormalizeNewlines(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	return strings.ReplaceAll(input, "\r", "\n")
}

func markdownOfficeCleanupNormalizeMediaPath(pathValue string) string {
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
		last = markdownOfficeCleanupSanitizeMediaSegment(segment)
	}
	if last == "" {
		last = "image"
	}
	return "/media/" + last
}

func markdownOfficeCleanupSanitizeMediaSegment(segment string) string {
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

func markdownOfficeConvertHTMLTables(input string) string {
	matches := markdownOfficeTablePattern.FindAllStringIndex(input, -1)
	if len(matches) == 0 {
		return input
	}

	var out bytes.Buffer
	last := 0
	for _, match := range matches {
		out.WriteString(input[last:match[0]])
		tableHTML := input[match[0]:match[1]]
		if markdownTable, ok := markdownOfficeTableHTMLToMarkdown(tableHTML); ok {
			out.WriteString(markdownTable)
		} else {
			out.WriteString(tableHTML)
		}
		last = match[1]
	}
	out.WriteString(input[last:])
	return out.String()
}

func markdownOfficeTableHTMLToMarkdown(tableHTML string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(tableHTML))
	if err != nil {
		return "", false
	}
	table := markdownOfficeHTMLFindFirst(doc, "table")
	if table == nil || markdownOfficeHTMLTableHasSpanAttrs(table) {
		return "", false
	}

	rows := markdownOfficeHTMLFindAll(markdownOfficeHTMLFindFirst(table, "thead"), "tr")
	rows = append(rows, markdownOfficeHTMLFindAll(markdownOfficeHTMLFindFirst(table, "tbody"), "tr")...)
	if len(rows) == 0 {
		rows = markdownOfficeHTMLFindAll(table, "tr")
	}
	if len(rows) == 0 {
		return "", false
	}

	headerCells := markdownOfficeHTMLExtractCells(rows[0])
	if len(headerCells) == 0 {
		return "", false
	}

	colCount := len(headerCells)
	hasTH := markdownOfficeHTMLRowHasTH(rows[0])
	header := make([]string, 0, colCount)
	for _, cell := range headerCells {
		header = append(header, markdownOfficeHTMLCellText(cell))
	}

	bodyStart := 1
	if !hasTH {
		header = make([]string, colCount)
		for i := range header {
			header[i] = "Col" + markdownOfficeIntString(i+1)
		}
		bodyStart = 0
	}

	var builder strings.Builder
	builder.WriteString(markdownOfficeRenderMarkdownRow(header))
	builder.WriteString("\n")
	builder.WriteString(markdownOfficeRenderMarkdownSeparator(colCount))
	builder.WriteString("\n")

	rowCount := 0
	for i := bodyStart; i < len(rows); i++ {
		cells := markdownOfficeHTMLExtractCells(rows[i])
		if len(cells) == 0 {
			continue
		}
		row := make([]string, colCount)
		for j := 0; j < colCount; j++ {
			if j < len(cells) {
				row[j] = markdownOfficeHTMLCellText(cells[j])
			}
		}
		builder.WriteString(markdownOfficeRenderMarkdownRow(row))
		builder.WriteString("\n")
		rowCount++
	}

	if rowCount == 0 && hasTH {
		values := make([]string, colCount)
		for i, cell := range headerCells {
			full := markdownOfficeHTMLCellText(cell)
			prefix := strings.TrimSpace(header[i]) + " "
			values[i] = strings.TrimSpace(strings.TrimPrefix(full, prefix))
		}
		builder.WriteString(markdownOfficeRenderMarkdownRow(values))
		builder.WriteString("\n")
	}

	return builder.String(), true
}

func markdownOfficeCleanupStripHTML(input string) string {
	text := markdownOfficeScriptPattern.ReplaceAllString(input, "")
	text = markdownOfficeStylePattern.ReplaceAllString(text, "")
	text = markdownOfficeTagPattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	return text
}

func markdownOfficeHTMLFindFirst(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, tag) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := markdownOfficeHTMLFindFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func markdownOfficeHTMLFindAll(n *html.Node, tag string) []*html.Node {
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

func markdownOfficeHTMLTableHasSpanAttrs(n *html.Node) bool {
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
		if markdownOfficeHTMLTableHasSpanAttrs(c) {
			return true
		}
	}
	return false
}

func markdownOfficeHTMLExtractCells(tr *html.Node) []*html.Node {
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

func markdownOfficeHTMLRowHasTH(tr *html.Node) bool {
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

func markdownOfficeHTMLCellText(cell *html.Node) string {
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

func markdownOfficeRenderMarkdownRow(cells []string) string {
	escaped := make([]string, len(cells))
	for i, cell := range cells {
		cell = strings.ReplaceAll(cell, "|", `\|`)
		escaped[i] = strings.TrimSpace(cell)
	}
	return "| " + strings.Join(escaped, " | ") + " |"
}

func markdownOfficeRenderMarkdownSeparator(n int) string {
	if n <= 0 {
		return "| --- |"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "---"
	}
	return "| " + strings.Join(parts, " | ") + " |"
}

func markdownOfficeIntString(v int) string {
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
