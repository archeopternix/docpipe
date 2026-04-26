package docpipe

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dpstore "docpipe/store"
)

func TestServiceImportMarkdownUpdateAndExportZip(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	service := NewService(dpstore.FS{BasePath: baseDir})

	doc, err := service.ImportDocument(ctx, ImportSource{
		Reader: strings.NewReader(sampleMarkdown("Sample", "1.0", "# Body\n")),
		Name:   "sample.md",
		Size:   int64(len(sampleMarkdown("Sample", "1.0", "# Body\n"))),
	})
	if err != nil {
		t.Fatalf("ImportDocument() error = %v", err)
	}

	root, err := service.ReadMarkdown(ctx, doc)
	if err != nil {
		t.Fatalf("ReadMarkdown() error = %v", err)
	}
	if !strings.Contains(root, `title: "Sample"`) {
		t.Fatalf("ReadMarkdown() missing frontmatter:\n%s", root)
	}

	fm, err := service.RenderFrontmatter(ctx, doc)
	if err != nil {
		t.Fatalf("RenderFrontmatter() error = %v", err)
	}
	fm.Title = "Updated"
	if err := service.UpdateFrontmatter(ctx, doc, fm, UpdateOptions{
		ArchivePrevious: true,
		BumpVersion:     true,
		Now:             func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) },
	}); err != nil {
		t.Fatalf("UpdateFrontmatter() error = %v", err)
	}

	root, err = service.ReadMarkdown(ctx, doc)
	if err != nil {
		t.Fatalf("ReadMarkdown(updated) error = %v", err)
	}
	if !strings.Contains(root, `title: "Updated"`) || !strings.Contains(root, `version: "1.1"`) {
		t.Fatalf("updated root missing expected values:\n%s", root)
	}

	versions, err := service.listAllFiles(ctx, doc, service.paths().VersionsDir)
	if err != nil {
		t.Fatalf("listAllFiles(versions) error = %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions len = %d, want 1", len(versions))
	}

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	if err := service.ExportZip(ctx, doc, zw); err != nil {
		t.Fatalf("ExportZip() error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close() error = %v", err)
	}

	zipPath := filepath.Join(baseDir, "export.zip")
	if err := os.WriteFile(zipPath, zipBuf.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile(export) error = %v", err)
	}
	entries := readTestZip(t, zipPath)
	if _, ok := entries["root.md"]; !ok {
		t.Fatalf("ExportZip() missing root.md: %v", sortedStringKeys(entries))
	}
}

func TestServiceImportZipAndRender(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	service := NewService(dpstore.FS{BasePath: baseDir})

	zipBytes := makeTestZip(t, map[string]string{
		"Sample_en_v1.0.md": sampleMarkdown("Sample", "1.0", "# Intro\n\n![Logo](media/logo.png)\n"),
		"media/logo.png":    "image-bytes",
		"slides/slide.png":  "slide-bytes",
	})

	doc, err := service.ImportZip(ctx, bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("ImportZip() error = %v", err)
	}

	media, err := service.ListMedia(ctx, doc)
	if err != nil {
		t.Fatalf("ListMedia() error = %v", err)
	}
	if got, want := strings.Join(media, ","), "media/logo.png"; got != want {
		t.Fatalf("ListMedia() = %q, want %q", got, want)
	}

	rendered, err := service.RenderHTML(ctx, doc, RenderOptions{AnchorifyHeadings: true, SplitSections: true})
	if err != nil {
		t.Fatalf("RenderHTML() error = %v", err)
	}
	if !strings.Contains(rendered.BodyHTML, `<h1 id="intro">Intro</h1>`) {
		t.Fatalf("RenderHTML() missing heading:\n%s", rendered.BodyHTML)
	}
	if !strings.Contains(rendered.TitleHTML, "Sample") {
		t.Fatalf("RenderHTML() missing title HTML: %+v", rendered)
	}

	file, err := service.OpenSlide(ctx, doc, "slide.png")
	if err != nil {
		t.Fatalf("OpenSlide() error = %v", err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll(slide) error = %v", err)
	}
	if got, want := string(body), "slide-bytes"; got != want {
		t.Fatalf("slide body = %q, want %q", got, want)
	}
}

func TestServiceOpenAssetRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	service := NewService(dpstore.FS{BasePath: t.TempDir()})

	doc, err := service.ImportDocument(ctx, ImportSource{
		Reader: strings.NewReader(sampleMarkdown("Sample", "1.0", "# Body\n")),
		Name:   "sample.md",
	})
	if err != nil {
		t.Fatalf("ImportDocument() error = %v", err)
	}

	for _, name := range []string{"../root.md", "media/../root.md", "/root.md"} {
		if file, err := service.OpenMedia(ctx, doc, name); err == nil {
			_ = file.Close()
			t.Fatalf("OpenMedia(%q) succeeded, want error", name)
		}
	}
}
