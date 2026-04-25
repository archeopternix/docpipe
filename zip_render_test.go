package docpipe

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenZipListsReadsRendersAndIndexes(t *testing.T) {
	body := sampleMarkdown("Sample", "1.0", strings.Join([]string{
		"# Intro",
		"",
		"![Logo](media/logo.png)",
		"",
		"## Details",
		"",
		"### Child",
		"",
		"## Details",
		"",
	}, "\n"))
	zipBytes := makeTestZip(t, map[string]string{
		"Sample_en_v1.0.md": body,
		"media/logo.png":    "image-bytes",
		"slides/slide.png":  "slide-bytes",
	})

	doc, err := OpenZip(bytes.NewReader(zipBytes))
	if err != nil {
		t.Fatalf("OpenZip() error = %v", err)
	}

	images, err := doc.ListImages("media")
	if err != nil {
		t.Fatalf("ListImages() error = %v", err)
	}
	if got, want := strings.Join(images, ","), "media/logo.png"; got != want {
		t.Fatalf("ListImages() = %q, want %q", got, want)
	}

	image, err := doc.GetImageBytes("logo.png")
	if err != nil {
		t.Fatalf("GetImageBytes() error = %v", err)
	}
	if got, want := image.String(), "image-bytes"; got != want {
		t.Fatalf("GetImageBytes() = %q, want %q", got, want)
	}

	slides, err := doc.ListSlides("slides/")
	if err != nil {
		t.Fatalf("ListSlides() error = %v", err)
	}
	if got, want := strings.Join(slides, ","), "slides/slide.png"; got != want {
		t.Fatalf("ListSlides() = %q, want %q", got, want)
	}

	rendered, err := doc.RenderHTML(RenderOptions{
		AnchorifyHeadings: true,
		RewriteImageURLs: func(orig string) (string, bool) {
			return "/assets/" + orig, true
		},
		SplitSections: true,
	})
	if err != nil {
		t.Fatalf("RenderHTML() error = %v", err)
	}
	for _, want := range []string{
		`<h1 id="intro">Intro</h1>`,
		`<h2 id="details">Details</h2>`,
		`<h2 id="details-2">Details</h2>`,
		`<img src="/assets/media/logo.png" alt="Logo">`,
	} {
		if !strings.Contains(rendered.BodyHTML, want) {
			t.Fatalf("RenderHTML() body missing %q in:\n%s", want, rendered.BodyHTML)
		}
	}
	if !strings.Contains(rendered.TitleHTML, "Sample") || !strings.Contains(rendered.FrontmatterHTML, "Version") {
		t.Fatalf("RenderHTML() split sections not populated: %+v", rendered)
	}

	index, err := doc.HeadingIndex(3)
	if err != nil {
		t.Fatalf("HeadingIndex() error = %v", err)
	}
	if len(index) != 1 || index[0].AnchorID != "intro" || len(index[0].Children) != 2 {
		t.Fatalf("HeadingIndex() unexpected tree: %+v", index)
	}
	if index[0].Children[0].AnchorID != "details" || index[0].Children[1].AnchorID != "details-2" {
		t.Fatalf("HeadingIndex() duplicate anchors not deterministic: %+v", index[0].Children)
	}
	if len(index[0].Children[0].Children) != 1 || index[0].Children[0].Children[0].AnchorID != "child" {
		t.Fatalf("HeadingIndex() missing nested child: %+v", index)
	}
}

func TestSetFrontmatterArchivesInMemoryVersion(t *testing.T) {
	zipBytes := makeTestZip(t, map[string]string{
		"Sample_en_v1.0.md": sampleMarkdown("Sample", "1.0", "# Body\n"),
	})
	doc, err := OpenZip(bytes.NewReader(zipBytes))
	if err != nil {
		t.Fatalf("OpenZip() error = %v", err)
	}

	meta := doc.metaData
	meta.Title = "Updated"
	if err := doc.SetFrontmatter(meta); err != nil {
		t.Fatalf("SetFrontmatter() error = %v", err)
	}

	if got, want := doc.metaData.Version, "1.1"; got != want {
		t.Fatalf("version = %q, want %q", got, want)
	}
	if len(doc.markdownVersions) != 1 {
		t.Fatalf("markdownVersions len = %d, want 1", len(doc.markdownVersions))
	}
	current := doc.markdownFile.String()
	if !strings.Contains(current, `title: "Updated"`) || !strings.Contains(current, `version: "1.1"`) || !strings.Contains(current, "# Body") {
		t.Fatalf("updated markdown missing expected content:\n%s", current)
	}
}

func TestPatchZipReplacesRootAndAddsVersion(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "bundle.zip")
	writeTestZipFile(t, archivePath, map[string]string{
		"Sample_en_v1.0.md": sampleMarkdown("Sample", "1.0", "# Body\n"),
		"media/logo.png":    "image-bytes",
	})

	doc, err := ParseZipFile(archivePath)
	if err != nil {
		t.Fatalf("ParseZipFile() error = %v", err)
	}
	meta := doc.metaData
	meta.Title = "Updated"
	if err := doc.SetFrontmatter(meta); err != nil {
		t.Fatalf("SetFrontmatter() error = %v", err)
	}
	if err := doc.PatchZip(archivePath); err != nil {
		t.Fatalf("PatchZip() error = %v", err)
	}

	entries := readTestZip(t, archivePath)
	if _, ok := entries["Updated_en_v1.1.md"]; !ok {
		t.Fatalf("patched ZIP missing updated root; entries=%v", sortedStringKeys(entries))
	}
	if _, ok := entries["Sample_en_v1.0.md"]; ok {
		t.Fatalf("patched ZIP still contains old root")
	}
	if got, want := entries["media/logo.png"], "image-bytes"; got != want {
		t.Fatalf("media entry = %q, want %q", got, want)
	}
	versionCount := 0
	for name, content := range entries {
		if strings.HasPrefix(name, "versions/") {
			versionCount++
			if !strings.Contains(content, "# Body") {
				t.Fatalf("version archive %s missing old markdown body:\n%s", name, content)
			}
		}
	}
	if versionCount != 1 {
		t.Fatalf("version entries = %d, want 1; entries=%v", versionCount, sortedStringKeys(entries))
	}
}

func TestZipImportAndCreateZipUsePrefixedKeysAsIs(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "bundle.zip")
	writeTestZipFile(t, archivePath, map[string]string{
		"Sample_en_v1.0.md": sampleMarkdown("Sample", "1.0", "# Body\n"),
		"media/logo.png":    "image-bytes",
		"slides/slide.png":  "slide-bytes",
		"versions/old.md":   "old-version",
	})

	doc, err := ParseZipFile(archivePath)
	if err != nil {
		t.Fatalf("ParseZipFile() error = %v", err)
	}
	if doc.extractedImages["media/logo.png"] == nil {
		t.Fatalf("ParseZipFile() did not preserve media/ key: %#v", doc.extractedImages)
	}
	if doc.extractedSlides["slides/slide.png"] == nil {
		t.Fatalf("ParseZipFile() did not preserve slides/ key: %#v", doc.extractedSlides)
	}
	if doc.markdownVersions["versions/old.md"] == nil {
		t.Fatalf("ParseZipFile() did not preserve versions/ key: %#v", doc.markdownVersions)
	}

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	if err := doc.CreateZipBytes(zw); err != nil {
		t.Fatalf("CreateZipBytes() error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close() error = %v", err)
	}

	outPath := filepath.Join(dir, "out.zip")
	if err := os.WriteFile(outPath, out.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	entries := readTestZip(t, outPath)
	for _, name := range []string{"media/logo.png", "slides/slide.png", "versions/old.md"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("CreateZipBytes() missing %q; entries=%v", name, sortedStringKeys(entries))
		}
	}
	for _, name := range []string{"media/media/logo.png", "slides/slides/slide.png", "versions/versions/old.md"} {
		if _, ok := entries[name]; ok {
			t.Fatalf("CreateZipBytes() wrote double-prefixed entry %q", name)
		}
	}
}

func sampleMarkdown(title, version, body string) string {
	return strings.Join([]string{
		"---",
		`title: "` + title + `"`,
		`version: "` + version + `"`,
		`language: "en"`,
		"---",
		"",
		body,
	}, "\n")
}

func makeTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedStringKeys(entries) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
		if _, err := io.WriteString(w, entries[name]); err != nil {
			t.Fatalf("WriteString(%q) error = %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return buf.Bytes()
}

func writeTestZipFile(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	if err := os.WriteFile(path, makeTestZip(t, entries), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func readTestZip(t *testing.T, path string) map[string]string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer reader.Close()

	entries := make(map[string]string)
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("Open(%q) error = %v", file.Name, err)
		}
		body, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			t.Fatalf("ReadAll(%q) error = %v", file.Name, err)
		}
		if closeErr != nil {
			t.Fatalf("Close(%q) error = %v", file.Name, closeErr)
		}
		entries[file.Name] = string(body)
	}
	return entries
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		value := values[i]
		j := i - 1
		for j >= 0 && values[j] > value {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = value
	}
}
