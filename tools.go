package docpipe

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

// SplitMarkdownToSearchSections splits markdown into sections by h1..h3 headings.
// It returns sections suitable for indexing and for deep-link navigation.
//
// Rules:
// - Headings are lines matching: ^(#{1,3})\s+(.+)$
// - AnchorTag is slugified from heading text
// - Content is the markdown content until the next h1..h3 heading
// - If markdown has no h1..h3 headings, returns a single section with Title "Content"
func SplitMarkdownToSearchSections(markdown string) []SearchSection {
	normalized := normalizeNewlines(markdown)
	lines := strings.Split(normalized, "\n")

	headingRe := regexp.MustCompile(`^(#{1,3})\s+(.+?)\s*$`)

	type sec struct {
		title  string
		level  int
		anchor string
		lines  []string
	}

	var (
		introLines []string
		current    *sec
		out        []SearchSection
		foundAny   bool
	)

	flushCurrent := func() {
		if current == nil {
			return
		}
		content := strings.TrimSpace(strings.Join(current.lines, "\n"))
		out = append(out, SearchSection{
			Title:     current.title,
			AnchorTag: current.anchor,
			Content:   content,
		})
		current = nil
	}

	flushIntro := func() {
		intro := strings.TrimSpace(strings.Join(introLines, "\n"))
		if intro == "" {
			return
		}
		out = append(out, SearchSection{
			Title:     "Introduction",
			AnchorTag: "introduction",
			Content:   intro,
		})
		introLines = nil
	}

	for _, line := range lines {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			// New h1..h3 starts here.
			if current != nil {
				flushCurrent()
			} else if !foundAny {
				flushIntro()
			}
			foundAny = true

			level := len(m[1])
			title := cleanHeadingText(m[2])
			anchor := Slugify(title)

			current = &sec{
				title:  title,
				level:  level,
				anchor: anchor,
				lines:  nil,
			}
			continue
		}

		if !foundAny && current == nil {
			introLines = append(introLines, line)
			continue
		}
		if current == nil {
			// Shouldn't happen because foundAny implies we created current at a heading,
			// but keep safe: treat as intro.
			introLines = append(introLines, line)
			continue
		}
		current.lines = append(current.lines, line)
	}

	if current != nil {
		flushCurrent()
	} else if !foundAny {
		// No headings at all → one section.
		content := strings.TrimSpace(normalized)
		if content == "" {
			return []SearchSection{}
		}
		return []SearchSection{{
			Title:     "Content",
			AnchorTag: "",
			Content:   content,
		}}
	}

	// If intro existed and first heading appeared later, it was flushed as "Introduction".
	// If intro exists but headings never occurred, we returned "Content" above.
	return dropEmptySections(out)
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// cleanHeadingText removes trailing hashes like "Title ###" and trims spaces.
func cleanHeadingText(s string) string {
	s = strings.TrimSpace(s)
	// Strip trailing " ###" style closing markers sometimes used in Markdown.
	s = strings.TrimRight(s, "#")
	return strings.TrimSpace(s)
}

func dropEmptySections(in []SearchSection) []SearchSection {
	out := make([]SearchSection, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s.Title) == "" && strings.TrimSpace(s.Content) == "" {
			continue
		}
		out = append(out, s)
	}
	return out
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
