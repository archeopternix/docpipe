package tools

import (
	"regexp"
	"strings"
)

var (
	anchorSpaceRe  = regexp.MustCompile(`\s+`)
	anchorKeepRe   = regexp.MustCompile(`[^a-z0-9\-_]+`)
	anchorHyphenRe = regexp.MustCompile(`-+`)
)

// AnchorFromHeadline converts a markdown headline into a deterministic anchor tag.
//
// Notes:
//   - This is a "good enough" GitHub-ish slug.
//   - If you need exact compatibility with your HTML renderer's heading IDs
//     (including Unicode handling and duplicate disambiguation), adapt accordingly.
func AnchorFromHeadline(headline string) string {
	s := strings.TrimSpace(headline)

	// Some markdown allows closing hashes: "Title ###"
	s = strings.TrimRight(s, "#")
	s = strings.TrimSpace(s)

	s = strings.ToLower(s)
	s = anchorSpaceRe.ReplaceAllString(s, "-")
	s = anchorKeepRe.ReplaceAllString(s, "")
	s = anchorHyphenRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	return s
}

func NormalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// normalize tabs to single spaces to avoid weird Markdown indentation
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}

// cleanHeadingText removes trailing hashes like "Title ###" and trims spaces.
func CleanHeadingText(s string) string {
	s = strings.TrimSpace(s)
	// Strip trailing " ###" style closing markers sometimes used in Markdown.
	s = strings.TrimRight(s, "#")
	return strings.TrimSpace(s)
}

// Slugify creates an anchor-like slug from heading text.
// This is "good enough" and deterministic; if you already have a slugger in docpipe,
// replace this with that to match your renderer's anchor algorithm.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	// Replace whitespace with single hyphen.
	spaceRe := regexp.MustCompile(`\s+`)
	s = spaceRe.ReplaceAllString(s, "-")

	// Remove characters that are not alnum, hyphen, underscore.
	keepRe := regexp.MustCompile(`[^a-z0-9\-_]+`)
	s = keepRe.ReplaceAllString(s, "")

	// Collapse multiple hyphens.
	hyphenRe := regexp.MustCompile(`-+`)
	s = hyphenRe.ReplaceAllString(s, "-")

	return strings.Trim(s, "-")
}
