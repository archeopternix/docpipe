package docpipe

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/archeopternix/docpipe/search"
	dpstore "github.com/archeopternix/docpipe/store"
)

func TestServiceImportMarkdownUpdateAndExportZip(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	search, err := search.NewBleveSearch(baseDir)
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: baseDir}, search)

	doc, err := service.ImportDocument(ctx, ImportSource{
		Reader: strings.NewReader(sampleMarkdown("Sample", "1.0", "# Body\n")),
		Name:   "sample.md",
		Size:   int64(len(sampleMarkdown("Sample", "1.0", "# Body\n"))),
	})
	if err != nil {
		t.Fatalf("ImportDocument() error = %v", err)
	}

	parts, err := service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("ReadMarkdown() error = %v", err)
	}
	root := parts.Full

	if !strings.Contains(root, `title: "Sample"`) {
		t.Fatalf("ReadMarkdown() missing frontmatter:\n%s", root)
	}

	parts, err = service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("RenderFrontmatter() error = %v", err)
	}

	fm := parts.Frontmatter

	fm.Title = "Updated"
	if err := service.WriteFrontmatter(ctx, doc, fm, UpdateOptions{
		ArchivePrevious: true,
		BumpVersion:     true,
		Now:             func() time.Time { return time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC) },
	}); err != nil {
		t.Fatalf("UpdateFrontmatter() error = %v", err)
	}

	parts, err = service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("ReadMarkdown(updated) error = %v", err)
	}
	root = parts.Full

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
	search, err := search.NewBleveSearch(baseDir)
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: baseDir}, search)

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
	search, err := search.NewBleveSearch(t.TempDir())
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: t.TempDir()}, search)

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

func TestServiceDefaultsValidationAndMIMEImport(t *testing.T) {
	ctx := context.Background()
	search, err := search.NewBleveSearch(t.TempDir())
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: t.TempDir()}, search)

	if !service.Import.IncludeImages || !service.Import.IncludeSlides {
		t.Fatalf("NewService() import flags = %+v, want images and slides enabled", service.Import)
	}
	if service.Import.MaxBytes != defaultMaxZipEntryReadBytes {
		t.Fatalf("NewService() MaxBytes = %d, want %d", service.Import.MaxBytes, defaultMaxZipEntryReadBytes)
	}
	if got := service.Doc("  document-id  "); got.ID != "document-id" {
		t.Fatalf("Doc() ID = %q, want trimmed ID", got.ID)
	}

	defaults := DefaultPaths()
	if defaults.RootMarkdown != "root.md" || defaults.MediaDir != "media" || defaults.SlidesDir != "slides" || defaults.VersionsDir != "versions" {
		t.Fatalf("DefaultPaths() = %+v", defaults)
	}

	custom := Service{Paths: Paths{RootMarkdown: "main.md"}}
	paths := custom.paths()
	if paths.RootMarkdown != "main.md" || paths.MediaDir != defaults.MediaDir || paths.SlidesDir != defaults.SlidesDir || paths.VersionsDir != defaults.VersionsDir {
		t.Fatalf("paths() = %+v, want custom root with defaults for other dirs", paths)
	}
	if cfg := (Service{}).importConfig(); cfg.MaxBytes != defaultMaxZipEntryReadBytes {
		t.Fatalf("importConfig() MaxBytes = %d, want default", cfg.MaxBytes)
	}
	if err := service.ensureService(Document{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ensureService(empty doc) error = %v, want ErrInvalidInput", err)
	}
	if err := (Service{}).ensureStoreOnly(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ensureStoreOnly(nil store) error = %v, want ErrInvalidInput", err)
	}
	if got := service.importExtensionFromMime("text/markdown; charset=utf-8"); got != ".md" {
		t.Fatalf("importExtensionFromMime(text/markdown) = %q, want .md", got)
	}

	doc, err := service.ImportDocument(ctx, ImportSource{
		Reader:   strings.NewReader("# Body\n"),
		Name:     "upload",
		MimeType: "text/markdown",
	})
	if err != nil {
		t.Fatalf("ImportDocument(text/markdown MIME) error = %v", err)
	}

	parts, err := service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("ReadMarkdown(MIME import) error = %v", err)
	}
	root := parts.Full

	if !strings.Contains(root, `title: "upload"`) || !strings.Contains(root, "# Body") {
		t.Fatalf("MIME import root missing defaults/body:\n%s", root)
	}

	if _, err := service.ImportDocument(ctx, ImportSource{Reader: strings.NewReader("x"), Name: "file.bin"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ImportDocument(unsupported) error = %v, want ErrUnsupported", err)
	}
}

func TestServiceStageImportSource(t *testing.T) {
	ctx := context.Background()
	search, err := search.NewBleveSearch(t.TempDir())
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: t.TempDir()}, search)
	service.Import.TempDir = t.TempDir()
	service.Import.MaxBytes = 3

	file, size, cleanup, err := service.stageImportSource(ctx, ImportSource{
		Reader: strings.NewReader("abc"),
		Name:   "note.md",
	})
	if err != nil {
		t.Fatalf("stageImportSource() error = %v", err)
	}
	tempName := file.Name()
	defer cleanup()
	if size != 3 {
		t.Fatalf("stageImportSource() size = %d, want 3", size)
	}
	if filepath.Ext(tempName) != ".md" {
		t.Fatalf("stageImportSource() temp name = %q, want .md extension", tempName)
	}
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll(staged file) error = %v", err)
	}
	if string(body) != "abc" {
		t.Fatalf("staged body = %q, want abc", string(body))
	}
	cleanup()
	if _, err := os.Stat(tempName); !os.IsNotExist(err) {
		t.Fatalf("cleanup() stat error = %v, want removed file", err)
	}

	if _, _, _, err := service.stageImportSource(ctx, ImportSource{Reader: strings.NewReader("abc"), Name: "too-large.md", Size: 4}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("stageImportSource(size limit) error = %v, want ErrInvalidInput", err)
	}
	if _, _, _, err := service.stageImportSource(ctx, ImportSource{Reader: strings.NewReader("abcd"), Name: "too-large.md"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("stageImportSource(reader limit) error = %v, want ErrInvalidInput", err)
	}
	if _, _, _, err := service.stageImportSource(ctx, ImportSource{Name: "missing.md"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("stageImportSource(nil reader) error = %v, want ErrInvalidInput", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, _, err := service.stageImportSource(canceled, ImportSource{Reader: strings.NewReader("abc"), Name: "canceled.md"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("stageImportSource(canceled) error = %v, want context.Canceled", err)
	}
}

func TestServicePersistImportedDocumentResetsAndWritesAssets(t *testing.T) {
	ctx := context.Background()
	search, err := search.NewBleveSearch(t.TempDir())
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: t.TempDir()}, search)
	doc := service.Doc("doc-1")

	if err := service.persistImportedDocument(ctx, doc, importedDocument{
		Root:  []byte(sampleMarkdown("Old", "1.0", "# Old\n")),
		Media: map[string][]byte{"media/old.png": []byte("old")},
	}); err != nil {
		t.Fatalf("persistImportedDocument(initial) error = %v", err)
	}

	if err := service.persistImportedDocument(ctx, doc, importedDocument{
		Root:     []byte(sampleMarkdown("New", "2.0", "# New\n")),
		Media:    map[string][]byte{"media/nested/new.png": []byte("new")},
		Slides:   map[string][]byte{"slides/slide-1.png": []byte("slide")},
		Versions: map[string][]byte{"old.md": []byte("old version"), "versions/kept.md": []byte("kept version")},
	}); err != nil {
		t.Fatalf("persistImportedDocument(replace) error = %v", err)
	}

	media, err := service.ListMedia(ctx, doc)
	if err != nil {
		t.Fatalf("ListMedia() error = %v", err)
	}
	if got, want := strings.Join(media, ","), "media/nested/new.png"; got != want {
		t.Fatalf("ListMedia() = %q, want %q", got, want)
	}
	if file, err := service.OpenMedia(ctx, doc, "old.png"); err == nil {
		_ = file.Close()
		t.Fatal("OpenMedia(old.png) succeeded, want reset document to remove old asset")
	}

	slide, err := service.OpenSlide(ctx, doc, "slide-1.png")
	if err != nil {
		t.Fatalf("OpenSlide() error = %v", err)
	}
	defer slide.Close()
	slideBody, err := io.ReadAll(slide)
	if err != nil {
		t.Fatalf("ReadAll(slide) error = %v", err)
	}
	if string(slideBody) != "slide" {
		t.Fatalf("slide body = %q, want slide", string(slideBody))
	}

	versions, err := service.listAllFiles(ctx, doc, service.paths().VersionsDir)
	if err != nil {
		t.Fatalf("listAllFiles(versions) error = %v", err)
	}
	if got, want := strings.Join(versions, ","), "versions/kept.md,versions/old.md"; got != want {
		t.Fatalf("versions = %q, want %q", got, want)
	}
}

func TestServiceRenderAccessors(t *testing.T) {
	ctx := context.Background()
	search, err := search.NewBleveSearch(t.TempDir())
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: t.TempDir()}, search)
	doc := importTestMarkdown(t, service, "outline.md", sampleMarkdown("Outline", "1.0", strings.Join([]string{
		"# Intro",
		"",
		"## Details",
		"",
		"```md",
		"# Ignored",
		"```",
		"",
		"### Deep",
		"",
		"# Intro",
		"",
	}, "\n")))

	index, err := service.HeadingIndex(ctx, doc, 2)
	if err != nil {
		t.Fatalf("HeadingIndex() error = %v", err)
	}
	if len(index) != 2 {
		t.Fatalf("HeadingIndex() root len = %d, want 2: %+v", len(index), index)
	}
	if index[0].Text != "Intro" || index[0].AnchorID != "intro" {
		t.Fatalf("HeadingIndex()[0] = %+v, want Intro/intro", index[0])
	}
	if len(index[0].Children) != 1 || index[0].Children[0].Text != "Details" || index[0].Children[0].AnchorID != "details" {
		t.Fatalf("HeadingIndex()[0].Children = %+v, want Details child", index[0].Children)
	}
	if index[1].Text != "Intro" || index[1].AnchorID != "intro-2" {
		t.Fatalf("HeadingIndex()[1] = %+v, want duplicate intro anchor", index[1])
	}

	parts, err := service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("GetMarkdownBody() error = %v", err)
	}
	body := parts.Body

	if hasFrontmatter(body) || !strings.Contains(body, "# Intro") {
		t.Fatalf("GetMarkdownBody() = %q, want body without frontmatter", body)
	}

	if err := service.WriteFrontmatter(ctx, doc, Frontmatter{Title: "Replacement", Version: "2.0", Language: "de"}, UpdateOptions{}); err != nil {
		t.Fatalf("SetFrontmatter() error = %v", err)
	}

	parts, err = service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("RenderFrontmatter() error = %v", err)
	}
	fm := parts.Frontmatter

	if fm.Title != "Replacement" || fm.Version != "2.0" || fm.Language != "de" {
		t.Fatalf("RenderFrontmatter() = %+v, want replacement metadata", fm)
	}

	parts, err = service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("GetMarkdownBody(after SetFrontmatter) error = %v", err)
	}
	body = parts.Body

	if !strings.Contains(body, "## Details") {
		t.Fatalf("SetFrontmatter() did not preserve body:\n%s", body)
	}
}

func TestServiceCleanTranslateAndDetectLanguage(t *testing.T) {
	ctx := context.Background()
	search, err := search.NewBleveSearch(t.TempDir())
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: t.TempDir()}, search)
	doc := importTestMarkdown(t, service, "clean.md", sampleMarkdown("Clean", "1.0", "![Alt](folder\\image one.png)\n\nEscaped \\# heading\n"))

	if err := service.Clean(ctx, doc, UpdateOptions{}); err != nil {
		t.Fatalf("Clean() error = %v", err)
	}

	parts, err := service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("ReadMarkdown(translated) error = %v", err)
	}
	root := parts.Full

	if !strings.Contains(root, "![Alt](/media/image_one.png)") || !strings.Contains(root, "Escaped # heading") {
		t.Fatalf("Clean() root missing normalized content:\n%s", root)
	}

	translated := sampleMarkdown("Ubersetzt", "1.0", "# Hallo\n")
	translator := &fakeAIClient{responses: []string{translated}}
	if err := service.Translate(ctx, doc, translator, "German", true, UpdateOptions{}); err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if len(translator.instructions) != 1 || !strings.Contains(translator.instructions[0], "natural idiomatic phrasing") || !strings.Contains(translator.instructions[0], "german") {
		t.Fatalf("Translate() instruction = %#v, want rephrase German instruction", translator.instructions)
	}

	parts, err = service.ReadMarkdownParts(ctx, doc)
	if err != nil {
		t.Fatalf("ReadMarkdown(translated) error = %v", err)
	}
	root = parts.Full

	fm, err := parseFrontmatter(root)
	if err != nil {
		t.Fatalf("ParseFrontmatter(translated) error = %v", err)
	}
	if fm.Title != "Ubersetzt" || fm.Language != "de" || !strings.Contains(stripFrontmatter(root), "# Hallo") {
		t.Fatalf("Translate() root/frontmatter = %+v\n%s", fm, root)
	}

	detector := &fakeAIClient{responses: []string{`"EN"`}}
	lang, err := service.DetectLanguage(ctx, doc, detector)
	if err != nil {
		t.Fatalf("DetectLanguage() error = %v", err)
	}
	if lang != "en" {
		t.Fatalf("DetectLanguage() = %q, want en", lang)
	}

	if err := service.Translate(ctx, doc, nil, "de", false, UpdateOptions{}); !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("Translate(nil client) error = %v, want ErrAIUnavailable", err)
	}
	if err := service.Translate(ctx, doc, translator, "", false, UpdateOptions{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Translate(empty target) error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.DetectLanguage(ctx, doc, &fakeAIClient{responses: []string{"eng"}}); !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("DetectLanguage(invalid code) error = %v, want ErrAIUnavailable", err)
	}
	if _, err := service.DetectLanguage(ctx, doc, nil); !errors.Is(err, ErrAIUnavailable) {
		t.Fatalf("DetectLanguage(nil client) error = %v, want ErrAIUnavailable", err)
	}
}

func TestServiceImportZipValidationAndExportErrors(t *testing.T) {
	ctx := context.Background()
	search, err := search.NewBleveSearch(t.TempDir())
	if err != nil {
		t.Log(err)
	}
	service := NewService(dpstore.FS{BasePath: t.TempDir()}, search)

	if _, err := service.ImportZip(ctx, nil, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ImportZip(nil reader) error = %v, want ErrInvalidInput", err)
	}

	noRootZip := makeTestZip(t, map[string]string{
		"nested/root.md": sampleMarkdown("Nested", "1.0", "# Nested\n"),
		"media/logo.png": "image",
	})
	if _, err := service.ImportZip(ctx, bytes.NewReader(noRootZip), int64(len(noRootZip))); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ImportZip(no root) error = %v, want ErrInvalidInput", err)
	}

	multipleRootsZip := makeTestZip(t, map[string]string{
		"a.md": sampleMarkdown("A", "1.0", "# A\n"),
		"b.md": sampleMarkdown("B", "1.0", "# B\n"),
	})
	if _, err := service.ImportZip(ctx, bytes.NewReader(multipleRootsZip), int64(len(multipleRootsZip))); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ImportZip(multiple roots) error = %v, want ErrInvalidInput", err)
	}

	doc := service.Doc("bad-frontmatter")
	if err := service.Store.WriteFile(ctx, doc.ID, service.paths().RootMarkdown, []byte("---\ntitle: bad\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(bad root) error = %v", err)
	}
	var buf bytes.Buffer
	if err := service.ExportZip(ctx, doc, zip.NewWriter(&buf)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ExportZip(bad frontmatter) error = %v, want ErrInvalidInput", err)
	}
	if err := service.ExportZip(ctx, doc, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ExportZip(nil writer) error = %v, want ErrInvalidInput", err)
	}
}

func TestZipHelpers(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: " /root.md ", want: "root.md"},
		{name: `media\logo.png`, want: "media/logo.png"},
		{name: "../evil.md", want: ""},
		{name: "safe/../../evil.md", want: ""},
		{name: ".", want: ""},
	}
	for _, tc := range cases {
		if got := markdownZipCleanEntryName(tc.name); got != tc.want {
			t.Fatalf("markdownZipCleanEntryName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}

	if !markdownZipIsRootMarkdown("root.md") || !markdownZipIsRootMarkdown("root.markdown") {
		t.Fatal("markdownZipIsRootMarkdown(root markdown) = false, want true")
	}
	for _, name := range []string{"nested/root.md", "root.txt", "media/image.png"} {
		if markdownZipIsRootMarkdown(name) {
			t.Fatalf("markdownZipIsRootMarkdown(%q) = true, want false", name)
		}
	}

	if err := markdownWriteZipEntry(nil, "root.md", []byte("body")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("markdownWriteZipEntry(nil writer) error = %v, want ErrInvalidInput", err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := markdownWriteZipEntry(zw, "root.md", []byte("body")); err != nil {
		t.Fatalf("markdownWriteZipEntry() error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close() error = %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "root.md" {
		t.Fatalf("zip entries = %+v, want root.md", zr.File)
	}
	body, err := markdownZipReadFile(zr.File[0])
	if err != nil {
		t.Fatalf("markdownZipReadFile() error = %v", err)
	}
	if string(body) != "body" {
		t.Fatalf("markdownZipReadFile() = %q, want body", string(body))
	}
	if _, err := markdownZipReadFile(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("markdownZipReadFile(nil) error = %v, want ErrInvalidInput", err)
	}

	id1, err := markdownUUID()
	if err != nil {
		t.Fatalf("markdownUUID() error = %v", err)
	}
	id2, err := markdownUUID()
	if err != nil {
		t.Fatalf("markdownUUID() second error = %v", err)
	}
	if id1 == id2 || len(id1) != 36 || id1[8] != '-' || id1[13] != '-' || id1[18] != '-' || id1[23] != '-' {
		t.Fatalf("markdownUUID() values = %q, %q; want distinct UUID-shaped values", id1, id2)
	}
}

func TestExternalToolHelpers(t *testing.T) {
	if contextOrBackground(nil) == nil {
		t.Fatal("contextOrBackground(nil) = nil, want background context")
	}
	ctx := context.Background()
	if contextOrBackground(ctx) != ctx {
		t.Fatal("contextOrBackground(ctx) did not return the provided context")
	}

	deadlineCtx, deadlineCancel := context.WithDeadline(ctx, time.Now().Add(time.Minute))
	defer deadlineCancel()
	gotCtx, cancel, timeout := contextWithToolTimeout(deadlineCtx, time.Hour)
	defer cancel()
	if gotCtx != deadlineCtx || timeout <= 0 || timeout > time.Minute {
		t.Fatalf("contextWithToolTimeout(existing deadline) = (%v, %s), want original context and remaining timeout", gotCtx, timeout)
	}

	gotCtx, cancel, timeout = contextWithToolTimeout(ctx, time.Second)
	defer cancel()
	if gotCtx == ctx || timeout != time.Second {
		t.Fatalf("contextWithToolTimeout(no deadline) timeout/context = (%v, %s), want child context with requested timeout", gotCtx, timeout)
	}
	if _, ok := gotCtx.Deadline(); !ok {
		t.Fatal("contextWithToolTimeout(no deadline) returned context without deadline")
	}

	toolName := "docpipe-test-tool.cmd"
	toolDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(toolDir, toolName), []byte("@echo off\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(tool) error = %v", err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	path, err := requiredTool(toolName)
	if err != nil {
		t.Fatalf("requiredTool(%q) error = %v", toolName, err)
	}
	if filepath.Base(path) != toolName {
		t.Fatalf("requiredTool(%q) = %q, want temp tool path", toolName, path)
	}
	if _, err := requiredTool("__docpipe_missing_tool__"); !errors.Is(err, ErrUnsupported) || !errors.Is(err, ErrToolMissing) {
		t.Fatalf("requiredTool(missing) error = %v, want ErrUnsupported and ErrToolMissing", err)
	}

	path, err = firstAvailableTool("__docpipe_missing_tool__", toolName)
	if err != nil {
		t.Fatalf("firstAvailableTool() error = %v", err)
	}
	if filepath.Base(path) != toolName {
		t.Fatalf("firstAvailableTool() = %q, want temp tool path", path)
	}
	if _, err := firstAvailableTool("__docpipe_missing_one__", "__docpipe_missing_two__"); !errors.Is(err, ErrUnsupported) || !errors.Is(err, ErrToolMissing) {
		t.Fatalf("firstAvailableTool(all missing) error = %v, want ErrUnsupported and ErrToolMissing", err)
	}

	canceled, cancelCanceled := context.WithCancel(ctx)
	cancelCanceled()
	if err := commandRunError(canceled, "tool", 0, errors.New("exit"), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("commandRunError(canceled) error = %v, want context.Canceled", err)
	}
	expired, cancelExpired := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := commandRunError(expired, "tool", time.Second, errors.New("exit"), nil); !errors.Is(err, ErrTimeout) {
		t.Fatalf("commandRunError(timeout) error = %v, want ErrTimeout", err)
	}
	if err := commandRunError(ctx, "tool", 0, errors.New("exit status 1"), []byte("bad stderr\n")); err == nil || !strings.Contains(err.Error(), "bad stderr") {
		t.Fatalf("commandRunError(stderr) error = %v, want stderr in error", err)
	}

	long := strings.Repeat("a", defaultCommandErrorSnippetSize+5)
	snippet := responseSnippet([]byte(long))
	if len(snippet) != defaultCommandErrorSnippetSize+3 || !strings.HasSuffix(snippet, "...") {
		t.Fatalf("responseSnippet(long) len/suffix = %d/%q, want truncated with ellipsis", len(snippet), snippet[len(snippet)-3:])
	}
	if got := responseSnippet([]byte("  short  \n")); got != "short" {
		t.Fatalf("responseSnippet(short) = %q, want short", got)
	}
}

type fakeAIClient struct {
	responses    []string
	err          error
	instructions []string
	inputs       []string
}

func (c *fakeAIClient) Generate(ctx context.Context, instructions, input string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.instructions = append(c.instructions, instructions)
	c.inputs = append(c.inputs, input)
	if c.err != nil {
		return "", c.err
	}
	if len(c.responses) == 0 {
		return "", nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func importTestMarkdown(t *testing.T, service Service, name, markdown string) Document {
	t.Helper()
	doc, err := service.ImportDocument(context.Background(), ImportSource{
		Reader: strings.NewReader(markdown),
		Name:   name,
		Size:   int64(len(markdown)),
	})
	if err != nil {
		t.Fatalf("ImportDocument(%q) error = %v", name, err)
	}
	return doc
}

func sampleMarkdown(title, version, body string) string {
	return mdComposeMarkdownWithMeta(Frontmatter{
		Title:            title,
		Date:             "2026-04-26 12:00:00",
		ChangedDate:      "2026-04-26 12:00:00",
		OriginalDocument: "document/sample.md",
		OriginalFormat:   "md",
		Version:          version,
		Language:         "en",
	}, body)
}

func makeTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := sortedStringKeys(entries)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip Create(%q) error = %v", name, err)
		}
		if _, err := w.Write([]byte(entries[name])); err != nil {
			t.Fatalf("zip Write(%q) error = %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close() error = %v", err)
	}
	return buf.Bytes()
}

func readTestZip(t *testing.T, filename string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatalf("zip OpenReader(%q) error = %v", filename, err)
	}
	defer zr.Close()

	entries := make(map[string]string, len(zr.File))
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("zip entry Open(%q) error = %v", file.Name, err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("zip entry ReadAll(%q) error = %v", file.Name, err)
		}
		entries[file.Name] = string(body)
	}
	return entries
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
