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

	"gopkg.in/yaml.v3"
)

var (
	markdownFilenamePattern = regexp.MustCompile(`(?i)^(.*)_([a-z]{2})_v(\d+(?:\.\d+)*)$`)
	versionPattern          = regexp.MustCompile(`^\d+(?:\.\d+)*$`)
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

type MarkdownFileNameParts struct {
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

func ReadMetaData(path string) (MetaData, error) {
	meta := defaultMetaData(path)

	switch NormalizeExtension(filepath.Ext(path)) {
	case ".docx", ".pptx":
		return readOfficeMetaData(path)
	case ".md", ".markdown":
		return readMarkdownMetaData(path)
	case ".txt":
		return meta, nil
	default:
		return meta, fmt.Errorf("metadata extraction not supported for %q", filepath.Ext(path))
	}
}

func ApplyMetaDataFrontmatter(body string, meta MetaData) string {
	body = strings.TrimLeft(stripLeadingFrontmatter(normalizeFrontmatterNewlines(body)), "\n")

	var builder strings.Builder
	builder.WriteString("---\n")
	writeFrontmatterString(&builder, "title", meta.Title)
	writeFrontmatterString(&builder, "subtitle", meta.Subtitle)
	writeFrontmatterString(&builder, "date", formatDate(meta.Date))
	writeFrontmatterString(&builder, "changed_date", formatDate(meta.ChangedDate))
	writeFrontmatterString(&builder, "original_document", meta.OriginalDocument)
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

func formatDate(input string) string {
	layouts := []string{
		time.RFC3339,          // 2006-01-02T15:04:05Z07:00
		time.RFC3339Nano,      // 2006-01-02T15:04:05.999999999Z07:00
		time.RFC1123,          // Mon, 02 Jan 2006 15:04:05 MST
		time.RFC1123Z,         // Mon, 02 Jan 2006 15:04:05 -0700
		time.RFC850,           // Monday, 02-Jan-06 15:04:05 MST
		time.RFC822,           // 02 Jan 06 15:04 MST
		time.RFC822Z,          // 02 Jan 06 15:04 -0700
		"2006-01-02 15:04:05", // datetime with seconds
		"2006-01-02 15:04",    // datetime without seconds
		"2006-01-02",          // date only
		"02.01.2006 15:04:05", // EU datetime with seconds
		"02.01.2006 15:04",    // EU datetime
		"02.01.2006",          // EU date
		"01/02/2006 15:04:05", // US datetime with seconds
		"01/02/2006 15:04",    // US datetime
		"01/02/2006",          // US date
		"Jan 2, 2006 3:04 PM", // verbose with AM/PM
		"Jan 2, 2006",         // verbose date
		"January 2, 2006",     // full month name
		"2 Jan 2006 15:04:05", // day-first verbose
		"2 Jan 2006",          // day-first verbose date
		time.UnixDate,         // Mon Jan _2 15:04:05 MST 2006
		time.ANSIC,            // Mon Jan _2 15:04:05 2006
	}

	input = strings.TrimSpace(input)

	for _, layout := range layouts {
		t, err := time.Parse(layout, input)
		if err == nil {
			return t.Format("02.01.2006 15:04")
		}
	}

	return ""
}

func NormalizeLanguageCode(value string) string {
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

func FilenameLanguageCode(value string) string {
	if normalized := NormalizeLanguageCode(value); normalized != "" {
		return strings.ToUpper(normalized)
	}
	return ""
}

func NormalizeVersion(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(strings.TrimPrefix(normalized, "v"), "V")
	if !versionPattern.MatchString(normalized) {
		return ""
	}
	return normalized
}

func NewFileNameVersion(entryName, body string, meta *MetaData, detectLanguage func(string) (string, error)) (string, error) {
	parts := ParseMarkdownFileName(entryName)
	baseStem := strings.TrimSpace(parts.BaseStem)
	if baseStem == "" {
		baseStem = MarkdownBaseStem(entryName)
	}
	if baseStem == "" {
		baseStem = "document"
	}

	version := NormalizeVersion(parts.Version)
	if version == "" && meta != nil {
		version = NormalizeVersion(meta.Version)
	}
	if version == "" {
		version = "1.0"
	}

	language := NormalizeLanguageCode(parts.Language)
	if language == "" && meta != nil {
		language = NormalizeLanguageCode(meta.Language)
	}
	if language == "" {
		if detectLanguage == nil {
			return "", fmt.Errorf("language detection unavailable for %s", entryName)
		}
		detected, err := detectLanguage(body)
		if err != nil {
			return "", err
		}
		language = NormalizeLanguageCode(detected)
		if language == "" {
			return "", fmt.Errorf("language detection returned no usable language for %s", entryName)
		}
	}

	if meta != nil {
		meta.Version = version
		meta.Language = language
		if strings.TrimSpace(meta.Title) == "" {
			meta.Title = baseStem
		}
	}

	return fmt.Sprintf("%s_%s_v%s.md", baseStem, FilenameLanguageCode(language), version), nil
}

func readOfficeMetaData(path string) (MetaData, error) {
	meta := defaultMetaData(path)

	reader, err := zip.OpenReader(path)
	if err != nil {
		return meta, err
	}
	defer reader.Close()

	props, err := readOfficeCorePropertiesFromFiles(reader.File)
	if err != nil {
		return meta, err
	}
	applyOfficeCoreProperties(&meta, props)
	return meta, nil
}

func readMarkdownMetaData(path string) (MetaData, error) {
	meta := defaultMetaData(path)

	body, err := os.ReadFile(path)
	if err != nil {
		return meta, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return meta, err
	}

	if parsed, ok, err := parseMarkdownMetaData(string(body), meta); err != nil {
		return meta, err
	} else if ok {
		meta = parsed
	}

	if meta.Title == "" {
		meta.Title = MarkdownBaseStem(path)
	}
	if meta.Date == "" {
		meta.Date = info.ModTime().Format("2006-01-02")
	}
	if meta.ChangedDate == "" {
		meta.ChangedDate = info.ModTime().Format("2006-01-02")
	}

	return meta, nil
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
	meta.Version = firstNonEmpty(defaults.Version, NormalizeVersion(readStringField(raw, "version")))
	meta.Language = firstNonEmpty(defaults.Language, NormalizeLanguageCode(readStringField(raw, "language")), NormalizeLanguageCode(readStringField(raw, "lang")))
	meta.Abstract = readStringField(raw, "abstract")
	meta.Keywords = readKeywordsField(raw["keywords"])

	return meta, true, nil
}

func getFileCreatedDate(path string) string {
	date, _ := FileTimeFormatted(path, "created", time.RFC3339)
	return date
}

func getFileModifiedDate(path string) string {
	date, _ := FileTimeFormatted(path, "modified", time.RFC3339)
	return date
}

func defaultMetaData(path string) MetaData {
	meta := MetaData{
		Title:            MarkdownBaseStem(path),
		OriginalDocument: filepath.Base(path),
		OriginalFormat:   strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		Date:             getFileCreatedDate(path),
		ChangedDate:      getFileModifiedDate(path),
	}
	if meta.OriginalFormat == "md" || meta.OriginalFormat == "markdown" {
		parts := ParseMarkdownFileName(path)
		meta.Version = parts.Version
		meta.Language = parts.Language
	}
	return meta
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

func normalizeFrontmatterNewlines(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	return strings.ReplaceAll(input, "\r", "\n")
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
	meta.Author = strings.TrimSpace(props.Creator)
	meta.Title = strings.TrimSpace(props.Title)
	meta.Language = NormalizeLanguageCode(props.Language)
	meta.Version = NormalizeVersion(props.Revision)

	description := strings.TrimSpace(props.Description)
	if description != "" {
		meta.Abstract = description
	} else {
		meta.Abstract = strings.TrimSpace(props.Subject)
	}
	meta.Keywords = normalizeKeywords(props.Keywords)
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
