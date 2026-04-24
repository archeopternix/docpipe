package io

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Documents struct {
	FileName         string                   // Name of the zip
	OriginalFile     *bytes.Buffer            // will be saved under /document
	MarkdownFile     *bytes.Buffer            // will be saved under /
	ExtractedImages  map[string]*bytes.Buffer // will be save in /media
	ExtractedSlides  map[string]*bytes.Buffer // will be save in /slides
	MarkdownVersions map[string]*bytes.Buffer // will be save in /versions under the id "<lang>_<version>" e.g. "DE_v1.2"
	MetaData         MetaData
}

func NewDocuments() *Documents {
	return &Documents{
		ExtractedImages:  make(map[string]*bytes.Buffer),
		ExtractedSlides:  make(map[string]*bytes.Buffer),
		MarkdownVersions: make(map[string]*bytes.Buffer),
	}
}

func (d *Documents) SaveAsZip(path string) error {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}

	zipfile := filepath.Join(path, zipFileName(d.GetMarkdownFileName()))

	// Create the zip file
	out, err := os.Create(zipfile)
	if err != nil {
		fmt.Println("File not created", err)
		os.Exit(1)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	defer func() {
		if err := zw.Close(); err != nil {
			fmt.Println("File not created", err)
			os.Exit(1)
		}
	}()

	if err := d.CreateZipBytes(zw); err != nil {
		fmt.Println("Zip not written", err)
		os.Exit(1)
	}
	return nil
}

func (d *Documents) CreateZipBytes(writer *zip.Writer) error {
	if writer == nil {
		return fmt.Errorf("documents is nil")
	}

	fmt.Println(d.MetaData)

	// Save Markdown file at the root of the ZIP with a name derived from the original document
	if d.MarkdownFile != nil && d.MarkdownFile.Len() > 0 {
		if err := writeZipEntry(writer, d.GetMarkdownFileName(), d.MarkdownFile); err != nil {
			_ = writer.Close()
			return nil
		}
	}

	if d.OriginalFile != nil && d.OriginalFile.Len() > 0 {
		if err := writeZipEntry(writer, documentEntryName(d.MetaData.OriginalDocument), d.OriginalFile); err != nil {
			_ = writer.Close()
			return err
		}
	}

	for _, name := range sortedMapKeys(d.ExtractedImages) {
		if err := writeZipEntry(writer, prefixedEntryName("media", name), d.ExtractedImages[name]); err != nil {
			_ = writer.Close()
			return err
		}
	}

	for _, name := range sortedMapKeys(d.ExtractedSlides) {
		if err := writeZipEntry(writer, prefixedEntryName("slides", name), d.ExtractedSlides[name]); err != nil {
			_ = writer.Close()
			return err
		}
	}

	for _, name := range sortedMapKeys(d.MarkdownVersions) {
		entryName := name
		if filepath.Ext(entryName) == "" {
			entryName += ".md"
		}
		if err := writeZipEntry(writer, prefixedEntryName("versions", entryName), d.MarkdownVersions[name]); err != nil {
			_ = writer.Close()
			return err
		}
	}

	return nil
}

func (d *Documents) applyMetaDataFrontmatter() {
	if d == nil {
		return
	}

	// strip existing frontmatter
	body := ""
	if d.MarkdownFile != nil {
		body = d.MarkdownFile.String()
	}
	body = strings.TrimLeft(stripLeadingFrontmatter(normalizeFrontmatterNewlines(body)), "\n")

	var builder strings.Builder
	builder.WriteString("---\n")
	writeFrontmatterString(&builder, "title", d.MetaData.Title)
	writeFrontmatterString(&builder, "subtitle", d.MetaData.Subtitle)
	writeFrontmatterString(&builder, "date", formatFrontmatterDate(d.MetaData.Date))
	writeFrontmatterString(&builder, "changed_date", formatFrontmatterDate(d.MetaData.ChangedDate))
	writeFrontmatterString(&builder, "original_document", normalizeOriginalDocumentPath(d.MetaData.OriginalDocument))
	writeFrontmatterString(&builder, "original_format", d.MetaData.OriginalFormat)
	writeFrontmatterString(&builder, "version", d.MetaData.Version)
	writeFrontmatterString(&builder, "language", d.MetaData.Language)
	writeFrontmatterString(&builder, "abstract", d.MetaData.Abstract)
	writeFrontmatterKeywords(&builder, d.MetaData.Keywords)
	writeFrontmatterString(&builder, "author", d.MetaData.Author)
	builder.WriteString("---\n\n")
	builder.WriteString(strings.TrimRight(body, "\n"))
	builder.WriteString("\n")

	d.MarkdownFile = bytes.NewBufferString(builder.String())
}

// GetMarkdownFileName generates a Markdown file name based on the metadata information.
func (d Documents) GetMarkdownFileName() string {
	baseStem := strings.TrimSpace(strings.TrimSpace(d.MetaData.Title))
	if baseStem == "" {
		baseStem = "Document"
	}

	language := normalizeVersionLanguage(d.MetaData.Language)
	if language == "" {
		language = "EN"
	}

	version := normalizeMarkdownVersion(d.MetaData.Version)
	if version == "" {
		version = "1.0"
	}

	return fmt.Sprintf("%s_%s_v%s.md", baseStem, language, version)
}

// ---------------------------------------------------------------------------

func (d *Documents) populateMetaData(path string) error {
	switch normalizeExtension(filepath.Ext(path)) {
	case ".docx", ".pptx":
		if err := readOfficeMetaData(path, &d.MetaData); err != nil {
			return err
		}
		return nil
	case ".md", ".markdown":
		if err := readMarkdownMetaData(path, &d.MetaData); err != nil {
			return err
		}
		return nil
	case ".txt":
		return nil
	default:
		return fmt.Errorf("metadata extraction not supported for %q", filepath.Ext(path))
	}
}

func writeZipEntry(writer *zip.Writer, name string, buf *bytes.Buffer) error {
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

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func zipFileName(name string) string {
	base := strings.TrimSpace(filepath.Base(name))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		base = "document"
	}
	return base + ".zip"
}

func documentEntryName(originalDocument string) string {
	base := strings.TrimSpace(filepath.Base(originalDocument))
	if base == "" || base == "." {
		base = "document.bin"
	}
	return filepath.ToSlash(filepath.Join("document", base))
}

func prefixedEntryName(prefix, name string) string {
	clean := strings.TrimSpace(filepath.ToSlash(name))
	clean = strings.TrimPrefix(clean, "/")
	clean = strings.TrimPrefix(clean, prefix+"/")
	if clean == "" {
		clean = "file"
	}
	return filepath.ToSlash(filepath.Join(prefix, clean))
}
