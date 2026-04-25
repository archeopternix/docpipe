package docpipe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

var (
	cleanEscapedSpecialCharPattern = regexp.MustCompile(`\\([.,()\-:;!?&/\[\]{}#+=])`)
	cleanHTMLFigurePattern         = regexp.MustCompile(`(?is)</?figure[^>]*>`)
	cleanHTMLFigcaptionPattern     = regexp.MustCompile(`(?is)<figcaption[^>]*>.*?</figcaption>`)
	cleanHTMLImagePattern          = regexp.MustCompile(`(?is)<img[^>]*src="([^"]+)"[^>]*alt="([^"]*)"[^>]*/?>`)
	cleanMarkdownImagePathPattern  = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	cleanTablePattern              = regexp.MustCompile(`(?is)<table\b[^>]*>.*?</table>`)
	cleanScriptPattern             = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	cleanStylePattern              = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	cleanTagPattern                = regexp.MustCompile(`(?is)<[^>]+>`)
)

type CleanUpParameters struct {
	UsingAI       bool
	CleanUpTables bool
}

type TranslationParameters struct {
	TargetLang string
	Rephrase   bool
}

type openAIResponsesRequest struct {
	Model        string `json:"model"`
	Instructions string `json:"instructions"`
	Input        string `json:"input"`
}

type openAIResponsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// LanguageDetectionAI detects the markdown document language using OpenAI and
// returns a two-letter language code such as "de" or "en".
//
// Requirements:
//   - usingAI must be true
//   - receiver and its markdown content must be non-nil
//   - AI integration must be active (see DetectAI)
//
// Returns a pointer to the detected ISO 639-1 code. If detection fails, an error is returned and the markdown is unchanged.
func (m *Markdown) LanguageDetectionAI(usingAI bool) (*string, error) {
	if !usingAI {
		return nil, fmt.Errorf("language detection by AI is disabled")
	}
	if m == nil || m.markdownFile == nil {
		return nil, fmt.Errorf("markdown is nil")
	}
	active, err := DetectAI()
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("AI is not active")
	}

	code, err := openAIText(
		`Detect the primary language of the markdown document. Return only the ISO 639-1 language code in lowercase, for example "de" or "en".`,
		m.markdownFile.String(),
	)
	if err != nil {
		return nil, err
	}

	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.Trim(code, "`'\" \n\r\t")
	if idx := strings.IndexAny(code, " \n\r\t.,;:"); idx >= 0 {
		code = code[:idx]
	}
	if len(code) != 2 {
		return nil, fmt.Errorf("openai returned invalid language code %q", code)
	}
	for _, r := range code {
		if r < 'a' || r > 'z' {
			return nil, fmt.Errorf("openai returned invalid language code %q", code)
		}
	}

	return &code, nil
}

// DetectAI checks whether the OpenAI integration is configured.
//
// It validates environment variables:
//   - OPENAI_API_KEY must be set and must not contain line breaks.
//   - OPENAI_MODEL may be empty; if set, it must not contain line breaks.
//
// Returns true if configuration looks usable, otherwise false.
func DetectAI() (bool, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return false, nil
	}
	if strings.ContainsAny(apiKey, "\r\n") {
		return false, fmt.Errorf("OPENAI_API_KEY contains invalid line breaks")
	}

	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if strings.ContainsAny(model, "\r\n") {
		return false, fmt.Errorf("OPENAI_MODEL contains invalid line breaks")
	}

	return true, nil
}

// CleanUpMarkdown normalizes markdown content and optionally asks OpenAI to
// reformat the document without changing its meaning.
//
// Behavior:
//   - Archives the current markdown into /versions before modifying.
//   - Performs deterministic cleanup (HTML stripping, optional table conversion, etc.).
//   - If UsingAI is true, calls OpenAI to reformat (must preserve content) and
//     then re-detects language via AI.
//   - If UsingAI is false, sets MetaData.Language to "xx".
//   - Bumps the minor version and re-applies YAML frontmatter.
func (m *Markdown) CleanUpMarkdown(param *CleanUpParameters) error {
	if m == nil || m.markdownFile == nil {
		return fmt.Errorf("markdown is nil")
	}
	if param == nil {
		param = &CleanUpParameters{CleanUpTables: true}
	}

	if err := m.archiveCurrentMarkdown(); err != nil {
		return err
	}

	text := cleanMarkdownContent(m.markdownFile.String(), param.CleanUpTables)
	if param.UsingAI {
		cleaned, err := openAIText(
			`Clean and reformat this markdown document. Preserve all content, facts, headings, links, images, code blocks, and YAML frontmatter values. Do not summarize, omit, translate, or add content. Return only the cleaned markdown.`,
			text,
		)
		if err != nil {
			return err
		}
		text = cleaned
	}

	previousMarkdown := m.markdownFile
	m.markdownFile = bytes.NewBufferString(text)
	if param.UsingAI {
		code, err := m.LanguageDetectionAI(true)
		if err != nil {
			m.markdownFile = previousMarkdown
			return err
		}
		m.metaData.Language = *code
	} else {
		m.metaData.Language = "xx"
	}
	m.metaData.Version = bumpMinorVersion(m.metaData.Version)
	mdApplyMetaDataFrontmatter(m)

	return nil
}

// TranslateTo translates the markdown to TargetLang using OpenAI. If Rephrase
// is true, idiomatic phrasing may be adapted while preserving the meaning.
//
// Behavior:
//   - Archives the current markdown into /versions before modifying.
//   - Preserves markdown structure, YAML keys, links, image paths, code blocks,
//     tables, and factual content.
//   - Updates metadata language (normalized) and re-applies YAML frontmatter.
func (m *Markdown) TranslateTo(param *TranslationParameters) error {
	if m == nil || m.markdownFile == nil {
		return fmt.Errorf("markdown is nil")
	}
	if param == nil || strings.TrimSpace(param.TargetLang) == "" {
		return fmt.Errorf("target language is missing")
	}

	if err := m.archiveCurrentMarkdown(); err != nil {
		return err
	}

	targetLang := strings.ToLower(strings.TrimSpace(param.TargetLang))
	instruction := fmt.Sprintf(
		`Translate this markdown document to %s. Preserve markdown structure, YAML frontmatter keys, links, image paths, code blocks, tables, and all factual content. Return only the translated markdown.`,
		targetLang,
	)
	if param.Rephrase {
		instruction = fmt.Sprintf(
			`Translate this markdown document to %s. Use natural idiomatic phrasing in the target language while preserving meaning, markdown structure, YAML frontmatter keys, links, image paths, code blocks, tables, and all factual content. Return only the translated markdown.`,
			targetLang,
		)
	}

	translated, err := openAIText(instruction, m.markdownFile.String())
	if err != nil {
		return err
	}

	m.markdownFile = bytes.NewBufferString(translated)
	if normalized := mdNormalizeLanguageCode(targetLang); normalized != "" {
		m.metaData.Language = normalized
	} else {
		m.metaData.Language = "xx"
	}
	mdApplyMetaDataFrontmatter(m)
	return nil
}

func openAIText(instructions, input string) (string, error) {
	active, err := DetectAI()
	if err != nil {
		return "", err
	}
	if !active {
		return "", fmt.Errorf("AI is not active")
	}

	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = "gpt-4.1-mini"
	}

	body, err := json.Marshal(openAIResponsesRequest{
		Model:        model,
		Instructions: instructions,
		Input:        input,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var parsed openAIResponsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("openai error: %s", strings.TrimSpace(parsed.Error.Message))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai request failed: %s", resp.Status)
	}
	if strings.TrimSpace(parsed.OutputText) != "" {
		return strings.TrimSpace(parsed.OutputText), nil
	}

	var builder strings.Builder
	for _, output := range parsed.Output {
		if output.Type != "" && output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Text != "" {
				builder.WriteString(content.Text)
			}
		}
	}

	text := strings.TrimSpace(builder.String())
	if text == "" {
		return "", fmt.Errorf("openai response did not contain text output")
	}
	return text, nil
}

func (m *Markdown) archiveCurrentMarkdown() error {
	if m == nil || m.markdownFile == nil {
		return fmt.Errorf("markdown is nil")
	}
	if m.markdownVersions == nil {
		m.markdownVersions = make(map[string]*bytes.Buffer)
	}

	name := strings.TrimSpace(m.GetMarkdownFileName())
	if name == "" {
		name = "document.md"
	}
	m.markdownVersions[name] = bytes.NewBuffer(append([]byte(nil), m.markdownFile.Bytes()...))
	return nil
}

func bumpMinorVersion(version string) string {
	normalized := mdNormalizeVersion(version)
	if normalized == "" {
		return "1.1"
	}

	parts := strings.Split(normalized, ".")
	if len(parts) == 1 {
		return parts[0] + ".1"
	}

	last := len(parts) - 1
	value, err := strconv.Atoi(parts[last])
	if err != nil {
		return normalized
	}
	parts[last] = strconv.Itoa(value + 1)
	return strings.Join(parts, ".")
}

func cleanMarkdownContent(input string, cleanTables bool) string {
	text := strings.ReplaceAll(strings.ReplaceAll(input, "\r\n", "\n"), "\r", "\n")
	text = cleanHTMLFigcaptionPattern.ReplaceAllString(text, "")
	text = cleanHTMLFigurePattern.ReplaceAllString(text, "")
	if cleanTables {
		text = cleanConvertHTMLTables(text)
	}
	text = cleanHTMLImagePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := cleanHTMLImagePattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + strings.TrimSpace(parts[2]) + "](" + cleanNormalizeMediaPath(parts[1]) + ")"
	})
	text = cleanMarkdownImagePathPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := cleanMarkdownImagePathPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return "![" + parts[1] + "](" + cleanNormalizeMediaPath(parts[2]) + ")"
	})
	text = cleanStripHTML(text)
	return cleanEscapedSpecialCharPattern.ReplaceAllString(text, "$1")
}

func cleanNormalizeMediaPath(pathValue string) string {
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
		last = cleanSanitizeMediaSegment(segment)
	}
	if last == "" {
		last = "image"
	}
	return "/media/" + last
}

func cleanSanitizeMediaSegment(segment string) string {
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

func cleanConvertHTMLTables(input string) string {
	matches := cleanTablePattern.FindAllStringIndex(input, -1)
	if len(matches) == 0 {
		return input
	}

	var out bytes.Buffer
	last := 0
	for _, match := range matches {
		out.WriteString(input[last:match[0]])
		tableHTML := input[match[0]:match[1]]
		if markdownTable, ok := cleanTableHTMLToMarkdown(tableHTML); ok {
			out.WriteString(markdownTable)
		} else {
			out.WriteString(tableHTML)
		}
		last = match[1]
	}
	out.WriteString(input[last:])
	return out.String()
}

func cleanTableHTMLToMarkdown(tableHTML string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(tableHTML))
	if err != nil {
		return "", false
	}
	table := cleanHTMLFindFirst(doc, "table")
	if table == nil || cleanHTMLTableHasSpanAttrs(table) {
		return "", false
	}

	rows := cleanHTMLFindAll(cleanHTMLFindFirst(table, "thead"), "tr")
	rows = append(rows, cleanHTMLFindAll(cleanHTMLFindFirst(table, "tbody"), "tr")...)
	if len(rows) == 0 {
		rows = cleanHTMLFindAll(table, "tr")
	}
	if len(rows) == 0 {
		return "", false
	}

	headerCells := cleanHTMLExtractCells(rows[0])
	if len(headerCells) == 0 {
		return "", false
	}

	colCount := len(headerCells)
	hasTH := cleanHTMLRowHasTH(rows[0])
	header := make([]string, 0, colCount)
	for _, cell := range headerCells {
		header = append(header, cleanHTMLCellText(cell))
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
	builder.WriteString(cleanRenderMarkdownRow(header))
	builder.WriteString("\n")
	builder.WriteString(cleanRenderMarkdownSeparator(colCount))
	builder.WriteString("\n")

	rowCount := 0
	for i := bodyStart; i < len(rows); i++ {
		cells := cleanHTMLExtractCells(rows[i])
		if len(cells) == 0 {
			continue
		}
		row := make([]string, colCount)
		for j := 0; j < colCount; j++ {
			if j < len(cells) {
				row[j] = cleanHTMLCellText(cells[j])
			}
		}
		builder.WriteString(cleanRenderMarkdownRow(row))
		builder.WriteString("\n")
		rowCount++
	}

	if rowCount == 0 && hasTH {
		values := make([]string, colCount)
		for i, cell := range headerCells {
			full := cleanHTMLCellText(cell)
			prefix := strings.TrimSpace(header[i]) + " "
			values[i] = strings.TrimSpace(strings.TrimPrefix(full, prefix))
		}
		builder.WriteString(cleanRenderMarkdownRow(values))
		builder.WriteString("\n")
	}

	return builder.String(), true
}

func cleanStripHTML(input string) string {
	text := cleanScriptPattern.ReplaceAllString(input, "")
	text = cleanStylePattern.ReplaceAllString(text, "")
	text = cleanTagPattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	return text
}

func cleanHTMLFindFirst(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, tag) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := cleanHTMLFindFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func cleanHTMLFindAll(n *html.Node, tag string) []*html.Node {
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

func cleanHTMLTableHasSpanAttrs(n *html.Node) bool {
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
		if cleanHTMLTableHasSpanAttrs(c) {
			return true
		}
	}
	return false
}

func cleanHTMLExtractCells(tr *html.Node) []*html.Node {
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

func cleanHTMLRowHasTH(tr *html.Node) bool {
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

func cleanHTMLCellText(cell *html.Node) string {
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

func cleanRenderMarkdownRow(cells []string) string {
	escaped := make([]string, len(cells))
	for i, cell := range cells {
		cell = strings.ReplaceAll(cell, "|", `\|`)
		escaped[i] = strings.TrimSpace(cell)
	}
	return "| " + strings.Join(escaped, " | ") + " |"
}

func cleanRenderMarkdownSeparator(n int) string {
	if n <= 0 {
		return "| --- |"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "---"
	}
	return "| " + strings.Join(parts, " | ") + " |"
}
