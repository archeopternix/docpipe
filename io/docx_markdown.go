package io

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ConvertDocxToMarkdownZip converts one DOCX source into a ZIP containing the
// generated markdown at the ZIP root, extracted media under media/, and the
// original DOCX under document/.
func convertDocxToMarkdown(path string, docs *Documents) error {

	mediaDir, err := os.MkdirTemp("", "docx-media-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(mediaDir) }()

	args := []string{
		path,
		"-t", "gfm",
		"--wrap=none",
		"--extract-media=" + mediaDir,
	}

	cmd := exec.Command("pandoc", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	body, err := cmd.Output()
	if err != nil {
		fmt.Printf("pandoc stderr: %s", strings.TrimSpace(stderr.String()))

		if stderr.Len() > 0 {
			return fmt.Errorf("pandoc failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return err
	}

	// Collect extracted media files to include in /media
	files, err := collectExtractedMediaFiles(mediaDir)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		for k, v := range files {
			docs.ExtractedImages[k] = bytes.NewBuffer(v)
		}
	}

	// Process markdown content
	text := CleanupMarkdownContent(string(body))
	text = ApplyMetaDataFrontmatter(text, &docs.MetaData)
	docs.MarkdownFile = bytes.NewBufferString(text)

	return nil
}

func stem(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func collectExtractedMediaFiles(mediaDir string) (map[string][]byte, error) {
	files := map[string][]byte{}

	if err := filepath.Walk(mediaDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(mediaDir, path)
		if err != nil {
			return err
		}
		relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "media/")

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(filepath.Join("media", relPath))] = body
		return nil
	}); err != nil {
		return nil, err
	}

	return files, nil
}

/*
func normalizeZipImagePath(pathValue, mediaDir string) string {
	pathValue = strings.TrimSpace(strings.Trim(pathValue, `"'`))
	pathValue = strings.ReplaceAll(pathValue, "\\", "/")
	pathValue = strings.ReplaceAll(pathValue, filepath.ToSlash(mediaDir)+"/media/", "/media/")
	pathValue = strings.ReplaceAll(pathValue, filepath.ToSlash(mediaDir)+"/", "/media/")
	if strings.Contains(pathValue, "/media/") {
		return "/media/" + filepath.Base(pathValue)
	}
	if strings.HasPrefix(pathValue, "media/") {
		return "/" + pathValue
	}
	if strings.HasPrefix(pathValue, "/media/") {
		return pathValue
	}
	return "/media/" + filepath.Base(pathValue)
}

func WriteZipToBuffer(files map[string][]byte) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, filepath.ToSlash(name))
	}
	sort.Strings(names)

	for _, name := range names {
		header := &zip.FileHeader{
			Name:   name,
			Method: zip.Deflate,
		}
		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		if _, err := bytes.NewReader(files[name]).WriteTo(entryWriter); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}
*/
