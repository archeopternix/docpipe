package docpipe

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"docpipe/pkg/filetime"

	"gopkg.in/yaml.v3"
)

type mdFileNameParts struct {
	BaseStem string
	Language string
	Version  string
}

// ParseMarkdownFile loads a standalone markdown file (".md" or ".markdown") into a Markdown document.
//
// Behavior:
//   - Reads YAML frontmatter if present and merges with defaults.
//   - Sets default Title/Date/ChangedDate/Version when missing.
//   - Stores the original file bytes as both originalFile and markdownFile.
//   - Applies (normalizes/rewrites) YAML frontmatter.
//   - Generates a ZIP filename for later export.
func ParseMarkdownFile(path string) (*Markdown, error) {
	return ParseMarkdownFileContext(context.Background(), path, nil)
}

// ParseMarkdownFileContext loads a standalone markdown file with cancellation support.
func ParseMarkdownFileContext(ctx context.Context, path string, params *PowerPointParams) (*Markdown, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrInvalidInput, path, err)
	}

	switch mdNormalizeExtension(filepath.Ext(path)) {
	case ".md", ".markdown":
	default:
		return nil, fmt.Errorf("%w: markdown conversion not supported for %q", ErrUnsupported, filepath.Ext(path))
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrInvalidInput, path, err)
	}

	meta := *mdDefaultMetaData(path)
	if parsed, ok, err := mdParseMetaData(string(body), meta); err != nil {
		return nil, err
	} else if ok {
		meta = parsed
	}
	if meta.Title == "" {
		meta.Title = mdBaseStem(path)
	}
	if meta.Date == "" {
		meta.Date = info.ModTime().Format("2006-01-02")
	}
	if meta.ChangedDate == "" {
		meta.ChangedDate = info.ModTime().Format("2006-01-02")
	}
	if meta.Version == "" {
		meta.Version = "1.0"
	}

	doc := &Markdown{
		originalFile:     bytes.NewBuffer(append([]byte(nil), body...)),
		markdownFile:     bytes.NewBuffer(append([]byte(nil), body...)),
		extractedImages:  make(map[string]*bytes.Buffer),
		extractedSlides:  make(map[string]*bytes.Buffer),
		markdownVersions: make(map[string]*bytes.Buffer),
		metaData:         meta,
	}

	mdApplyMetaDataFrontmatter(doc)
	zipName, err := markdownZipFileName(mdFileName(doc.metaData))
	if err != nil {
		return nil, err
	}
	doc.fileName = zipName

	return doc, nil
}

func mdDefaultMetaData(path string) *MetaData {
	createdDate, _ := filetime.Formatted(path, "created", time.RFC3339)
	modifiedDate, _ := filetime.Formatted(path, "modified", time.RFC3339)
	parts := mdParseFileName(path)

	meta := MetaData{
		Title:            mdBaseStem(path),
		OriginalDocument: mdNormalizeOriginalDocumentPath(path),
		OriginalFormat:   strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		Date:             createdDate,
		ChangedDate:      modifiedDate,
		Version:          parts.Version,
		Language:         parts.Language,
	}
	return &meta
}

func mdParseMetaData(body string, defaults MetaData) (MetaData, bool, error) {
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	if !strings.HasPrefix(body, "---\n") {
		return defaults, false, nil
	}
	end := strings.Index(body[4:], "\n---\n")
	if end < 0 {
		return defaults, false, nil
	}
	frontmatter := body[4 : end+4]

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &raw); err != nil {
		return defaults, true, err
	}

	meta := defaults
	meta.Author = mdReadStringField(raw, "author")
	meta.Title = mdReadStringField(raw, "title")
	meta.Subtitle = mdReadStringField(raw, "subtitle")
	meta.Date = mdFirstNonEmpty(mdReadStringField(raw, "date"), mdReadStringField(raw, "created_date"), defaults.Date)
	meta.ChangedDate = mdFirstNonEmpty(mdReadStringField(raw, "changed_date"), defaults.ChangedDate)
	meta.OriginalDocument = mdFirstNonEmpty(mdReadStringField(raw, "original_document"), defaults.OriginalDocument)
	meta.OriginalFormat = mdFirstNonEmpty(mdReadStringField(raw, "original_format"), defaults.OriginalFormat)
	meta.Version = mdFirstNonEmpty(mdNormalizeVersion(mdReadStringField(raw, "version")), defaults.Version)
	meta.Language = mdFirstNonEmpty(defaults.Language, mdNormalizeLanguageCode(mdReadStringField(raw, "language")), mdNormalizeLanguageCode(mdReadStringField(raw, "lang")))
	meta.Abstract = mdReadStringField(raw, "abstract")
	meta.Keywords = mdNormalizeKeywords(raw["keywords"])

	return meta, true, nil
}

func mdApplyMetaDataFrontmatter(doc *Markdown) {
	if doc == nil {
		return
	}

	body := ""
	if doc.markdownFile != nil {
		body = doc.markdownFile.String()
	}
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	if strings.HasPrefix(body, "---\n") {
		if end := strings.Index(body[4:], "\n---\n"); end >= 0 {
			body = body[end+9:]
		}
	}
	body = strings.TrimLeft(body, "\n")

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString(fmt.Sprintf("%s: %q\n", "title", doc.metaData.Title))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "subtitle", doc.metaData.Subtitle))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "date", mdFormatFrontmatterDate(doc.metaData.Date)))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "changed_date", mdFormatFrontmatterDate(doc.metaData.ChangedDate)))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "original_document", mdNormalizeOriginalDocumentPath(doc.metaData.OriginalDocument)))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "original_format", doc.metaData.OriginalFormat))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "version", doc.metaData.Version))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "language", doc.metaData.Language))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "abstract", doc.metaData.Abstract))
	builder.WriteString("keywords:\n")
	for _, keyword := range mdNormalizeKeywords(doc.metaData.Keywords) {
		builder.WriteString(fmt.Sprintf("  - %q\n", keyword))
	}
	builder.WriteString(fmt.Sprintf("%s: %q\n", "author", doc.metaData.Author))
	builder.WriteString("---\n\n")
	builder.WriteString(strings.TrimRight(body, "\n"))
	builder.WriteString("\n")

	doc.markdownFile = bytes.NewBufferString(builder.String())
}

func mdParseFileName(name string) mdFileNameParts {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if stem == "" {
		return mdFileNameParts{}
	}

	matches := markdownFilenamePattern.FindStringSubmatch(stem)
	if len(matches) != 4 {
		return mdFileNameParts{BaseStem: stem}
	}

	return mdFileNameParts{
		BaseStem: strings.TrimSpace(matches[1]),
		Language: mdNormalizeLanguageCode(matches[2]),
		Version:  mdNormalizeVersion(matches[3]),
	}
}

func mdBaseStem(name string) string {
	parts := mdParseFileName(name)
	if strings.TrimSpace(parts.BaseStem) != "" {
		return parts.BaseStem
	}
	return strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
}

func mdNormalizeExtension(ext string) string {
	if ext == "" {
		return ""
	}
	normalized := strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(normalized, ".") {
		normalized = "." + normalized
	}
	return normalized
}

func mdNormalizeOriginalDocumentPath(path string) string {
	name := strings.TrimSpace(filepath.Base(path))
	if name == "" || name == "." {
		return "document/"
	}
	return filepath.ToSlash(filepath.Join("document", name))
}

func mdNormalizeLanguageCode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "xx"
	}

	switch normalized {
	case "english":
		return "en"
	case "german", "deutsch":
		return "de"
	}

	normalized = strings.ReplaceAll(normalized, "_", "-")
	if idx := strings.IndexByte(normalized, '-'); idx >= 0 {
		normalized = normalized[:idx]
	}
	if len(normalized) != 2 {
		return "xx"
	}
	for _, r := range normalized {
		if r < 'a' || r > 'z' {
			return "xx"
		}
	}
	return normalized
}

func mdNormalizeVersion(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(strings.TrimPrefix(normalized, "v"), "V")
	if !versionPattern.MatchString(normalized) {
		return ""
	}
	return normalized
}

func mdReadStringField(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func mdNormalizeKeywords(value any) []string {
	var parts []string
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		for _, segment := range strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == ';'
		}) {
			parts = append(parts, segment)
		}
	case []string:
		parts = append(parts, typed...)
	case []any:
		for _, item := range typed {
			parts = append(parts, fmt.Sprint(item))
		}
	default:
		parts = append(parts, fmt.Sprint(typed))
	}

	seen := map[string]bool{}
	keywords := make([]string, 0, len(parts))
	for _, part := range parts {
		keyword := strings.TrimSpace(part)
		if keyword == "" || seen[keyword] {
			continue
		}
		seen[keyword] = true
		keywords = append(keywords, keyword)
	}
	return keywords
}

func mdFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mdFormatFrontmatterDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	for _, layout := range []string{time.RFC3339, "2006-01-02", datetimelayout} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.Format(datetimelayout)
		}
	}

	return value
}

func mdFileName(meta MetaData) string {
	baseStem := strings.TrimSpace(meta.Title)
	if baseStem == "" {
		baseStem = "Document"
	}

	language := strings.ToLower(strings.TrimSpace(meta.Language))
	if language == "" {
		language = "xx"
	}
	switch language {
	case "english":
		language = "en"
	case "german", "deutsch":
		language = "de"
	}
	language = strings.ReplaceAll(language, "_", "-")
	if idx := strings.IndexByte(language, '-'); idx >= 0 {
		language = language[:idx]
	}
	if len(language) > 2 {
		language = language[:2]
	}
	if len(language) != 2 {
		language = "xx"
	}
	for _, r := range language {
		if r < 'a' || r > 'z' {
			language = "xx"
			break
		}
	}

	version := mdNormalizeVersion(meta.Version)
	if version == "" {
		version = "1.0"
	}

	return fmt.Sprintf("%s_%s_v%s.md", baseStem, language, version)
}
