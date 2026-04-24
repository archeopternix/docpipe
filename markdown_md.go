package docpipe

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"docpipe/pkg/filetime"

	"gopkg.in/yaml.v3"
)

type markdownMDFileNameParts struct {
	BaseStem string
	Language string
	Version  string
	Matched  bool
}

func CreateFromMarkdown(path string) (*Markdown, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	switch markdownMDNormalizeExtension(filepath.Ext(path)) {
	case ".md", ".markdown":
	default:
		return nil, fmt.Errorf("markdown conversion not supported for %q", filepath.Ext(path))
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	doc := &Markdown{
		originalFile:     bytes.NewBuffer(append([]byte(nil), body...)),
		markdownFile:     bytes.NewBuffer(append([]byte(nil), body...)),
		extractedImages:  make(map[string]*bytes.Buffer),
		extractedSlides:  make(map[string]*bytes.Buffer),
		markdownVersions: make(map[string]*bytes.Buffer),
		metaData:         *markdownMDDefaultMetaData(path),
	}

	if err := markdownMDReadMetaData(path, &doc.metaData); err != nil {
		return nil, err
	}
	if doc.metaData.Version == "" {
		doc.metaData.Version = "1.0"
	}

	markdownMDApplyMetaDataFrontmatter(doc)
	doc.fileName = markdownMDZipFileName(markdownMDFileName(doc.metaData))

	return doc, nil
}

func markdownMDDefaultMetaData(path string) *MetaData {
	meta := MetaData{
		Title:            markdownMDBaseStem(path),
		OriginalDocument: markdownMDNormalizeOriginalDocumentPath(path),
		OriginalFormat:   strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		Date:             markdownMDFileCreatedDate(path),
		ChangedDate:      markdownMDFileModifiedDate(path),
		Version:          markdownMDParseVersionFromFile(path),
		Language:         markdownMDParseLanguageFromFile(path),
	}
	if meta.OriginalFormat == "md" || meta.OriginalFormat == "markdown" {
		parts := markdownMDParseFileName(path)
		meta.Version = parts.Version
		meta.Language = parts.Language
	}
	return &meta
}

func markdownMDReadMetaData(path string, meta *MetaData) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if parsed, ok, err := markdownMDParseMetaData(string(body), *meta); err != nil {
		return err
	} else if ok {
		*meta = parsed
	}

	if meta.Title == "" {
		meta.Title = markdownMDBaseStem(path)
	}
	if meta.Date == "" {
		meta.Date = info.ModTime().Format("2006-01-02")
	}
	if meta.ChangedDate == "" {
		meta.ChangedDate = info.ModTime().Format("2006-01-02")
	}

	return nil
}

func markdownMDParseMetaData(body string, defaults MetaData) (MetaData, bool, error) {
	frontmatter, ok := markdownMDExtractLeadingFrontmatter(markdownMDNormalizeFrontmatterNewlines(body))
	if !ok {
		return defaults, false, nil
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &raw); err != nil {
		return defaults, true, err
	}

	meta := defaults
	meta.Author = markdownMDReadStringField(raw, "author")
	meta.Title = markdownMDReadStringField(raw, "title")
	meta.Subtitle = markdownMDReadStringField(raw, "subtitle")
	meta.Date = markdownMDFirstNonEmpty(markdownMDReadStringField(raw, "date"), markdownMDReadStringField(raw, "created_date"), defaults.Date)
	meta.ChangedDate = markdownMDFirstNonEmpty(markdownMDReadStringField(raw, "changed_date"), defaults.ChangedDate)
	meta.OriginalDocument = markdownMDFirstNonEmpty(markdownMDReadStringField(raw, "original_document"), defaults.OriginalDocument)
	meta.OriginalFormat = markdownMDFirstNonEmpty(markdownMDReadStringField(raw, "original_format"), defaults.OriginalFormat)
	meta.Version = markdownMDFirstNonEmpty(defaults.Version, markdownMDNormalizeVersion(markdownMDReadStringField(raw, "version")))
	meta.Language = markdownMDFirstNonEmpty(defaults.Language, markdownMDNormalizeLanguageCode(markdownMDReadStringField(raw, "language")), markdownMDNormalizeLanguageCode(markdownMDReadStringField(raw, "lang")))
	meta.Abstract = markdownMDReadStringField(raw, "abstract")
	meta.Keywords = markdownMDReadKeywordsField(raw["keywords"])

	return meta, true, nil
}

func markdownMDApplyMetaDataFrontmatter(doc *Markdown) {
	if doc == nil {
		return
	}

	body := ""
	if doc.markdownFile != nil {
		body = doc.markdownFile.String()
	}
	body = strings.TrimLeft(markdownMDStripLeadingFrontmatter(markdownMDNormalizeFrontmatterNewlines(body)), "\n")

	var builder strings.Builder
	builder.WriteString("---\n")
	markdownMDWriteFrontmatterString(&builder, "title", doc.metaData.Title)
	markdownMDWriteFrontmatterString(&builder, "subtitle", doc.metaData.Subtitle)
	markdownMDWriteFrontmatterString(&builder, "date", markdownMDFormatFrontmatterDate(doc.metaData.Date))
	markdownMDWriteFrontmatterString(&builder, "changed_date", markdownMDFormatFrontmatterDate(doc.metaData.ChangedDate))
	markdownMDWriteFrontmatterString(&builder, "original_document", markdownMDNormalizeOriginalDocumentPath(doc.metaData.OriginalDocument))
	markdownMDWriteFrontmatterString(&builder, "original_format", doc.metaData.OriginalFormat)
	markdownMDWriteFrontmatterString(&builder, "version", doc.metaData.Version)
	markdownMDWriteFrontmatterString(&builder, "language", doc.metaData.Language)
	markdownMDWriteFrontmatterString(&builder, "abstract", doc.metaData.Abstract)
	markdownMDWriteFrontmatterKeywords(&builder, doc.metaData.Keywords)
	markdownMDWriteFrontmatterString(&builder, "author", doc.metaData.Author)
	builder.WriteString("---\n\n")
	builder.WriteString(strings.TrimRight(body, "\n"))
	builder.WriteString("\n")

	doc.markdownFile = bytes.NewBufferString(builder.String())
}

func markdownMDParseFileName(name string) markdownMDFileNameParts {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if stem == "" {
		return markdownMDFileNameParts{}
	}

	matches := markdownFilenamePattern.FindStringSubmatch(stem)
	if len(matches) != 4 {
		return markdownMDFileNameParts{BaseStem: stem}
	}

	return markdownMDFileNameParts{
		BaseStem: strings.TrimSpace(matches[1]),
		Language: markdownMDNormalizeLanguageCode(matches[2]),
		Version:  markdownMDNormalizeVersion(matches[3]),
		Matched:  true,
	}
}

func markdownMDBaseStem(name string) string {
	parts := markdownMDParseFileName(name)
	if strings.TrimSpace(parts.BaseStem) != "" {
		return parts.BaseStem
	}
	return strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
}

func markdownMDNormalizeExtension(ext string) string {
	if ext == "" {
		return ""
	}
	normalized := strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(normalized, ".") {
		normalized = "." + normalized
	}
	return normalized
}

func markdownMDNormalizeOriginalDocumentPath(path string) string {
	name := strings.TrimSpace(filepath.Base(path))
	if name == "" || name == "." {
		return "document/"
	}
	return filepath.ToSlash(filepath.Join("document", name))
}

func markdownMDNormalizeLanguageCode(value string) string {
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
		return ""
	}
	for _, r := range normalized {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return normalized
}

func markdownMDNormalizeVersion(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(strings.TrimPrefix(normalized, "v"), "V")
	if !versionPattern.MatchString(normalized) {
		return ""
	}
	return normalized
}

func markdownMDNormalizeMarkdownVersion(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(strings.TrimPrefix(normalized, "v"), "V")
	if !markdownVersionPattern.MatchString(normalized) {
		return ""
	}
	return normalized
}

func markdownMDNormalizeVersionLanguage(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "en"
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
	if len(normalized) > 2 {
		normalized = normalized[:2]
	}
	if len(normalized) != 2 {
		return "en"
	}
	for _, r := range normalized {
		if r < 'a' || r > 'z' {
			return "en"
		}
	}
	return normalized
}

func markdownMDParseLanguageFromFile(path string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if stem == "" {
		return ""
	}

	matches := markdownVersionFilePattern.FindStringSubmatch(stem)
	if len(matches) != 4 {
		return ""
	}

	return markdownMDNormalizeVersionLanguage(matches[2])
}

func markdownMDParseVersionFromFile(path string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if stem == "" {
		return ""
	}

	matches := markdownVersionFilePattern.FindStringSubmatch(stem)
	if len(matches) != 4 {
		return ""
	}

	return markdownMDNormalizeMarkdownVersion(matches[3])
}

func markdownMDExtractLeadingFrontmatter(body string) (string, bool) {
	if !strings.HasPrefix(body, "---\n") {
		return "", false
	}
	end := strings.Index(body[4:], "\n---\n")
	if end < 0 {
		return "", false
	}
	return body[4 : end+4], true
}

func markdownMDStripLeadingFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	end := strings.Index(body[4:], "\n---\n")
	if end < 0 {
		return body
	}
	return body[end+9:]
}

func markdownMDNormalizeFrontmatterNewlines(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	return strings.ReplaceAll(input, "\r", "\n")
}

func markdownMDReadStringField(values map[string]any, key string) string {
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

func markdownMDReadKeywordsField(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return markdownMDNormalizeKeywords(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = append(values, fmt.Sprint(item))
		}
		return markdownMDNormalizeKeywords(values)
	case []string:
		return markdownMDNormalizeKeywords(typed)
	default:
		return markdownMDNormalizeKeywords(fmt.Sprint(typed))
	}
}

func markdownMDNormalizeKeywords(value any) []string {
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

func markdownMDFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func markdownMDWriteFrontmatterString(builder *strings.Builder, key, value string) {
	builder.WriteString(fmt.Sprintf("%s: %q\n", key, value))
}

func markdownMDWriteFrontmatterKeywords(builder *strings.Builder, keywords []string) {
	builder.WriteString("keywords:\n")
	for _, keyword := range markdownMDNormalizeKeywords(keywords) {
		builder.WriteString(fmt.Sprintf("  - %q\n", keyword))
	}
}

func markdownMDFormatFrontmatterDate(value string) string {
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

func markdownMDFileCreatedDate(path string) string {
	date, _ := filetime.Formatted(path, "created", time.RFC3339)
	return date
}

func markdownMDFileModifiedDate(path string) string {
	date, _ := filetime.Formatted(path, "modified", time.RFC3339)
	return date
}

func markdownMDFileName(meta MetaData) string {
	baseStem := strings.TrimSpace(meta.Title)
	if baseStem == "" {
		baseStem = "Document"
	}

	language := markdownMDNormalizeVersionLanguage(meta.Language)
	if language == "" {
		language = "EN"
	}

	version := markdownMDNormalizeMarkdownVersion(meta.Version)
	if version == "" {
		version = "1.0"
	}

	return fmt.Sprintf("%s_%s_v%s.md", baseStem, language, version)
}

func markdownMDZipFileName(name string) string {
	base := strings.TrimSpace(filepath.Base(name))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		base = "document"
	}
	return base + ".zip"
}
