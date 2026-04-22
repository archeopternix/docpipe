package io

import (
	"os"
	"path/filepath"
	"strings"
)

func ParseMarkdownFileName(name string) MarkdownFileNameParts {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if stem == "" {
		return MarkdownFileNameParts{}
	}

	matches := markdownFilenamePattern.FindStringSubmatch(stem)
	if len(matches) != 4 {
		return MarkdownFileNameParts{BaseStem: stem}
	}

	return MarkdownFileNameParts{
		BaseStem: strings.TrimSpace(matches[1]),
		Language: NormalizeLanguageCode(matches[2]),
		Version:  NormalizeVersion(matches[3]),
		Matched:  true,
	}
}

func MarkdownBaseStem(name string) string {
	parts := ParseMarkdownFileName(name)
	if strings.TrimSpace(parts.BaseStem) != "" {
		return parts.BaseStem
	}
	return strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
}

// Exists reports whether the path currently exists on disk.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func NormalizeExtension(ext string) string {
	if ext == "" {
		return ""
	}
	normalized := strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(normalized, ".") {
		normalized = "." + normalized
	}
	return normalized
}

func FilepathDir(path string) string {
	lastSlash := strings.LastIndexAny(path, `/\`)
	if lastSlash < 0 {
		return "."
	}
	if lastSlash == 0 {
		return path[:1]
	}
	return path[:lastSlash]
}
