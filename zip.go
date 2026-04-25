package docpipe

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type readerAtStatter interface {
	Stat() (os.FileInfo, error)
}

type readerAtSizer interface {
	Size() int64
}

type readerAtNamer interface {
	Name() string
}

// OpenZip creates a lazy Markdown document backed by a docpipe ZIP bundle.
// It reads the ZIP directory and the root markdown frontmatter only; assets
// and the markdown body are opened on demand.
func OpenZip(r io.ReaderAt) (*Markdown, error) {
	if r == nil {
		return nil, fmt.Errorf("zip reader is nil")
	}

	size, ok := readerAtSize(r)
	if !ok || size < 0 {
		return nil, fmt.Errorf("zip reader size cannot be determined")
	}
	return openZipWithSize(r, size)
}

func openZipWithSize(r io.ReaderAt, size int64) (*Markdown, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}

	zipIndex := make(map[string]*zip.File, len(zr.File))
	var rootMarkdowns []string
	for _, file := range zr.File {
		name := markdownZipCleanEntryName(file.Name)
		if name == "" {
			continue
		}
		zipIndex[name] = file
		if !file.FileInfo().IsDir() && markdownZipIsRootMarkdown(name) {
			rootMarkdowns = append(rootMarkdowns, name)
		}
	}
	sort.Strings(rootMarkdowns)
	if len(rootMarkdowns) == 0 {
		return nil, fmt.Errorf("zip does not contain a root markdown file")
	}
	if len(rootMarkdowns) > 1 {
		return nil, fmt.Errorf("zip contains multiple root markdown files: %s", strings.Join(rootMarkdowns, ", "))
	}

	sourcePath := ""
	fileName := "document.zip"
	if named, ok := r.(readerAtNamer); ok {
		sourcePath = named.Name()
		if base := filepath.Base(sourcePath); base != "" && base != "." {
			fileName = base
		}
	}

	doc := &Markdown{
		fileName:         fileName,
		extractedImages:  make(map[string]*bytes.Buffer),
		extractedSlides:  make(map[string]*bytes.Buffer),
		markdownVersions: make(map[string]*bytes.Buffer),
		metaData:         *mdDefaultMetaData(fileName),
		zipr:             zr,
		zipIndex:         zipIndex,
		zipSize:          size,
		zipMarkdownName:  rootMarkdowns[0],
		zipPath:          sourcePath,
	}

	frontmatter, err := markdownZipReadFrontmatter(zipIndex[rootMarkdowns[0]])
	if err != nil {
		return nil, err
	}
	markdownZipApplyMarkdownMetaData(doc, rootMarkdowns[0], frontmatter)
	if doc.metaData.Title == "" {
		doc.metaData.Title = mdBaseStem(rootMarkdowns[0])
	}
	if doc.metaData.Version == "" {
		doc.metaData.Version = "1.0"
	}

	return doc, nil
}

func readerAtSize(r io.ReaderAt) (int64, bool) {
	if statter, ok := r.(readerAtStatter); ok {
		info, err := statter.Stat()
		if err == nil && info != nil {
			return info.Size(), true
		}
	}
	if sizer, ok := r.(readerAtSizer); ok {
		return sizer.Size(), true
	}
	if seeker, ok := r.(io.Seeker); ok {
		current, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, false
		}
		end, err := seeker.Seek(0, io.SeekEnd)
		if _, restoreErr := seeker.Seek(current, io.SeekStart); err != nil || restoreErr != nil {
			return 0, false
		}
		return end, true
	}
	return 0, false
}

func markdownZipReadFrontmatter(file *zip.File) (string, error) {
	if file == nil {
		return "", fmt.Errorf("markdown file is nil")
	}

	rc, err := file.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	reader := bufio.NewReader(rc)
	first, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if strings.TrimSpace(first) != "---" {
		return "", nil
	}

	var builder strings.Builder
	builder.WriteString(first)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			builder.WriteString(line)
			if strings.TrimSpace(line) == "---" {
				return builder.String(), nil
			}
		}
		if readErr == io.EOF {
			return builder.String(), nil
		}
		if readErr != nil {
			return "", readErr
		}
		if builder.Len() > 1<<20 {
			return "", fmt.Errorf("frontmatter exceeds 1 MiB")
		}
	}
}

// ListImages lists image entries without reading their content.
func (m *Markdown) ListImages(prefix string) ([]string, error) {
	return m.listAssets(prefix, "media/", true)
}

// ListSlides lists slide entries without reading their content.
func (m *Markdown) ListSlides(prefix string) ([]string, error) {
	return m.listAssets(prefix, "slides/", false)
}

func (m *Markdown) listAssets(prefix, fallbackPrefix string, imagesOnly bool) ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("markdown is nil")
	}

	prefix = markdownZipNormalizePrefix(prefix, fallbackPrefix)
	var names []string
	if m.zipIndex != nil {
		for name, file := range m.zipIndex {
			if file == nil || file.FileInfo().IsDir() || !strings.HasPrefix(name, prefix) {
				continue
			}
			if imagesOnly && !markdownZipIsImage(name) {
				continue
			}
			names = append(names, name)
		}
	} else {
		values := m.extractedSlides
		if imagesOnly {
			values = m.extractedImages
		}
		for name := range values {
			entryName := filepath.ToSlash(name)
			if !strings.HasPrefix(entryName, prefix) {
				continue
			}
			if imagesOnly && !markdownZipIsImage(entryName) {
				continue
			}
			names = append(names, entryName)
		}
	}

	sort.Strings(names)
	return names, nil
}

// ImageReader opens an image asset from the ZIP or in-memory bundle.
func (m *Markdown) ImageReader(assetPath string) (io.ReadCloser, error) {
	return m.assetReader("media", assetPath)
}

// SlideReader opens a slide asset from the ZIP or in-memory bundle.
func (m *Markdown) SlideReader(assetPath string) (io.ReadCloser, error) {
	return m.assetReader("slides", assetPath)
}

// GetImageBytes reads an image asset into a buffer.
func (m *Markdown) GetImageBytes(assetPath string) (*bytes.Buffer, error) {
	return m.assetBytes("media", assetPath)
}

// GetSlideBytes reads a slide asset into a buffer.
func (m *Markdown) GetSlideBytes(assetPath string) (*bytes.Buffer, error) {
	return m.assetBytes("slides", assetPath)
}

func (m *Markdown) assetBytes(folder, assetPath string) (*bytes.Buffer, error) {
	var rc io.ReadCloser
	var err error
	if folder == "media" {
		rc, err = m.ImageReader(assetPath)
	} else {
		rc, err = m.SlideReader(assetPath)
	}
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (m *Markdown) assetReader(folder, assetPath string) (io.ReadCloser, error) {
	if m == nil {
		return nil, fmt.Errorf("markdown is nil")
	}

	entryName, err := markdownZipNormalizeAssetPath(folder, assetPath)
	if err != nil {
		return nil, err
	}

	if m.zipIndex != nil {
		file := m.zipIndex[entryName]
		if file == nil || file.FileInfo().IsDir() {
			return nil, fmt.Errorf("asset %q not found", entryName)
		}
		return file.Open()
	}

	values := m.extractedSlides
	if folder == "media" {
		values = m.extractedImages
	}
	for _, key := range []string{entryName, strings.TrimPrefix(entryName, folder+"/")} {
		if buf := values[key]; buf != nil {
			return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
		}
	}
	return nil, fmt.Errorf("asset %q not found", entryName)
}

func markdownZipNormalizePrefix(prefix, fallback string) string {
	value := strings.ReplaceAll(strings.TrimSpace(prefix), "\\", "/")
	value = strings.TrimPrefix(value, "/")
	if value == "" {
		value = fallback
	}
	value = path.Clean(value)
	if value == "." {
		return ""
	}
	if !strings.HasSuffix(value, "/") {
		value += "/"
	}
	return value
}

func markdownZipNormalizeAssetPath(folder, assetPath string) (string, error) {
	name := strings.ReplaceAll(strings.TrimSpace(assetPath), "\\", "/")
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return "", fmt.Errorf("asset path is empty")
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return "", fmt.Errorf("invalid asset path %q", assetPath)
		}
	}
	name = path.Clean(name)
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return "", fmt.Errorf("invalid asset path %q", assetPath)
	}
	prefix := folder + "/"
	if !strings.HasPrefix(name, prefix) {
		name = prefix + name
	}
	if strings.HasSuffix(name, "/") {
		return "", fmt.Errorf("asset path %q is a directory", assetPath)
	}
	return name, nil
}

func markdownZipIsImage(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		return true
	default:
		return false
	}
}

func (m *Markdown) currentMarkdownBytes() ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("markdown is nil")
	}
	if m.markdownFile != nil {
		return append([]byte(nil), m.markdownFile.Bytes()...), nil
	}
	if m.zipIndex == nil {
		return nil, fmt.Errorf("markdown content is not loaded")
	}

	name := m.zipMarkdownName
	if name == "" {
		var roots []string
		for entryName, file := range m.zipIndex {
			if file != nil && !file.FileInfo().IsDir() && markdownZipIsRootMarkdown(entryName) {
				roots = append(roots, entryName)
			}
		}
		sort.Strings(roots)
		if len(roots) == 0 {
			return nil, fmt.Errorf("zip does not contain a root markdown file")
		}
		name = roots[0]
		m.zipMarkdownName = name
	}

	file := m.zipIndex[name]
	if file == nil {
		return nil, fmt.Errorf("markdown entry %q not found", name)
	}
	body, err := markdownZipReadFile(file)
	if err != nil {
		return nil, err
	}
	m.markdownFile = bytes.NewBuffer(append([]byte(nil), body...))
	return body, nil
}

func (m *Markdown) currentMarkdownString() (string, error) {
	body, err := m.currentMarkdownBytes()
	if err != nil {
		return "", err
	}
	return string(body), nil
}
