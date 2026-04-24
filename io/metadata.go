package io

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	stdio "io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"docpipe/pkg/filetime"

	"gopkg.in/yaml.v3"
)

var (
	markdownFilenamePattern = regexp.MustCompile(`(?i)^(.*)_([a-z]{2})_v(\d+(?:\.\d+)*)$`)
	versionPattern          = regexp.MustCompile(`^\d+(?:\.\d+)*$`)
	datetimelayout          = "2006-01-02 15:04:05"
)

// MetaData holds the metadata information of a document.
type MetaData struct {
	Author           string
	Title            string
	Subtitle         string
	Date             string // Created date
	ChangedDate      string // Last changed date
	OriginalDocument string
	OriginalFormat   string
	Version          string
	Language         string
	Abstract         string
	Keywords         []string
}

type markdownFileNameParts struct {
	BaseStem string
	Language string
	Version  string
	Matched  bool
}

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

func ApplyMetaDataFrontmatter(body string, meta *MetaData) string {
	body = strings.TrimLeft(stripLeadingFrontmatter(normalizeFrontmatterNewlines(body)), "\n")

	var builder strings.Builder
	builder.WriteString("---\n")
	writeFrontmatterString(&builder, "title", meta.Title)
	writeFrontmatterString(&builder, "subtitle", meta.Subtitle)
	writeFrontmatterString(&builder, "date", formatFrontmatterDate(meta.Date))
	writeFrontmatterString(&builder, "changed_date", formatFrontmatterDate(meta.ChangedDate))
	writeFrontmatterString(&builder, "original_document", normalizeOriginalDocumentPath(meta.OriginalDocument))
	writeFrontmatterString(&builder, "original_format", meta.OriginalFormat)
	writeFrontmatterString(&builder, "version", meta.Version)
	writeFrontmatterString(&builder, "language", meta.Language)
	writeFrontmatterString(&builder, "abstract", meta.Abstract)
	writeFrontmatterKeywords(&builder, meta.Keywords)
	writeFrontmatterString(&builder, "author", meta.Author)
	builder.WriteString("---\n\n")
	builder.WriteString(strings.TrimRight(body, "\n"))
	builder.WriteString("\n")
	return builder.String()
}

func PopulateMetaData(path string, meta *MetaData) error {
	switch normalizeExtension(filepath.Ext(path)) {
	case ".docx", ".pptx":
		if err := readOfficeMetaData(path, meta); err != nil {
			return err
		}
		return nil
	case ".md", ".markdown":
		if err := readMarkdownMetaData(path, meta); err != nil {
			return err
		}
		return nil
	case ".txt":
		return nil
	default:
		return fmt.Errorf("metadata extraction not supported for %q", filepath.Ext(path))
	}
}

// ParseFileNameFromMetaData generates a Markdown file name based on the metadata information.
func (m MetaData) ParseFileNameFromMetaData() string {

	baseStem := strings.TrimSpace(strings.TrimSpace(m.Title))
	if baseStem == "" {
		baseStem = "Document"
	}

	language := normalizeVersionLanguage(m.Language)
	if language == "" {
		language = "EN"
	}

	version := normalizeMarkdownVersion(m.Version)
	if version == "" {
		version = "1.0"
	}

	return fmt.Sprintf("%s_%s_v%s.md", baseStem, language, version)
}

//----------------------------------------------------------------------------

func stripLeadingFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	end := strings.Index(body[4:], "\n---\n")
	if end < 0 {
		return body
	}
	return body[end+9:]
}

func writeFrontmatterString(builder *strings.Builder, key, value string) {
	builder.WriteString(fmt.Sprintf("%s: %q\n", key, value))
}

func writeFrontmatterKeywords(builder *strings.Builder, keywords []string) {
	builder.WriteString("keywords:\n")
	for _, keyword := range normalizeKeywords(keywords) {
		builder.WriteString(fmt.Sprintf("  - %q\n", keyword))
	}
}

func formatFrontmatterDate(value string) string {
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

func normalizeOriginalDocumentPath(path string) string {
	name := strings.TrimSpace(filepath.Base(path))
	if name == "" || name == "." {
		return "document/"
	}
	return filepath.ToSlash(filepath.Join("document", name))
}

func normalizeLanguageCode(value string) string {
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

func normalizeVersion(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(strings.TrimPrefix(normalized, "v"), "V")
	if !versionPattern.MatchString(normalized) {
		return ""
	}
	return normalized
}

func parseMarkdownFileName(name string) markdownFileNameParts {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if stem == "" {
		return markdownFileNameParts{}
	}

	matches := markdownFilenamePattern.FindStringSubmatch(stem)
	if len(matches) != 4 {
		return markdownFileNameParts{BaseStem: stem}
	}

	return markdownFileNameParts{
		BaseStem: strings.TrimSpace(matches[1]),
		Language: normalizeLanguageCode(matches[2]),
		Version:  normalizeVersion(matches[3]),
		Matched:  true,
	}
}

func markdownBaseStem(name string) string {
	parts := parseMarkdownFileName(name)
	if strings.TrimSpace(parts.BaseStem) != "" {
		return parts.BaseStem
	}
	return strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
}

func normalizeExtension(ext string) string {
	if ext == "" {
		return ""
	}
	normalized := strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(normalized, ".") {
		normalized = "." + normalized
	}
	return normalized
}

func readMarkdownMetaData(path string, meta *MetaData) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if parsed, ok, err := parseMarkdownMetaData(string(body), *meta); err != nil {
		return err
	} else if ok {
		*meta = parsed
	}

	if meta.Title == "" {
		meta.Title = markdownBaseStem(path)
	}
	if meta.Date == "" {
		meta.Date = info.ModTime().Format("2006-01-02")
	}
	if meta.ChangedDate == "" {
		meta.ChangedDate = info.ModTime().Format("2006-01-02")
	}

	return nil
}

func parseMarkdownMetaData(body string, defaults MetaData) (MetaData, bool, error) {
	frontmatter, ok := extractLeadingFrontmatter(normalizeFrontmatterNewlines(body))
	if !ok {
		return defaults, false, nil
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &raw); err != nil {
		return defaults, true, err
	}

	meta := defaults

	meta.Author = readStringField(raw, "author")
	meta.Title = readStringField(raw, "title")
	meta.Subtitle = readStringField(raw, "subtitle")
	meta.Date = firstNonEmpty(readStringField(raw, "date"), readStringField(raw, "created_date"), defaults.Date)
	meta.ChangedDate = firstNonEmpty(readStringField(raw, "changed_date"), defaults.ChangedDate)
	meta.OriginalDocument = firstNonEmpty(readStringField(raw, "original_document"), defaults.OriginalDocument)
	meta.OriginalFormat = firstNonEmpty(readStringField(raw, "original_format"), defaults.OriginalFormat)
	meta.Version = firstNonEmpty(defaults.Version, normalizeVersion(readStringField(raw, "version")))
	meta.Language = firstNonEmpty(defaults.Language, normalizeLanguageCode(readStringField(raw, "language")), normalizeLanguageCode(readStringField(raw, "lang")))
	meta.Abstract = readStringField(raw, "abstract")
	meta.Keywords = readKeywordsField(raw["keywords"])

	return meta, true, nil
}

func getFileCreatedDate(path string) string {
	date, _ := filetime.Formatted(path, "created", time.RFC3339)
	return date
}

func getFileModifiedDate(path string) string {
	date, _ := filetime.Formatted(path, "modified", time.RFC3339)
	return date
}

func defaultMetaData(path string) *MetaData {
	meta := MetaData{
		Title:            markdownBaseStem(path),
		OriginalDocument: normalizeOriginalDocumentPath(path),
		OriginalFormat:   strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		Date:             getFileCreatedDate(path),
		ChangedDate:      getFileModifiedDate(path),
		Version:          parseVersionFromFile(path),
		Language:         parseLanguageFromFile(path),
	}
	if meta.OriginalFormat == "md" || meta.OriginalFormat == "markdown" {
		parts := parseMarkdownFileName(path)
		meta.Version = parts.Version
		meta.Language = parts.Language
	}
	return &meta
}

func extractLeadingFrontmatter(body string) (string, bool) {
	if !strings.HasPrefix(body, "---\n") {
		return "", false
	}
	end := strings.Index(body[4:], "\n---\n")
	if end < 0 {
		return "", false
	}
	return body[4 : end+4], true
}

func normalizeFrontmatterNewlines(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	return strings.ReplaceAll(input, "\r", "\n")
}

func readStringField(values map[string]any, key string) string {
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

func readKeywordsField(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return normalizeKeywords(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = append(values, fmt.Sprint(item))
		}
		return normalizeKeywords(values)
	case []string:
		return normalizeKeywords(typed)
	default:
		return normalizeKeywords(fmt.Sprint(typed))
	}
}

func normalizeKeywords(value any) []string {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func readOfficeMetaData(path string, meta *MetaData) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()

	props, err := readOfficeCorePropertiesFromFiles(reader.File)
	if err != nil {
		return err
	}
	applyOfficeCoreProperties(meta, props)
	return nil
}

func readOfficeCorePropertiesFromFiles(files []*zip.File) (officeCoreProperties, error) {
	for _, file := range files {
		if file.Name != "docProps/core.xml" {
			continue
		}
		return readOfficeCoreProperties(file)
	}
	return officeCoreProperties{}, nil
}

func readOfficeCoreProperties(file *zip.File) (officeCoreProperties, error) {
	var props officeCoreProperties
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

func applyOfficeCoreProperties(meta *MetaData, props officeCoreProperties) {
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
		meta.Version = normalizeVersion(props.Revision)
	}
	if meta.Language == "" {
		meta.Language = normalizeLanguageCode(props.Language)
	}

	description := strings.TrimSpace(props.Description)
	if description != "" {
		meta.Abstract = description
	} else {
		meta.Abstract = strings.TrimSpace(props.Subject)
	}
	meta.Keywords = normalizeKeywords(props.Keywords)
}
