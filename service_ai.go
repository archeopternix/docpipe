package docpipe

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/archeopternix/docpipe/ai"
	tools "github.com/archeopternix/docpipe/internal/tools"
)

func (s Service) Clean(ctx context.Context, doc Document, upd UpdateOptions) error {

	parts, err := s.ReadMarkdownParts(ctx, doc)
	if err != nil {
		return err
	}

	optmized := txtToMarkdown(parts.Body)

	return s.WriteMarkdown(ctx, doc, mdComposeMarkdownWithMeta(parts.Frontmatter, optmized), upd)
}

// txtToMarkdown formats unstructured text into structured Markdown.
// Heuristics:
// - First non-empty line becomes H1 (unless it already looks like a heading)
// - Lines that look like "Section:" become H2
// - Lines that look like "Subsection - ..." become H3
// - Lines that look like list items become "- ..."
// - Ensures blank lines around headings and list blocks
func txtToMarkdown(input string) string {
	s := tools.NormalizeNewlines(input)
	lines := splitAndTrimRight(s)

	// Remove leading/trailing empty lines
	lines = trimEmptyEdges(lines)

	// If empty -> empty
	if len(lines) == 0 {
		return ""
	}

	// Promote first line to H1 if it doesn't already look like a heading
	firstIdx := firstNonEmpty(lines)
	if firstIdx >= 0 && !looksLikeHeading(lines[firstIdx]) {
		lines[firstIdx] = "# " + cleanupInline(lines[firstIdx])
	}

	var out []string
	inList := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)

		// Preserve paragraph breaks
		if line == "" {
			// Close list block with a blank line
			if inList {
				out = append(out, "")
				inList = false
			} else if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}

		// Headings
		if h, ok := toHeading(line); ok {
			// ensure blank line before heading (unless start)
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			// close list
			inList = false

			out = append(out, h)
			out = append(out, "") // blank line after heading
			continue
		}

		// Bullets
		if b, ok := toBullet(line); ok {
			// ensure blank line before list block
			if len(out) > 0 && out[len(out)-1] != "" && !inList {
				out = append(out, "")
			}
			out = append(out, b)
			inList = true
			continue
		}

		// Normal paragraph line: if we were in a list, add a blank line first
		if inList {
			out = append(out, "")
			inList = false
		}

		out = append(out, cleanupInline(line))
	}

	// Clean up trailing blanks
	out = trimTrailingEmpty(out)

	// Collapse excessive blank lines to max 1
	return collapseBlankLines(strings.Join(out, "\n"))
}

/* ------------------------- helpers ------------------------- */

func splitAndTrimRight(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

func trimEmptyEdges(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines) - 1
	for end >= start && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	if start > end {
		return nil
	}
	return lines[start : end+1]
}

func firstNonEmpty(lines []string) int {
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			return i
		}
	}
	return -1
}

func looksLikeHeading(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "# ")
}

var (
	reHeading2 = regexp.MustCompile(`(?i)^(section|overview|summary|details|notes|background|problem|solution|architecture|design|api|usage|examples|faq|next steps)\s*:\s*(.+)$`)
	reHeading3 = regexp.MustCompile(`(?i)^([a-z0-9][a-z0-9 /_-]{1,60})\s*[-–—]\s*(.+)$`)
)

func toHeading(line string) (string, bool) {
	trim := cleanupInline(line)
	// Already Markdown heading => keep (but normalize spacing)
	if strings.HasPrefix(trim, "###") || strings.HasPrefix(trim, "##") || strings.HasPrefix(trim, "#") {
		return normalizeHeading(trim), true
	}

	// "Section: Title" => H2
	if m := reHeading2.FindStringSubmatch(trim); m != nil {
		return "## " + cleanupInline(m[2]), true
	}
	// "Subtopic - Title" => H3
	if m := reHeading3.FindStringSubmatch(trim); m != nil {
		// avoid turning normal sentences into headings by requiring short-ish LHS
		left := strings.TrimSpace(m[1])
		if len(left) <= 30 {
			return "### " + cleanupInline(m[2]), true
		}
	}

	// Lines that look like titles (short, no trailing period) => H2
	if looksLikeTitle(trim) {
		return "## " + trim, true
	}
	return "", false
}

func normalizeHeading(h string) string {
	// Ensure exactly one space after leading #'s
	h = strings.TrimSpace(h)
	i := 0
	for i < len(h) && h[i] == '#' {
		i++
	}
	if i == 0 {
		return h
	}
	level := i
	if level > 3 {
		level = 3
	}
	text := strings.TrimSpace(h[i:])
	if text == "" {
		text = "Untitled"
	}
	return strings.Repeat("#", level) + " " + text
}

func looksLikeTitle(s string) bool {
	if len(s) < 3 || len(s) > 60 {
		return false
	}
	// avoid obvious sentences
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "?") || strings.HasSuffix(s, "!") {
		return false
	}
	// avoid "key: value" lines (probably content)
	if strings.Contains(s, ":") {
		return false
	}
	// title-ish if few words and mostly letters/digits/spaces
	words := strings.Fields(s)
	if len(words) >= 2 && len(words) <= 6 {
		return true
	}
	return false
}

var (
	reBullet = regexp.MustCompile(`^(\*|-|•|\d+\.)\s+(.+)$`)
)

func toBullet(line string) (string, bool) {
	m := reBullet.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	item := cleanupInline(m[2])
	if item == "" {
		return "", false
	}
	return "- " + item, true
}

func cleanupInline(s string) string {
	s = strings.TrimSpace(s)
	// collapse multiple spaces
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	// remove trailing ":" for pseudo-headings
	s = strings.TrimSuffix(s, ":")
	return s
}

func trimTrailingEmpty(lines []string) []string {
	i := len(lines) - 1
	for i >= 0 && strings.TrimSpace(lines[i]) == "" {
		i--
	}
	return lines[:i+1]
}

func collapseBlankLines(s string) string {
	// Reduce 3+ newlines to exactly 2 (one blank line)
	re := regexp.MustCompile(`\n{3,}`)
	return re.ReplaceAllString(s, "\n\n")
}

func (s Service) Translate(ctx context.Context, doc Document, client ai.Client, targetLang string, rephrase bool, upd UpdateOptions) error {
	if client == nil {
		return fmt.Errorf("%w: ai client is nil", ErrAIUnavailable)
	}
	if strings.TrimSpace(targetLang) == "" {
		return fmt.Errorf("%w: target language is missing", ErrInvalidInput)
	}

	parts, err := s.ReadMarkdownParts(ctx, doc)
	if err != nil {
		return err
	}
	root := parts.Full

	instruction := fmt.Sprintf(
		`Translate this markdown document to %s. Preserve markdown structure, YAML frontmatter keys, links, image paths, code blocks, tables, and all factual content. Return only the translated markdown.`,
		strings.ToLower(strings.TrimSpace(targetLang)),
	)
	if rephrase {
		instruction = fmt.Sprintf(
			`Translate this markdown document to %s. Use natural idiomatic phrasing in the target language while preserving meaning, markdown structure, YAML frontmatter keys, links, image paths, code blocks, tables, and all factual content. Return only the translated markdown.`,
			strings.ToLower(strings.TrimSpace(targetLang)),
		)
	}

	translated, err := client.Generate(ctx, instruction, root)
	if err != nil {
		return err
	}

	translatedFM, err := parseFrontmatter(translated)
	if err != nil {
		return err
	}
	fm := mdMergeFrontmatter(translatedFM, parts.Frontmatter)
	fm.Language = mdNormalizeLanguageCode(targetLang)
	return s.WriteMarkdown(ctx, doc, mdComposeMarkdownWithMeta(fm, stripFrontmatter(translated)), upd)
}

func (s Service) DetectLanguage(ctx context.Context, doc Document, client ai.Client) (string, error) {
	if client == nil {
		return "", fmt.Errorf("%w: ai client is nil", ErrAIUnavailable)
	}

	parts, err := s.ReadMarkdownParts(ctx, doc)
	if err != nil {
		return "", err
	}
	root := parts.Full

	text, err := client.Generate(ctx,
		`Detect the primary language of the markdown document. Return only the ISO 639-1 language code in lowercase, for example "de" or "en".`,
		root,
	)
	if err != nil {
		return "", err
	}
	code := strings.ToLower(strings.Trim(strings.TrimSpace(text), "`'\""))
	if len(code) < 2 {
		return "", fmt.Errorf("%w: ai client returned invalid language code %q", ErrAIUnavailable, text)
	}
	if idx := strings.IndexAny(code, " \n\r\t.,;:"); idx >= 0 {
		code = code[:idx]
	}
	if len(code) != 2 {
		return "", fmt.Errorf("%w: ai client returned invalid language code %q", ErrAIUnavailable, text)
	}
	for _, r := range code {
		if r < 'a' || r > 'z' {
			return "", fmt.Errorf("%w: ai client returned invalid language code %q", ErrAIUnavailable, text)
		}
	}
	return code, nil
}
