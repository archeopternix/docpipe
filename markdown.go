package docpipe

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"fmt"
	stdio "io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	markdownFilenamePattern = regexp.MustCompile(`(?i)^(.*)_([a-z]{2})_v(\d+(?:\.\d+)*)$`)
	versionPattern          = regexp.MustCompile(`^\d+(?:\.\d+)*$`)
	datetimelayout          = "2006-01-02 15:04:05"
)

type Markdown struct {
	fileName         string                   // Name of the zip
	originalFile     *bytes.Buffer            // will be saved under /document
	markdownFile     *bytes.Buffer            // will be saved under /
	extractedImages  map[string]*bytes.Buffer // will be save in /media
	extractedSlides  map[string]*bytes.Buffer // will be save in /slides
	markdownVersions map[string]*bytes.Buffer // will be save in /versions under the id "<lang>_<version>" e.g. "DE_v1.2"
	metaData         MetaData
}

type Documents = Markdown

// MetaData holds the metadata information of the markdown document.
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

func NewDocuments() *Documents {
	return &Documents{
		extractedImages:  make(map[string]*bytes.Buffer),
		extractedSlides:  make(map[string]*bytes.Buffer),
		markdownVersions: make(map[string]*bytes.Buffer),
	}
}

type WordParams struct {
	IncludeImages bool
}

type PowerPointParams struct {
	IncludeSlides bool
	IncludeImages bool
}

func CreateFromZip(path string) (*Markdown, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	if strings.ToLower(filepath.Ext(path)) != ".zip" {
		return nil, fmt.Errorf("zip import not supported for %q", filepath.Ext(path))
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	doc := NewDocuments()
	doc.fileName = filepath.Base(path)
	doc.metaData = *mdDefaultMetaData(path)

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		name := markdownZipCleanEntryName(file.Name)
		if name == "" {
			continue
		}

		body, err := markdownZipReadFile(file)
		if err != nil {
			return nil, err
		}

		switch {
		case strings.HasPrefix(name, "document/"):
			if doc.originalFile == nil {
				doc.originalFile = bytes.NewBuffer(body)
				doc.metaData.OriginalDocument = name
				doc.metaData.OriginalFormat = strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
			}
		case strings.HasPrefix(name, "media/"):
			doc.extractedImages[strings.TrimPrefix(name, "media/")] = bytes.NewBuffer(body)
		case strings.HasPrefix(name, "slides/"):
			doc.extractedSlides[strings.TrimPrefix(name, "slides/")] = bytes.NewBuffer(body)
		case strings.HasPrefix(name, "versions/"):
			doc.markdownVersions[strings.TrimPrefix(name, "versions/")] = bytes.NewBuffer(body)
		case markdownZipIsRootMarkdown(name):
			doc.markdownFile = bytes.NewBuffer(body)
			markdownZipApplyMarkdownMetaData(doc, name, string(body))
		}
	}

	if doc.markdownFile == nil {
		return nil, fmt.Errorf("zip does not contain a root markdown file")
	}
	if doc.metaData.Title == "" {
		doc.metaData.Title = mdBaseStem(doc.fileName)
	}
	if doc.metaData.Version == "" {
		doc.metaData.Version = "1.0"
	}

	return doc, nil

}

func (d *Markdown) SaveAsZip(path string) error {
	if d == nil {
		return fmt.Errorf("markdown is nil")
	}
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}

	zipName, err := markdownZipFileName(d.GetMarkdownFileName())
	if err != nil {
		return err
	}
	zipfile := filepath.Join(path, zipName)
	out, err := os.Create(zipfile)
	if err != nil {
		return err
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	if err := d.CreateZipBytes(zw); err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}

func (d *Markdown) CreateZipBytes(writer *zip.Writer) error {
	if d == nil {
		return fmt.Errorf("markdown is nil")
	}
	if writer == nil {
		return fmt.Errorf("zip writer is nil")
	}

	if d.markdownFile != nil && d.markdownFile.Len() > 0 {
		if err := markdownWriteZipEntry(writer, d.GetMarkdownFileName(), d.markdownFile); err != nil {
			return err
		}
	}

	if d.originalFile != nil && d.originalFile.Len() > 0 {
		if err := markdownWriteZipEntry(writer, markdownDocumentEntryName(d.metaData.OriginalDocument), d.originalFile); err != nil {
			return err
		}
	}

	for _, name := range markdownSortedMapKeys(d.extractedImages) {
		if err := markdownWriteZipEntry(writer, markdownPrefixedEntryName("media", name), d.extractedImages[name]); err != nil {
			return err
		}
	}

	for _, name := range markdownSortedMapKeys(d.extractedSlides) {
		if err := markdownWriteZipEntry(writer, markdownPrefixedEntryName("slides", name), d.extractedSlides[name]); err != nil {
			return err
		}
	}

	for _, name := range markdownSortedMapKeys(d.markdownVersions) {
		entryName := name
		if filepath.Ext(entryName) == "" {
			entryName += ".md"
		}
		if err := markdownWriteZipEntry(writer, markdownPrefixedEntryName("versions", entryName), d.markdownVersions[name]); err != nil {
			return err
		}
	}

	return nil
}

func (d Markdown) GetMarkdownFileName() string {
	return mdFileName(d.metaData)
}

func markdownZipReadFile(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return stdio.ReadAll(rc)
}

func markdownZipCleanEntryName(name string) string {
	clean := filepath.ToSlash(strings.TrimSpace(name))
	clean = strings.TrimPrefix(clean, "/")
	clean = filepath.Clean(clean)
	clean = filepath.ToSlash(clean)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return ""
	}
	return clean
}

func markdownZipIsRootMarkdown(name string) bool {
	if strings.Contains(name, "/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func markdownZipApplyMarkdownMetaData(doc *Markdown, name string, body string) {
	defaults := doc.metaData
	defaults.Title = mdBaseStem(name)
	parts := mdParseFileName(name)
	if parts.Version != "" {
		defaults.Version = parts.Version
	}
	if parts.Language != "" {
		defaults.Language = parts.Language
	}
	if defaults.OriginalDocument == "" {
		defaults.OriginalDocument = mdNormalizeOriginalDocumentPath(name)
	}

	if parsed, ok, err := mdParseMetaData(body, defaults); err == nil && ok {
		doc.metaData = parsed
		return
	}
	doc.metaData = defaults
}

func markdownWriteZipEntry(writer *zip.Writer, name string, buf *bytes.Buffer) error {
	if writer == nil || buf == nil || buf.Len() == 0 {
		return nil
	}

	header := &zip.FileHeader{
		Name:   filepath.ToSlash(name),
		Method: zip.Deflate,
	}
	entryWriter, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = bytes.NewReader(buf.Bytes()).WriteTo(entryWriter)
	return err
}

func markdownSortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func markdownZipFileName(name string) (string, error) {
	base := strings.TrimSpace(mdBaseStem(name))
	if base == "" {
		base = "document"
	}

	id, err := markdownUUID()
	if err != nil {
		return "", err
	}
	return id + "_" + base + ".zip", nil
}

func markdownUUID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}

	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

func markdownDocumentEntryName(originalDocument string) string {
	base := strings.TrimSpace(filepath.Base(originalDocument))
	if base == "" || base == "." {
		base = "document.bin"
	}
	return filepath.ToSlash(filepath.Join("document", base))
}

func markdownPrefixedEntryName(prefix, name string) string {
	clean := strings.TrimSpace(filepath.ToSlash(name))
	clean = strings.TrimPrefix(clean, "/")
	clean = strings.TrimPrefix(clean, prefix+"/")
	if clean == "" {
		clean = "file"
	}
	return filepath.ToSlash(filepath.Join(prefix, clean))
}
