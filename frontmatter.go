package docpipe

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	markdownFilenamePattern = regexp.MustCompile(`(?i)^(.*)_([a-z]{2})_v(\d+(?:\.\d+)*)$`)
	versionPattern          = regexp.MustCompile(`^\d+(?:\.\d+)*$`)
)

// datetimelayout is the canonical date-time format used when writing frontmatter dates.
const datetimelayout = "2006-01-02 15:04:05"

// Frontmatter represents the YAML metadata block at the top of a Markdown document ("--- ... ---").
type Frontmatter struct {
	Author           string
	Title            string
	Subtitle         string
	Date             string
	ChangedDate      string
	OriginalDocument string
	OriginalFormat   string
	Version          string
	Language         string
	Abstract         string
	Keywords         []string
}

type mdFileNameParts struct {
	BaseStem string
	Language string
	Version  string
}

// HasFrontmatter reports whether markdown starts with a YAML frontmatter opener ("---\n").
func hasFrontmatter(markdown string) bool {
	return strings.HasPrefix(normalizeMarkdownNewlines(markdown), "---\n")
}

// StripFrontmatter returns markdown without the leading YAML frontmatter block (if present).
func stripFrontmatter(markdown string) string {
	_, body, ok := mdSplitFrontmatterContent(markdown)
	if !ok {
		return normalizeMarkdownNewlines(markdown)
	}
	return body
}

// ParseFrontmatter parses a leading YAML frontmatter block into Frontmatter.
// Parameter: markdown is the full document text. Returns zero-value Frontmatter if no frontmatter exists.
func parseFrontmatter(markdown string) (Frontmatter, error) {
	meta, _, err := mdParseFrontmatter(markdown, Frontmatter{})
	return meta, err
}

func mdParseFrontmatter(body string, defaults Frontmatter) (Frontmatter, bool, error) {
	body = normalizeMarkdownNewlines(body)
	if !strings.HasPrefix(body, "---\n") {
		return defaults, false, nil
	}

	frontmatter, _, ok := mdSplitFrontmatterContent(body)
	if !ok {
		return defaults, true, fmt.Errorf("%w: frontmatter is not terminated", ErrInvalidInput)
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &raw); err != nil {
		return defaults, true, err
	}

	meta := defaults
	meta.Author = mdFirstNonEmpty(mdReadStringField(raw, "author"), defaults.Author)
	meta.Title = mdFirstNonEmpty(mdReadStringField(raw, "title"), defaults.Title)
	meta.Subtitle = mdFirstNonEmpty(mdReadStringField(raw, "subtitle"), defaults.Subtitle)
	meta.Date = mdFirstNonEmpty(mdReadStringField(raw, "date"), mdReadStringField(raw, "created_date"), defaults.Date)
	meta.ChangedDate = mdFirstNonEmpty(mdReadStringField(raw, "changed_date"), defaults.ChangedDate)
	meta.OriginalDocument = mdFirstNonEmpty(mdReadStringField(raw, "original_document"), defaults.OriginalDocument)
	meta.OriginalFormat = mdFirstNonEmpty(mdReadStringField(raw, "original_format"), defaults.OriginalFormat)
	meta.Version = mdFirstNonEmpty(mdNormalizeVersion(mdReadStringField(raw, "version")), defaults.Version)
	meta.Language = mdFirstNonEmpty(
		mdNormalizeLanguageCode(mdReadStringField(raw, "language")),
		mdNormalizeLanguageCode(mdReadStringField(raw, "lang")),
		defaults.Language,
	)
	meta.Abstract = mdFirstNonEmpty(mdReadStringField(raw, "abstract"), defaults.Abstract)
	if keywords := mdNormalizeKeywords(raw["keywords"]); len(keywords) > 0 {
		meta.Keywords = keywords
	}

	return meta, true, nil
}

func mdComposeMarkdownWithMeta(meta Frontmatter, body string) string {
	body = normalizeMarkdownNewlines(body)
	if _, stripped, ok := mdSplitFrontmatterContent(body); ok {
		body = stripped
	}
	body = strings.TrimLeft(body, "\n")

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString(fmt.Sprintf("%s: %q\n", "title", meta.Title))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "subtitle", meta.Subtitle))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "date", mdFormatFrontmatterDate(meta.Date)))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "changed_date", mdFormatFrontmatterDate(meta.ChangedDate)))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "original_document", mdNormalizeOriginalDocumentPath(meta.OriginalDocument)))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "original_format", meta.OriginalFormat))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "version", meta.Version))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "language", meta.Language))
	builder.WriteString(fmt.Sprintf("%s: %q\n", "abstract", meta.Abstract))
	builder.WriteString("keywords:\n")
	for _, keyword := range mdNormalizeKeywords(meta.Keywords) {
		builder.WriteString(fmt.Sprintf("  - %q\n", keyword))
	}
	builder.WriteString(fmt.Sprintf("%s: %q\n", "author", meta.Author))
	builder.WriteString("---\n\n")
	builder.WriteString(strings.TrimRight(body, "\n"))
	builder.WriteString("\n")
	return builder.String()
}

func mdDefaultFrontmatter(name string, modTime time.Time) Frontmatter {
	if strings.TrimSpace(name) == "" {
		name = "document.md"
	}
	parts := mdParseFileName(name)
	meta := Frontmatter{
		Title:            mdBaseStem(name),
		OriginalDocument: mdNormalizeOriginalDocumentPath(name),
		OriginalFormat:   strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."),
		Version:          parts.Version,
		Language:         parts.Language,
	}
	if !modTime.IsZero() {
		meta.Date = modTime.UTC().Format(time.RFC3339)
		meta.ChangedDate = modTime.UTC().Format(time.RFC3339)
	}
	return meta
}

func mdEnsureFrontmatterDefaults(meta Frontmatter, sourceName string, modTime time.Time) Frontmatter {
	defaults := mdDefaultFrontmatter(sourceName, modTime)
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = defaults.Title
	}
	if strings.TrimSpace(meta.OriginalDocument) == "" {
		meta.OriginalDocument = defaults.OriginalDocument
	}
	if strings.TrimSpace(meta.OriginalFormat) == "" {
		meta.OriginalFormat = defaults.OriginalFormat
	}
	if strings.TrimSpace(meta.Date) == "" {
		meta.Date = defaults.Date
	}
	if strings.TrimSpace(meta.ChangedDate) == "" {
		meta.ChangedDate = defaults.ChangedDate
	}
	if strings.TrimSpace(meta.Version) == "" {
		meta.Version = "1.0"
	}
	if strings.TrimSpace(meta.Language) == "" {
		meta.Language = defaults.Language
	}
	return meta
}

func mdMergeFrontmatter(next, current Frontmatter) Frontmatter {
	if next.Author == "" {
		next.Author = current.Author
	}
	if next.Title == "" {
		next.Title = current.Title
	}
	if next.Subtitle == "" {
		next.Subtitle = current.Subtitle
	}
	if next.Date == "" {
		next.Date = current.Date
	}
	if next.ChangedDate == "" {
		next.ChangedDate = current.ChangedDate
	}
	if next.OriginalDocument == "" {
		next.OriginalDocument = current.OriginalDocument
	}
	if next.OriginalFormat == "" {
		next.OriginalFormat = current.OriginalFormat
	}
	if next.Version == "" {
		next.Version = current.Version
	}
	if next.Language == "" {
		next.Language = current.Language
	}
	if next.Abstract == "" {
		next.Abstract = current.Abstract
	}
	if len(next.Keywords) == 0 && len(current.Keywords) > 0 {
		next.Keywords = append([]string(nil), current.Keywords...)
	}
	return next
}

func mdSplitFrontmatterContent(markdown string) (frontmatter, body string, ok bool) {
	normalized := normalizeMarkdownNewlines(markdown)
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

func mdNormalizeOriginalDocumentPath(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." {
		return "document/"
	}
	return filepath.ToSlash(filepath.Join("document", name))
}

func mdNormalizeLanguageCode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
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

func mdFileName(meta Frontmatter) string {
	baseStem := strings.TrimSpace(meta.Title)
	if baseStem == "" {
		baseStem = "Document"
	}

	language := mdNormalizeLanguageCode(meta.Language)
	if language == "" || language == "xx" {
		language = "xx"
	}

	version := mdNormalizeVersion(meta.Version)
	if version == "" {
		version = "1.0"
	}

	return fmt.Sprintf("%s_%s_v%s.md", baseStem, language, version)
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
	value := 0
	if _, err := fmt.Sscanf(parts[last], "%d", &value); err != nil {
		return normalized
	}
	parts[last] = fmt.Sprint(value + 1)
	return strings.Join(parts, ".")
}

func normalizeMarkdownNewlines(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
}
