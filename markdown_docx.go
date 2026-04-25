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
	officeEscapedSpecialCharPattern = regexp.MustCompile(`\\([.,()\-:;!?&/\[\]{}#+=])`)
	officeHTMLFigurePattern         = regexp.MustCompile(`(?is)</?figure[^>]*>`)
	officeHTMLFigcaptionPattern     = regexp.MustCompile(`(?is)<figcaption[^>]*>.*?</figcaption>`)
	officeHTMLImagePattern          = regexp.MustCompile(`(?is)<img[^>]*src="([^"]+)"[^>]*alt="([^"]*)"[^>]*/?>`)
	officeImagePathPattern          = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	officeTablePattern              = regexp.MustCompile(`(?is)<table\b[^>]*>.*?</table>`)
	officeScriptPattern             = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	officeStylePattern              = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	officeTagPattern                = regexp.MustCompile(`(?is)<[^>]+>`)
)

type officeCoreProperties struct {
	XMLName     xml.Name `xml:"coreProperties"`
	Title       string   `xml:"title"`
	Subject     string   `xml:"subject"`
	Creator     string   `xml:"creator"`
	Keywords    string   `xml:"keywords"`
	Description string   `xml:"description"`
	Language    string   `xml:"language"`
	Revision    string   `xml:"revision"`
}

// WordParams and IncludeImages controls whether embedded images should be extracted and
// stored into the resulting Markdown ZIP (under /media).
type WordParams struct {
	IncludeImages bool
}

// ParseWordFile converts a .docx file into a Markdown ZIP document.
//
// Behavior:
//   - Only ".docx" is supported; other extensions return an error.
//   - Uses `pandoc` to convert the document to GitHub-Flavored Markdown (GFM)
//     with wrapping disabled.
//   - If params.IncludeImages is true, images are extracted and added to the
//     Markdown object under /media.
//   - Produces cleaned markdown and injects YAML frontmatter based on document
//     metadata.
//
// params:
//   - If params is nil, defaults are used (IncludeImages=true).
func ParseWordFile(path string, params *WordParams) (*Markdown, error) {
	if params == nil {
		params = &WordParams{
			IncludeImages: true,
		}
	}

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	if mdNormalizeExtension(filepath.Ext(path)) != ".docx" {
		return nil, fmt.Errorf("word conversion not supported for %q", filepath.Ext(path))
	}

	doc, err := officeNewDocument(path)
	if err != nil {
		return nil, err
	}
	var mediaDir string
	if params.IncludeImages {
		var err error
		mediaDir, err = os.MkdirTemp("", "docx-media-*")
		if err != nil {
			return nil, err
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
			return nil, fmt.Errorf("pandoc failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}

	if params.IncludeImages {
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
			doc.extractedImages[filepath.ToSlash(filepath.Join("media", relPath))] = bytes.NewBuffer(body)
			return nil
		}); err != nil {
			return nil, err
		}
	}

	doc.markdownFile = bytes.NewBufferString(officeCleanupMarkdownContent(string(body)))
	mdApplyMetaDataFrontmatter(doc)
	zipName, err := markdownZipFileName(mdFileName(doc.metaData))
	if err != nil {
		return nil, err
	}
	doc.fileName = zipName

	return doc, nil
}

// --------------------------------------------------------------------

func officeNewDocument(path string) (*Markdown, error) {
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	doc := &Markdown{
		originalFile:     bytes.NewBuffer(append([]byte(nil), original...)),
		extractedImages:  make(map[string]*bytes.Buffer),
		extractedSlides:  make(map[string]*bytes.Buffer),
		markdownVersions: make(map[string]*bytes.Buffer),
		metaData:         *mdDefaultMetaData(path),
	}

	var props officeCoreProperties
	for _, file := range reader.File {
		if file.Name != "docProps/core.xml" {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		decoder := xml.NewDecoder(rc)
		decodeErr := decoder.Decode(&props)
		closeErr := rc.Close()
		if decodeErr != nil && decodeErr != stdio.EOF {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		break
	}

	if author := strings.TrimSpace(props.Creator); author != "" {
		doc.metaData.Author = author
	}
	if title := strings.TrimSpace(props.Title); title != "" {
		doc.metaData.Title = title
	}
	if doc.metaData.Version == "" {
		doc.metaData.Version = mdNormalizeVersion(props.Revision)
	}
	if doc.metaData.Language == "" {
		doc.metaData.Language = mdNormalizeLanguageCode(props.Language)
	}

	description := strings.TrimSpace(props.Description)
	if description != "" {
		doc.metaData.Abstract = description
	} else {
		doc.metaData.Abstract = strings.TrimSpace(props.Subject)
	}
	doc.metaData.Keywords = mdNormalizeKeywords(props.Keywords)

	return doc, nil
}

func officeCleanupMarkdownContent(input string) string {
	text := strings.ReplaceAll(strings.ReplaceAll(input, "\r\n", "\n"), "\r", "\n")
	text = officeHTMLFigcaptionPattern.ReplaceAllString(text, "")
	text = officeHTMLFigurePattern.ReplaceAllString(text, "")
	matches := officeTablePattern.FindAllStringIndex(text, -1)
	if len(matches) > 0 {
		var out bytes.Buffer
		last := 0
		for _, match := range matches {
			out.WriteString(text[last:match[0]])
			tableHTML := text[match[0]:match[1]]

			markdownTable := ""
			ok := false
			htmlDoc, err := html.Parse(strings.NewReader(tableHTML))
			if err == nil {
				table := officeHTMLFindFirst(htmlDoc, "table")
				if table != nil && !officeHTMLTableHasSpanAttrs(table) {
					rows := officeHTMLFindAll(officeHTMLFindFirst(table, "thead"), "tr")
					rows = append(rows, officeHTMLFindAll(officeHTMLFindFirst(table, "tbody"), "tr")...)
					if len(rows) == 0 {
						rows = officeHTMLFindAll(table, "tr")
					}
					if len(rows) > 0 {
						headerCells := officeHTMLExtractCells(rows[0])
						if len(headerCells) > 0 {
							colCount := len(headerCells)
							hasTH := false
							for c := rows[0].FirstChild; c != nil; c = c.NextSibling {
								if c.Type == html.ElementNode && strings.EqualFold(c.Data, "th") {
									hasTH = true
									break
								}
							}
							header := make([]string, 0, colCount)
							for _, cell := range headerCells {
								header = append(header, officeHTMLCellText(cell))
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
							builder.WriteString(officeRenderMarkdownRow(header))
							builder.WriteString("\n")
							if colCount <= 0 {
								builder.WriteString("| --- |")
							} else {
								parts := make([]string, colCount)
								for i := range parts {
									parts[i] = "---"
								}
								builder.WriteString("| " + strings.Join(parts, " | ") + " |")
							}
							builder.WriteString("\n")

							rowCount := 0
							for i := bodyStart; i < len(rows); i++ {
								cells := officeHTMLExtractCells(rows[i])
								if len(cells) == 0 {
									continue
								}
								row := make([]string, colCount)
								for j := 0; j < colCount; j++ {
									if j < len(cells) {
										row[j] = officeHTMLCellText(cells[j])
									}
								}
								builder.WriteString(officeRenderMarkdownRow(row))
								builder.WriteString("\n")
								rowCount++
							}

							if rowCount == 0 && hasTH {
								values := make([]string, colCount)
								for i, cell := range headerCells {
									full := officeHTMLCellText(cell)
									prefix := strings.TrimSpace(header[i]) + " "
									values[i] = strings.TrimSpace(strings.TrimPrefix(full, prefix))
								}
								builder.WriteString(officeRenderMarkdownRow(values))
								builder.WriteString("\n")
							}

							markdownTable = builder.String()
							ok = true
						}
					}
				}
			}

			if ok {
				out.WriteString(markdownTable)
			} else {
				out.WriteString(tableHTML)
			}
			last = match[1]
		}
		out.WriteString(text[last:])
		text = out.String()
	}
	text = officeHTMLImagePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := officeHTMLImagePattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + strings.TrimSpace(parts[2]) + "](" + officeCleanupNormalizeMediaPath(parts[1]) + ")"
	})
	text = officeImagePathPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := officeImagePathPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + parts[1] + "](" + officeCleanupNormalizeMediaPath(parts[2]) + ")"
	})
	text = officeScriptPattern.ReplaceAllString(text, "")
	text = officeStylePattern.ReplaceAllString(text, "")
	text = officeTagPattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	return officeEscapedSpecialCharPattern.ReplaceAllString(text, "$1")
}

func officeCleanupNormalizeMediaPath(pathValue string) string {
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
		last = strings.Trim(builder.String(), "_")
		if last == "" {
			last = "file"
		}
	}
	if last == "" {
		last = "image"
	}
	return "/media/" + last
}

func officeHTMLFindFirst(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, tag) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := officeHTMLFindFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func officeHTMLFindAll(n *html.Node, tag string) []*html.Node {
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

func officeHTMLTableHasSpanAttrs(n *html.Node) bool {
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
		if officeHTMLTableHasSpanAttrs(c) {
			return true
		}
	}
	return false
}

func officeHTMLExtractCells(tr *html.Node) []*html.Node {
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

func officeHTMLCellText(cell *html.Node) string {
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

func officeRenderMarkdownRow(cells []string) string {
	escaped := make([]string, len(cells))
	for i, cell := range cells {
		cell = strings.ReplaceAll(cell, "|", `\|`)
		escaped[i] = strings.TrimSpace(cell)
	}
	return "| " + strings.Join(escaped, " | ") + " |"
}
