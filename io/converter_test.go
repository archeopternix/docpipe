package io_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	docio "docpipe/io"
	"docpipe/processor"
)

func TestConvertFileTextMetadataAndBufferIsolation(t *testing.T) {
	path := filepath.Join("..", "TestData", "sample.txt")

	var metaA docio.MetaData
	bufA, err := docio.ConvertFile(path, &metaA)
	if err != nil {
		t.Fatalf("ConvertFile() error = %v", err)
	}

	var metaB docio.MetaData
	bufB, err := docio.ConvertFile(path, &metaB)
	if err != nil {
		t.Fatalf("second ConvertFile() error = %v", err)
	}

	wantCreated, _ := docio.FileTimeFormatted(path, "created", "2006-01-02T15:04:05Z07:00")
	wantModified, _ := docio.FileTimeFormatted(path, "modified", "2006-01-02T15:04:05Z07:00")

	if metaA.OriginalDocument != path {
		t.Fatalf("OriginalDocument = %q, want %q", metaA.OriginalDocument, path)
	}
	if metaA.OriginalFormat != "txt" {
		t.Fatalf("OriginalFormat = %q, want txt", metaA.OriginalFormat)
	}
	if metaA.Date != wantCreated {
		t.Fatalf("Date = %q, want %q", metaA.Date, wantCreated)
	}
	if metaA.ChangedDate != wantModified {
		t.Fatalf("ChangedDate = %q, want %q", metaA.ChangedDate, wantModified)
	}

	originalB := bufB.String()
	bufA.WriteString("\nmutated")
	if bufB.String() != originalB {
		t.Fatalf("expected independent buffers, second buffer changed to %q", bufB.String())
	}
}

func TestConvertFileMarkdownRouting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	content := "# heading\nbody\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var meta docio.MetaData
	buf, err := docio.ConvertFile(path, &meta)
	if err != nil {
		t.Fatalf("ConvertFile() error = %v", err)
	}
	if buf.String() != content {
		t.Fatalf("markdown content = %q, want %q", buf.String(), content)
	}
	if meta.OriginalFormat != "md" {
		t.Fatalf("OriginalFormat = %q, want md", meta.OriginalFormat)
	}
}

func TestConvertFileDocxMetadata(t *testing.T) {
	path := filepath.Join("..", "TestData", "Strategy.docx")

	var meta docio.MetaData
	buf, err := docio.ConvertFile(path, &meta)
	if err != nil {
		t.Fatalf("ConvertFile() error = %v", err)
	}
	if strings.TrimSpace(buf.String()) == "" {
		t.Fatal("expected extracted DOCX text to be non-empty")
	}
	if meta.Author != "Andreas Eisner" {
		t.Fatalf("Author = %q, want %q", meta.Author, "Andreas Eisner")
	}
	if meta.Title != "Strategy Sales and Marketing" {
		t.Fatalf("Title = %q, want %q", meta.Title, "Strategy Sales and Marketing")
	}
	if meta.Version != "8" {
		t.Fatalf("Version = %q, want %q", meta.Version, "8")
	}
	if meta.Date == "" || meta.ChangedDate == "" {
		t.Fatal("expected file timestamps to be populated")
	}
}

func TestConvertFilePptxMetadata(t *testing.T) {
	path := filepath.Join("..", "TestData", "Real.pptx")

	var meta docio.MetaData
	buf, err := docio.ConvertFile(path, &meta)
	if err != nil {
		t.Fatalf("ConvertFile() error = %v", err)
	}
	if strings.TrimSpace(buf.String()) == "" {
		t.Fatal("expected extracted PPTX text to be non-empty")
	}
	if meta.Author != "Eisner, Andreas" {
		t.Fatalf("Author = %q, want %q", meta.Author, "Eisner, Andreas")
	}
	if meta.Version != "1" {
		t.Fatalf("Version = %q, want %q", meta.Version, "1")
	}
}

func TestConvertFileUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.bin")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var meta docio.MetaData
	_, err := docio.ConvertFile(path, &meta)
	if err != processor.ErrUnsupportedFormat {
		t.Fatalf("ConvertFile() error = %v, want %v", err, processor.ErrUnsupportedFormat)
	}
}

func TestConvertFileNilMetaData(t *testing.T) {
	path := filepath.Join("..", "TestData", "sample.txt")

	_, err := docio.ConvertFile(path, nil)
	if err != processor.ErrMetaDataNil {
		t.Fatalf("ConvertFile() error = %v, want %v", err, processor.ErrMetaDataNil)
	}
}

func TestConvertFileMissingFile(t *testing.T) {
	var meta docio.MetaData
	_, err := docio.ConvertFile(filepath.Join(t.TempDir(), "missing.txt"), &meta)
	if err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestTextConvertersRunConcurrently(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.md")
	if err := os.WriteFile(pathA, []byte("alpha"), 0o600); err != nil {
		t.Fatalf("WriteFile(pathA) error = %v", err)
	}
	if err := os.WriteFile(pathB, []byte("beta"), 0o600); err != nil {
		t.Fatalf("WriteFile(pathB) error = %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	errCh := make(chan error, 2)
	go func() {
		defer wg.Done()
		var meta docio.MetaData
		_, err := docio.ConvertFile(pathA, &meta)
		errCh <- err
	}()
	go func() {
		defer wg.Done()
		var meta docio.MetaData
		_, err := docio.ConvertFile(pathB, &meta)
		errCh <- err
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("ConvertFile() concurrent error = %v", err)
		}
	}
}
