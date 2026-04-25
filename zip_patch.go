package docpipe

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type zipReplacement struct {
	Body   []byte
	Header *zip.FileHeader
}

// changeMarkdown stores a new markdown body with frontmatter, bumps the minor
// version, and archives the previous markdown version.
func (m *Markdown) changeMarkdown(md string) (versionID string, err error) {
	if m == nil {
		return "", fmt.Errorf("markdown is nil")
	}

	oldMarkdown, err := m.currentMarkdownBytes()
	if err != nil {
		return "", err
	}
	oldMeta := m.metaData
	oldRoot := m.zipMarkdownName
	if oldRoot == "" {
		oldRoot = mdFileName(oldMeta)
	}

	newMeta := m.metaData
	if parsed, ok, err := mdParseMetaData(md, newMeta); err != nil {
		return "", err
	} else if ok {
		newMeta = parsed
	}
	newMeta.Version = bumpMinorVersion(oldMeta.Version)
	newMeta.ChangedDate = time.Now().Format(datetimelayout)

	_, body, ok := mdSplitFrontmatterContent(md)
	if !ok {
		body = md
	}
	newMarkdown := mdComposeMarkdownWithMeta(newMeta, body)
	newRoot := mdFileName(newMeta)
	archiveName := markdownVersionArchiveEntryName(oldMeta.Version)

	if m.zipPath != "" {
		replacements := map[string]zipReplacement{
			newRoot: {
				Body: []byte(newMarkdown),
			},
			archiveName: {
				Body: oldMarkdown,
			},
		}
		removeNames := map[string]bool{
			oldRoot: true,
		}
		if oldRoot != newRoot {
			removeNames[newRoot] = true
		}
		if err := patchZipFile(m.zipPath, removeNames, replacements); err != nil {
			return "", err
		}
	}

	m.metaData = newMeta
	m.markdownFile = bytes.NewBufferString(newMarkdown)
	if m.markdownVersions == nil {
		m.markdownVersions = make(map[string]*bytes.Buffer)
	}
	m.markdownVersions[archiveName] = bytes.NewBuffer(append([]byte(nil), oldMarkdown...))
	m.zipMarkdownName = newRoot

	return newMeta.Version, nil
}

// PatchZip streams an existing ZIP to a replacement ZIP, substituting the
// current markdown entry and any in-memory markdown versions.
func (m *Markdown) PatchZip(archivepath string) error {
	if m == nil {
		return fmt.Errorf("markdown is nil")
	}
	if strings.TrimSpace(archivepath) == "" {
		archivepath = m.zipPath
	}
	if strings.TrimSpace(archivepath) == "" {
		return fmt.Errorf("archive path is empty")
	}

	markdownBytes, err := m.currentMarkdownBytes()
	if err != nil {
		return err
	}
	oldRoot, err := markdownRootNameInZip(archivepath)
	if err != nil {
		oldRoot = m.zipMarkdownName
		if oldRoot == "" {
			return err
		}
	}
	newRoot := mdFileName(m.metaData)

	replacements := map[string]zipReplacement{
		newRoot: {Body: markdownBytes},
	}
	removeNames := map[string]bool{
		oldRoot: true,
	}
	if oldRoot != newRoot {
		removeNames[newRoot] = true
	}
	for name, body := range m.markdownVersions {
		if body == nil || body.Len() == 0 {
			continue
		}
		entryName := filepath.ToSlash(strings.TrimSpace(name))
		if path.Ext(entryName) == "" {
			entryName += ".md"
		}
		if !strings.HasPrefix(entryName, "versions/") {
			entryName = filepath.ToSlash(filepath.Join("versions", entryName))
		}
		replacements[entryName] = zipReplacement{Body: append([]byte(nil), body.Bytes()...)}
		removeNames[entryName] = true
	}

	if err := patchZipFile(archivepath, removeNames, replacements); err != nil {
		return err
	}
	m.zipPath = archivepath
	m.zipMarkdownName = newRoot
	return nil
}

func patchZipFile(srcPath string, removeNames map[string]bool, replacements map[string]zipReplacement) error {
	srcPath = strings.TrimSpace(srcPath)
	if srcPath == "" {
		return fmt.Errorf("archive path is empty")
	}

	reader, err := zip.OpenReader(srcPath)
	if err != nil {
		return err
	}

	dir := filepath.Dir(srcPath)
	base := filepath.Base(srcPath)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		_ = reader.Close()
		return err
	}
	tmpPath := tmp.Name()

	writeErr := func() error {
		zw := zip.NewWriter(tmp)
		written := make(map[string]bool)

		for _, file := range reader.File {
			name := markdownZipCleanEntryName(file.Name)
			if name == "" || removeNames[name] {
				continue
			}
			if _, replacing := replacements[name]; replacing {
				continue
			}
			if written[name] {
				continue
			}
			if err := markdownCopyZipEntry(zw, file, name); err != nil {
				_ = zw.Close()
				return err
			}
			written[name] = true
		}

		names := make([]string, 0, len(replacements))
		for name := range replacements {
			clean := markdownZipCleanEntryName(name)
			if clean != "" {
				names = append(names, clean)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if written[name] {
				continue
			}
			replacement := replacements[name]
			if err := markdownWriteReplacementZipEntry(zw, name, replacement); err != nil {
				_ = zw.Close()
				return err
			}
			written[name] = true
		}

		return zw.Close()
	}()

	closeTmpErr := tmp.Close()
	closeReaderErr := reader.Close()
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return writeErr
	}
	if closeTmpErr != nil {
		_ = os.Remove(tmpPath)
		return closeTmpErr
	}
	if closeReaderErr != nil {
		_ = os.Remove(tmpPath)
		return closeReaderErr
	}

	if err := markdownReplaceFile(tmpPath, srcPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func markdownCopyZipEntry(writer *zip.Writer, file *zip.File, name string) error {
	header := file.FileHeader
	header.Name = name
	if !file.FileInfo().IsDir() {
		header.Method = zip.Deflate
	}
	header.CRC32 = 0
	header.CompressedSize = 0
	header.CompressedSize64 = 0
	header.UncompressedSize = 0
	header.UncompressedSize64 = 0

	entryWriter, err := writer.CreateHeader(&header)
	if err != nil {
		return err
	}
	if file.FileInfo().IsDir() {
		return nil
	}

	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(entryWriter, rc)
	return err
}

func markdownWriteReplacementZipEntry(writer *zip.Writer, name string, replacement zipReplacement) error {
	header := replacement.Header
	if header == nil {
		header = &zip.FileHeader{
			Name:   name,
			Method: zip.Deflate,
		}
		header.SetModTime(time.Now())
	} else {
		copied := *header
		header = &copied
		header.Name = name
	}

	entryWriter, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = bytes.NewReader(replacement.Body).WriteTo(entryWriter)
	return err
}

func markdownReplaceFile(tmpPath, dstPath string) error {
	id, err := markdownUUID()
	if err != nil {
		return err
	}
	backupPath := fmt.Sprintf("%s.bak.%d.%s", dstPath, os.Getpid(), id)
	if err := os.Rename(dstPath, backupPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Rename(backupPath, dstPath)
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	return nil
}

func markdownVersionArchiveEntryName(version string) string {
	version = mdNormalizeVersion(version)
	if version == "" {
		version = "unknown"
	}
	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	return fmt.Sprintf("versions/%s_v%s.md", timestamp, version)
}

func markdownRootNameInZip(archivepath string) (string, error) {
	reader, err := zip.OpenReader(archivepath)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var roots []string
	for _, file := range reader.File {
		name := markdownZipCleanEntryName(file.Name)
		if name != "" && !file.FileInfo().IsDir() && markdownZipIsRootMarkdown(name) {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		return "", fmt.Errorf("zip does not contain a root markdown file")
	}
	if len(roots) > 1 {
		return "", fmt.Errorf("zip contains multiple root markdown files: %s", strings.Join(roots, ", "))
	}
	return roots[0], nil
}
