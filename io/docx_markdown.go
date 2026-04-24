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
	docs.MarkdownFile = bytes.NewBufferString(CleanupMarkdownContent(string(body)))
	docs.ApplyMetaDataFrontmatter()

	return nil
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
