package docpipe

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/archeopternix/docpipe/search"
	"github.com/archeopternix/docpipe/store"
)

// Service provides high-level document operations backed by a store (read/write markdown, assets, import/export).
type Service struct {
	Store  store.Store
	Search search.SearchProvider
	Paths  Paths

	Import struct {
		IncludeImages bool
		IncludeSlides bool
		MaxBytes      int64
		TempDir       string
	}
}

// Document identifies a document managed by Service.
type Document struct {
	ID string
}

// Paths configures where a document's files live inside the store.
type Paths struct {
	RootMarkdown string
	MediaDir     string
	SlidesDir    string
	VersionsDir  string
	OriginalDir  string
}

// UpdateOptions controls behavior when updating markdown/frontmatter.
type UpdateOptions struct {
	ArchivePrevious bool             // if true, save current root.md into VersionsDir before overwriting
	BumpVersion     bool             // if true, bump frontmatter version + update ChangedDate
	Now             func() time.Time // optional clock (UTC is enforced)
}

// ImportSource describes a file to import.
type ImportSource struct {
	Reader   io.Reader // content stream
	Name     string    // filename (used to infer extension/frontmatter defaults)
	Size     int64     // size hint (used for zip staging/limits)
	MimeType string    // optional MIME type (used when Name has no extension)
	ModTime  time.Time // optional timestamp used for default dates
}

// WordOptions configures DOCX import.
type WordOptions struct {
	IncludeImages bool
}

// PptxOptions configures PPTX import.
type PptxOptions struct {
	IncludeImages bool
	IncludeSlides bool
}

// NewService creates a Service with sensible import defaults.
// Parameter: st is the backing store (must be non-nil when calling methods).
func NewService(st store.Store, sp search.SearchProvider) Service {
	s := Service{Store: st, Search: sp}
	s.Import.IncludeImages = true
	s.Import.IncludeSlides = true
	s.Import.MaxBytes = defaultMaxZipEntryReadBytes
	return s
}

// DefaultPaths returns the default store layout
//
//		root.md - the main markdown document
//		media/ - keeps all the images embedded in pptx or docx
//	 slides/ - screenshots of pptx slides
//	 versions/ - old versions of markdown files
func DefaultPaths() Paths {
	return Paths{
		RootMarkdown: "root.md",
		MediaDir:     "media",
		SlidesDir:    "slides",
		VersionsDir:  "versions",
		OriginalDir:  "original",
	}
}

// Markdown
type Markdown struct {
	Full           string // full root.md
	Body           string // without frontmatter
	Frontmatter    Frontmatter
	HasFrontmatter bool
}

// Doc returns a Document handle for id (whitespace-trimmed).
func (s Service) Doc(id string) Document {
	return Document{ID: strings.TrimSpace(id)}
}

func (s Service) ReadMarkdownParts(ctx context.Context, doc Document) (Markdown, error) {
	fb, err := s.readFile(ctx, doc, s.paths().RootMarkdown)
	if err != nil {
		return Markdown{}, err
	}
	fullbody := string(fb)

	fm, err := parseFrontmatter(fullbody)
	if err != nil {
		fm = Frontmatter{}
	}

	return Markdown{
		Full:           fullbody,
		Body:           stripFrontmatter(fullbody),
		Frontmatter:    fm,
		HasFrontmatter: hasFrontmatter(fullbody),
	}, nil
}

// ListMedia lists stored media asset paths under MediaDir (sorted). Returns nil if none.
func (s Service) ListMedia(ctx context.Context, doc Document) ([]string, error) {
	return s.listNames(ctx, doc, s.paths().MediaDir)
}

// OpenMedia opens a media asset by name.
// Parameter: name may be relative; it is cleaned/validated to stay within MediaDir.
func (s Service) OpenMedia(ctx context.Context, doc Document, name string) (fs.File, error) {
	return s.openAsset(ctx, doc, s.paths().MediaDir, name)
}

// ListSlides lists stored slide asset paths under SlidesDir (sorted). Returns nil if none.
func (s Service) ListSlides(ctx context.Context, doc Document) ([]string, error) {
	return s.listNames(ctx, doc, s.paths().SlidesDir)
}

// OpenSlide opens a slide asset by name.
// Parameter: name may be relative; it is cleaned/validated to stay within SlidesDir.
func (s Service) OpenSlide(ctx context.Context, doc Document, name string) (fs.File, error) {
	return s.openAsset(ctx, doc, s.paths().SlidesDir, name)
}

// WriteMarkdown writes root markdown, optionally archiving the previous version and/or bumping frontmatter version.
// Parameters: root is the new markdown; opt controls archiving/version bump behavior.
func (s Service) WriteMarkdown(ctx context.Context, doc Document, root string, opt UpdateOptions) error {
	if err := s.ensureService(doc); err != nil {
		return err
	}

	parts, err := s.ReadMarkdownParts(ctx, doc)
	if err != nil {
		return err
	}

	// archive the "old" markdown
	if len(parts.Full) > 0 {
		if err := s.archiveMarkdown(ctx, doc, parts.Full, opt); err != nil {
			return err
		}
	}

	finalRoot, err := s.prepareMarkdown(root, parts.Full, opt)
	if err != nil {
		return err
	}
	return s.Store.WriteFile(ctx, doc.ID, s.paths().RootMarkdown, []byte(finalRoot), 0o644)
}

// UpdateFrontmatter updates only the frontmatter fields provided in fm (missing fields keep current values).
// Parameters: fm is merged into existing frontmatter; opt is passed through to WriteMarkdown.
func (s Service) UpdateFrontmatter(ctx context.Context, doc Document, fm Frontmatter, opt UpdateOptions) error {
	parts, err := s.ReadMarkdownParts(ctx, doc)
	if err != nil {
		return err
	}
	parts.Frontmatter = mdMergeFrontmatter(fm, parts.Frontmatter)

	return s.WriteMarkdown(ctx, doc, mdComposeMarkdownWithMeta(parts.Frontmatter, parts.Body), opt)
}

// ImportDocument creates a new document and imports content from src.
// Parameter: src.Name/src.MimeType determine the format (.docx/.pptx/.md/.zip).
func (s Service) ImportDocument(ctx context.Context, src ImportSource) (Document, error) {
	if err := s.ensureStoreOnly(); err != nil {
		return Document{}, err
	}
	docID, err := markdownUUID()
	if err != nil {
		return Document{}, err
	}
	doc := s.Doc(docID)

	ext := strings.ToLower(filepath.Ext(src.Name))
	if ext == "" {
		ext = s.importExtensionFromMime(src.MimeType)
	}

	// switch depending on extension (with MIME-based fallback) - if unsupported,
	// we still keep the original file in the store for potential future use
	switch ext {
	case ".docx":
		err = s.importDocx(ctx, doc, src, WordOptions{
			IncludeImages: s.importConfig().IncludeImages,
		})
	case ".pptx":
		err = s.importPptx(ctx, doc, src, PptxOptions{
			IncludeImages: s.importConfig().IncludeImages,
			IncludeSlides: s.importConfig().IncludeSlides,
		})
	case ".md", ".markdown", ".txt":
		err = s.importMarkdownFile(ctx, doc, src)
	case ".zip":
		file, size, cleanup, stageErr := s.stageImportSource(ctx, src)
		if stageErr != nil {
			return Document{}, stageErr
		}
		defer cleanup()
		err = s.ImportZipInto(ctx, doc, file, size)
	default:
		err = fmt.Errorf("%w: import format not supported for %q", ErrUnsupported, ext)
	}
	if err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ImportZip creates a new document by importing a docpipe zip.
// Parameters: r/size must describe the full zip content.
func (s Service) ImportZip(ctx context.Context, r io.ReaderAt, size int64) (Document, error) {
	if err := s.ensureStoreOnly(); err != nil {
		return Document{}, err
	}
	docID, err := markdownUUID()
	if err != nil {
		return Document{}, err
	}
	doc := s.Doc(docID)
	if err := s.ImportZipInto(ctx, doc, r, size); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ImportZipInto imports a docpipe zip into an existing document, replacing current contents.
// Parameters: doc selects the target; r/size must describe the full zip content.
func (s Service) ImportZipInto(ctx context.Context, doc Document, r io.ReaderAt, size int64) error {
	if err := s.ensureService(doc); err != nil {
		return err
	}
	if r == nil || size < 0 {
		return fmt.Errorf("%w: invalid zip source", ErrInvalidInput)
	}

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	var roots []string
	for _, file := range zr.File {
		name := markdownZipCleanEntryName(file.Name)
		if name == "" || file.FileInfo().IsDir() {
			continue
		}
		if markdownZipIsRootMarkdown(name) {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		return fmt.Errorf("%w: zip does not contain a root markdown file", ErrInvalidInput)
	}
	if len(roots) > 1 {
		return fmt.Errorf("%w: zip contains multiple root markdown files", ErrInvalidInput)
	}

	if err := s.resetDocument(ctx, doc); err != nil {
		return err
	}

	for _, file := range zr.File {
		name := markdownZipCleanEntryName(file.Name)
		if name == "" || file.FileInfo().IsDir() {
			continue
		}
		body, err := markdownZipReadFile(file)
		if err != nil {
			return err
		}

		switch {
		case name == roots[0]:
			if err := s.Store.WriteFile(ctx, doc.ID, s.paths().RootMarkdown, body, 0o644); err != nil {
				return err
			}
		case strings.HasPrefix(name, s.paths().MediaDir+"/"),
			strings.HasPrefix(name, s.paths().SlidesDir+"/"),
			strings.HasPrefix(name, s.paths().VersionsDir+"/"):
			if err := s.Store.WriteFile(ctx, doc.ID, name, body, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// ExportZip writes a docpipe zip for doc into w (root.md + media/slides/versions when present).
func (s Service) ExportZip(ctx context.Context, doc Document, w *zip.Writer) error {
	if err := s.ensureService(doc); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("%w: zip writer is nil", ErrInvalidInput)
	}

	// write markdown to zip
	parts, err := s.ReadMarkdownParts(ctx, doc)
	if err != nil {
		return err
	}
	if err := markdownWriteZipEntry(w, s.paths().RootMarkdown, []byte(parts.Full)); err != nil {
		return err
	}

	for _, dir := range []string{s.paths().MediaDir, s.paths().SlidesDir, s.paths().VersionsDir} {
		names, err := s.listAllFiles(ctx, doc, dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		for _, name := range names {
			body, err := s.readFile(ctx, doc, name)
			if err != nil {
				return err
			}
			if err := markdownWriteZipEntry(w, name, body); err != nil {
				return err
			}

		}
	}
	return nil
}

// ListDir lists entries under the docpipe store "root" directory (or a subdir) where
// parameter dir is optional and interpreted as follows:
// - "" or "." => list the store root
// - "some/subdir" => list that subdir under the store root (unless you decide dir is already absolute in store terms)
func (s Service) ListDir(ctx context.Context, dir string) ([]fs.DirEntry, error) {
	if s.Store == nil {
		return nil, ErrInvalidInput
	}

	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "." {
		// Convention: store root. Adjust if your store expects a non-empty path.
		dir = "."
	} else {
		// Normalize for safety; prevents odd inputs like "a/../b".
		dir = filepath.Clean(filepath.FromSlash(dir))
	}

	return s.Store.ListDir(ctx, dir)
}

func (s Service) importExtensionFromMime(mimeType string) string {
	if mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(mimeType)); err == nil {
		switch mediaType {
		case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
			return ".docx"
		case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
			return ".pptx"
		case "text/markdown":
			return ".md"
		case "text/plain", "application/yaml", "text/yaml", "text/html":
			return ".txt"
		}
	}
	return ""
}

func (s Service) paths() Paths {
	paths := s.Paths
	defaults := DefaultPaths()
	if strings.TrimSpace(paths.RootMarkdown) == "" {
		paths.RootMarkdown = defaults.RootMarkdown
	}
	if strings.TrimSpace(paths.MediaDir) == "" {
		paths.MediaDir = defaults.MediaDir
	}
	if strings.TrimSpace(paths.SlidesDir) == "" {
		paths.SlidesDir = defaults.SlidesDir
	}
	if strings.TrimSpace(paths.VersionsDir) == "" {
		paths.VersionsDir = defaults.VersionsDir
	}
	if strings.TrimSpace(paths.OriginalDir) == "" {
		paths.OriginalDir = defaults.OriginalDir
	}

	return paths
}

func (s Service) importConfig() struct {
	IncludeImages bool
	IncludeSlides bool
	MaxBytes      int64
	TempDir       string
} {
	cfg := s.Import
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = defaultMaxZipEntryReadBytes
	}
	return cfg
}

func (s Service) ensureService(doc Document) error {
	if err := s.ensureStoreOnly(); err != nil {
		return err
	}
	if strings.TrimSpace(doc.ID) == "" {
		return fmt.Errorf("%w: document id is empty", ErrInvalidInput)
	}
	return nil
}

func (s Service) ensureStoreOnly() error {
	if s.Store == nil {
		return fmt.Errorf("%w: service store is nil", ErrInvalidInput)
	}
	return nil
}

func (s Service) readFile(ctx context.Context, doc Document, name string) ([]byte, error) {
	file, err := s.Store.Open(ctx, doc.ID, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

/*
func (s Service) readMarkdownIfExists(ctx context.Context, doc Document) (string, bool, error) {
	body, err := s.readFile(ctx, doc, s.paths().RootMarkdown)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(body), true, nil
}
*/

func (s Service) openAsset(ctx context.Context, doc Document, dir, name string) (fs.File, error) {
	rawName := strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if rawName == "" {
		return nil, fmt.Errorf("%w: asset path is empty", ErrInvalidInput)
	}
	if strings.HasPrefix(rawName, "/") {
		return nil, fmt.Errorf("%w: invalid asset path %q", ErrInvalidInput, name)
	}
	for _, segment := range strings.Split(rawName, "/") {
		if segment == ".." {
			return nil, fmt.Errorf("%w: invalid asset path %q", ErrInvalidInput, name)
		}
	}

	cleanName := path.Clean(rawName)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return nil, fmt.Errorf("%w: invalid asset path %q", ErrInvalidInput, name)
	}
	if !strings.HasPrefix(cleanName, dir+"/") {
		cleanName = path.Join(dir, cleanName)
	}
	if cleanName == dir || !strings.HasPrefix(cleanName, dir+"/") {
		return nil, fmt.Errorf("%w: invalid asset path %q", ErrInvalidInput, name)
	}
	return s.Store.Open(ctx, doc.ID, cleanName)
}

func (s Service) listNames(ctx context.Context, doc Document, dir string) ([]string, error) {
	names, err := s.listAllFiles(ctx, doc, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return names, nil
}

func (s Service) listAllFiles(ctx context.Context, doc Document, dir string) ([]string, error) {
	entries, err := s.Store.ReadDir(ctx, doc.ID, dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := path.Join(dir, entry.Name())
		if entry.IsDir() {
			children, err := s.listAllFiles(ctx, doc, name)
			if err != nil {
				return nil, err
			}
			names = append(names, children...)
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s Service) prepareMarkdown(root, current string, opt UpdateOptions) (string, error) {
	if !opt.BumpVersion {
		return root, nil
	}

	now := s.now(opt)

	fm, err := parseFrontmatter(root)
	if err != nil {
		return "", err
	}
	currentFM, err := parseFrontmatter(current)
	if err != nil {
		return "", err
	}
	if fm.Version == "" {
		fm.Version = currentFM.Version
	}
	fm.Version = bumpMinorVersion(fm.Version)
	if fm.Date == "" {
		if currentFM.Date != "" {
			fm.Date = currentFM.Date
		} else {
			fm.Date = now.Format(datetimelayout)
		}
	}
	fm.ChangedDate = now.Format(datetimelayout)
	if fm.OriginalDocument == "" {
		fm.OriginalDocument = currentFM.OriginalDocument
	}
	if fm.OriginalFormat == "" {
		fm.OriginalFormat = currentFM.OriginalFormat
	}
	return mdComposeMarkdownWithMeta(fm, stripFrontmatter(root)), nil
}

func (s Service) archiveMarkdown(ctx context.Context, doc Document, current string, opt UpdateOptions) error {
	fm, err := parseFrontmatter(current)
	if err != nil {
		return err
	}
	archiveName := serviceArchiveName(s.paths().VersionsDir, fm.Version, s.now(opt))
	return s.Store.WriteFile(ctx, doc.ID, archiveName, []byte(current), 0o644)
}

func (s Service) now(opt UpdateOptions) time.Time {
	if opt.Now != nil {
		return opt.Now().UTC()
	}
	return time.Now().UTC()
}

func serviceArchiveName(dir, version string, now time.Time) string {
	version = mdNormalizeVersion(version)
	if version == "" {
		version = "unknown"
	}
	return path.Join(dir, fmt.Sprintf("%s_v%s.md", now.Format("20060102T150405.000000000Z"), version))
}

func (s Service) resetDocument(ctx context.Context, doc Document) error {
	for _, name := range []string{
		s.paths().RootMarkdown,
		s.paths().MediaDir,
		s.paths().SlidesDir,
		s.paths().VersionsDir,
	} {
		if err := s.Store.Remove(ctx, doc.ID, name); err != nil {
			return err
		}
	}
	return nil
}
