package search

import (
	"regexp"
	"strings"

	tools "github.com/archeopternix/docpipe/internal/tools"
)

// SplitMarkdownToSearchSections splits markdown into sections by h1..h3 headings.
// It returns sections suitable for indexing and for deep-link navigation.
//
// Rules:
// - Headings are lines matching: ^(#{1,3})\s+(.+)$
// - AnchorTag is slugified from heading text
// - Content is the markdown content until the next h1..h3 heading
// - If markdown has no h1..h3 headings, returns a single section with Title "Content"
func SplitMarkdownToSearchSections(markdown string) []SearchSection {
	normalized := tools.NormalizeNewlines(markdown)
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
			title := tools.CleanHeadingText(m[2])
			anchor := tools.Slugify(title)

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
