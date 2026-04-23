package io

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
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

	zipfile := filepath.Join(path, zipFileName(d.MetaData.ParseFileNameFromMetaData()))

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
		if err := writeZipEntry(writer, d.MetaData.ParseFileNameFromMetaData(), d.MarkdownFile); err != nil {
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

func (d *Documents) ReadFromOriginalFile(path string) error {
	r, err := os.Open(path)
	if err != nil {
		return err
	}
	defer r.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r); err != nil {
		return err
	}
	d.OriginalFile = buf
	return nil
}

func (d *Documents) ReadFromStream(r io.Reader) error {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r); err != nil {
		return err
	}
	d.OriginalFile = buf
	return nil
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

func markdownEntryName(fileName string) string {
	base := strings.TrimSpace(filepath.Base(fileName))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		base = "document"
	}
	return base + ".md"
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
