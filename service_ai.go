package docpipe

import (
	"context"
	"fmt"
	"strings"

	"docpipe/ai"
	"docpipe/clean"
)

func (s Service) Clean(ctx context.Context, doc Document, opt clean.Options, upd UpdateOptions) error {
	root, err := s.ReadMarkdown(ctx, doc)
	if err != nil {
		return err
	}

	fm, err := ParseFrontmatter(root)
	if err != nil {
		return err
	}
	text := clean.Normalize(StripFrontmatter(root), opt)
	return s.WriteMarkdown(ctx, doc, mdComposeMarkdownWithMeta(fm, text), upd)
}

func (s Service) Translate(ctx context.Context, doc Document, client ai.Client, targetLang string, rephrase bool, upd UpdateOptions) error {
	if client == nil {
		return fmt.Errorf("%w: ai client is nil", ErrAIUnavailable)
	}
	if strings.TrimSpace(targetLang) == "" {
		return fmt.Errorf("%w: target language is missing", ErrInvalidInput)
	}

	root, err := s.ReadMarkdown(ctx, doc)
	if err != nil {
		return err
	}

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

	currentFM, err := ParseFrontmatter(root)
	if err != nil {
		return err
	}
	translatedFM, err := ParseFrontmatter(translated)
	if err != nil {
		return err
	}
	fm := mdMergeFrontmatter(translatedFM, currentFM)
	fm.Language = mdNormalizeLanguageCode(targetLang)
	return s.WriteMarkdown(ctx, doc, mdComposeMarkdownWithMeta(fm, StripFrontmatter(translated)), upd)
}

func (s Service) DetectLanguage(ctx context.Context, doc Document, client ai.Client) (string, error) {
	if client == nil {
		return "", fmt.Errorf("%w: ai client is nil", ErrAIUnavailable)
	}
	root, err := s.ReadMarkdown(ctx, doc)
	if err != nil {
		return "", err
	}
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
