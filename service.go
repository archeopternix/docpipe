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

	"docpipe/store"
)

type Service struct {
	Store store.Store
	Paths Paths

	Import struct {
		IncludeImages bool
		IncludeSlides bool
		MaxBytes      int64
		TempDir       string
	}
}

type Document struct {
	ID string
}

type Paths struct {
	RootMarkdown string
	MediaDir     string
	SlidesDir    string
	VersionsDir  string
}

type UpdateOptions struct {
	ArchivePrevious bool
	BumpVersion     bool
	Now             func() time.Time
}

type ImportSource struct {
	Reader   io.Reader
	Name     string
	Size     int64
	MimeType string
	ModTime  time.Time
}

type WordOptions struct {
	IncludeImages bool
}

type PptxOptions struct {
	IncludeImages bool
	IncludeSlides bool
}

func NewService(st store.Store) Service {
	s := Service{Store: st}
	s.Import.IncludeImages = true
	s.Import.IncludeSlides = true
	s.Import.MaxBytes = defaultMaxZipEntryReadBytes
	return s
}

func DefaultPaths() Paths {
	return Paths{
		RootMarkdown: "root.md",
		MediaDir:     "media",
		SlidesDir:    "slides",
		VersionsDir:  "versions",
	}
}

func (s Service) Doc(id string) Document {
	return Document{ID: strings.TrimSpace(id)}
}

func (s Service) ReadMarkdown(ctx context.Context, doc Document) (string, error) {
	body, err := s.readFile(ctx, doc, s.paths().RootMarkdown)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s Service) ListMedia(ctx context.Context, doc Document) ([]string, error) {
	return s.listNames(ctx, doc, s.paths().MediaDir)
}

func (s Service) OpenMedia(ctx context.Context, doc Document, name string) (fs.File, error) {
	return s.openAsset(ctx, doc, s.paths().MediaDir, name)
}

func (s Service) ListSlides(ctx context.Context, doc Document) ([]string, error) {
	return s.listNames(ctx, doc, s.paths().SlidesDir)
}

func (s Service) OpenSlide(ctx context.Context, doc Document, name string) (fs.File, error) {
	return s.openAsset(ctx, doc, s.paths().SlidesDir, name)
}

func (s Service) WriteMarkdown(ctx context.Context, doc Document, root string, opt UpdateOptions) error {
	if err := s.ensureService(doc); err != nil {
		return err
	}

	current, exists, err := s.readMarkdownIfExists(ctx, doc)
	if err != nil {
		return err
	}
	if exists && opt.ArchivePrevious {
		if err := s.archiveMarkdown(ctx, doc, current, opt); err != nil {
			return err
		}
	}

	finalRoot, err := s.prepareMarkdown(root, current, opt)
	if err != nil {
		return err
	}
	return s.Store.WriteFile(ctx, doc.ID, s.paths().RootMarkdown, []byte(finalRoot), 0o644)
}

func (s Service) UpdateFrontmatter(ctx context.Context, doc Document, fm Frontmatter, opt UpdateOptions) error {
	current, err := s.ReadMarkdown(ctx, doc)
	if err != nil {
		return err
	}
	currentFM, err := ParseFrontmatter(current)
	if err != nil {
		return err
	}
	fm = mdMergeFrontmatter(fm, currentFM)
	return s.WriteMarkdown(ctx, doc, mdComposeMarkdownWithMeta(fm, StripFrontmatter(current)), opt)
}

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

	switch ext {
	case ".docx":
		err = s.importDocx(ctx, doc, src, WordOptions{IncludeImages: s.importConfig().IncludeImages})
	case ".pptx":
		err = s.importPptx(ctx, doc, src, PptxOptions{
			IncludeImages: s.importConfig().IncludeImages,
			IncludeSlides: s.importConfig().IncludeSlides,
		})
	case ".md", ".markdown":
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

func (s Service) ExportZip(ctx context.Context, doc Document, w *zip.Writer) error {
	if err := s.ensureService(doc); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("%w: zip writer is nil", ErrInvalidInput)
	}

	root, err := s.ReadMarkdown(ctx, doc)
	if err != nil {
		return err
	}
	if _, err := ParseFrontmatter(root); err != nil {
		return err
	}
	if err := markdownWriteZipEntry(w, s.paths().RootMarkdown, []byte(root)); err != nil {
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

func (s Service) importExtensionFromMime(mimeType string) string {
	if mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(mimeType)); err == nil {
		switch mediaType {
		case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
			return ".docx"
		case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
			return ".pptx"
		case "text/markdown":
			return ".md"
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
	fm, err := ParseFrontmatter(root)
	if err != nil {
		return "", err
	}
	currentFM, err := ParseFrontmatter(current)
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
	return mdComposeMarkdownWithMeta(fm, StripFrontmatter(root)), nil
}

func (s Service) archiveMarkdown(ctx context.Context, doc Document, current string, opt UpdateOptions) error {
	fm, err := ParseFrontmatter(current)
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
