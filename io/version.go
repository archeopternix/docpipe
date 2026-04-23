package io

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const markdownVersionMaxSuffix = 1000

var (
	markdownVersionFilePattern = regexp.MustCompile(`(?i)^(.*)_([a-z]{2})_v(\d+(?:\.\d+)*)$`)
	markdownVersionPattern     = regexp.MustCompile(`^\d+(?:\.\d+)*$`)
)

// ParseFileNameFromMetaData reads version/language information from
// metadata only.
func ParseFileNameFromMetaData(meta *MetaData) string {
	if meta == nil {
		return "document_EN_v1.0.md"
	}

	baseStem := strings.TrimSpace(strings.TrimSpace(meta.Title))
	if baseStem == "" {
		baseStem = "document"
	}

	language := normalizeVersionLanguage(meta.Language)
	if language == "" {
		language = "EN"
	}

	version := normalizeMarkdownVersion(meta.Version)
	if version == "" {
		version = "1.0"
	}

	return fmt.Sprintf("%s_%s_v%s.md", baseStem, language, version)
}

// ParseLanguageFromFile reads language information from fileName.
func parseLanguageFromFile(path string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if stem == "" {
		return ""
	}

	matches := markdownVersionFilePattern.FindStringSubmatch(stem)
	if len(matches) != 4 {
		return ""
	}

	return normalizeVersionLanguage(matches[2])
}

// ParseVersionFromFile reads version information from fileName.
func parseVersionFromFile(path string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if stem == "" {
		return ""
	}

	matches := markdownVersionFilePattern.FindStringSubmatch(stem)
	if len(matches) != 4 {
		return ""
	}

	return normalizeMarkdownVersion(matches[3])
}

func normalizeMarkdownVersion(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(strings.TrimPrefix(normalized, "v"), "V")
	if !markdownVersionPattern.MatchString(normalized) {
		return ""
	}
	return normalized
}

func normalizeVersionLanguage(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "en"
	}

	switch normalized {
	case "english":
		return "en"
	case "german", "deutsch":
		return "de"
	}

	normalized = strings.ReplaceAll(normalized, "_", "-")
	if idx := strings.IndexByte(normalized, '-'); idx >= 0 {
		normalized = normalized[:idx]
	}
	if len(normalized) > 2 {
		normalized = normalized[:2]
	}
	if len(normalized) != 2 {
		return "en"
	}
	for _, r := range normalized {
		if r < 'a' || r > 'z' {
			return "en"
		}
	}
	return normalized
}

/*
func filenameVersionLanguage(value string) string {
	if normalized := normalizeVersionLanguage(value); normalized != "" {
		return strings.ToUpper(normalized)
	}
	return ""
}
*/

/*
// MarkdownVersionBaseStem returns the filename without the `_LANG_vVERSION`
// suffix when present.
func MarkdownVersionBaseStem(path string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if stem == "" {
		return ""
	}
	matches := markdownVersionFilePattern.FindStringSubmatch(stem)
	if len(matches) == 4 {
		return strings.TrimSpace(matches[1])
	}
	return stem
}
*/
/*
// NormalizeMarkdownVersion normalizes version strings like `v1.0` to `1.0`.
func NormalizeMarkdownVersion(value string) string {
	return normalizeMarkdownVersion(value)
}

// NormalizeMarkdownVersionLanguage normalizes language values such as `EN`,
// `english`, or `de-DE` to a lowercase two-letter code.
func NormalizeMarkdownVersionLanguage(value string) string {
	return normalizeVersionLanguage(value)
}


// UniqueVersionedPath returns a collision-free target path using `_vN`
// suffixes.
func UniqueVersionedPath(target string) string {
	if !fileExists(target) {
		return target
	}

	dir := filepath.Dir(target)
	stem := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	ext := filepath.Ext(target)

	for i := 0; i <= markdownVersionMaxSuffix; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s_v%d%s", stem, i, ext))
		if !fileExists(candidate) {
			return candidate
		}
	}

	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, time.Now().UnixNano(), ext))
}


func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
*/
