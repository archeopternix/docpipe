package docpipe

import (
	"context"
)

// RenderHTML renders a document's markdown as HTML.
// Parameters: opt controls rendering (e.g. section splitting, heading anchors). Returns rendered HTML fragments.
func (s Service) RenderHTML(ctx context.Context, doc Document, opt RenderOptions) (Rendered, error) {

	parts, err := s.ReadMarkdownParts(ctx, doc)
	if err != nil {
		return Rendered{}, err
	}

	fm := parts.Frontmatter

	bodyHTML, err := markdownRenderHTML(parts.Body, opt)
	if err != nil {
		return Rendered{}, err
	}
	rendered := Rendered{BodyHTML: bodyHTML}
	if opt.SplitSections {
		rendered.TitleHTML = markdownRenderTitleHTML(fm.Title, opt.AnchorifyHeadings)
		rendered.FrontmatterHTML = markdownRenderFrontmatterHTML(fm)
	}
	return rendered, nil
}

// HeadingIndex extracts headings from the document body and returns a nested index tree.
// Parameter: maxLevel limits headings (defaults to 3; clamped to 1..6).
func (s Service) HeadingIndex(ctx context.Context, doc Document, maxLevel int) ([]HeadingNode, error) {
	parts, err := s.ReadMarkdownParts(ctx, doc)
	if err != nil {
		return nil, err
	}

	body := parts.Body
	if maxLevel <= 0 {
		maxLevel = 3
	}
	if maxLevel > 6 {
		maxLevel = 6
	}

	type treeNode struct {
		Level    int
		Text     string
		AnchorID string
		Children []*treeNode
	}

	var roots []*treeNode
	var stack []*treeNode
	anchors := newMarkdownAnchorGenerator()
	for _, heading := range markdownExtractHeadings(body, maxLevel) {
		node := &treeNode{
			Level:    heading.Level,
			Text:     heading.Text,
			AnchorID: anchors.Anchor(heading.Text),
		}
		for len(stack) > 0 && stack[len(stack)-1].Level >= node.Level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, node)
		} else {
			stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, node)
		}
		stack = append(stack, node)
	}

	var convert func(*treeNode) HeadingNode
	convert = func(node *treeNode) HeadingNode {
		out := HeadingNode{
			Level:    node.Level,
			Text:     node.Text,
			AnchorID: node.AnchorID,
		}
		for _, child := range node.Children {
			out.Children = append(out.Children, convert(child))
		}
		return out
	}

	index := make([]HeadingNode, 0, len(roots))
	for _, root := range roots {
		index = append(index, convert(root))
	}
	return index, nil
}
