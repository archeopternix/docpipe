package processor

import (
	"bytes"
	"context"
)

type MarkdownConverterFunc func(ctx context.Context, in *bytes.Buffer) (*bytes.Buffer, error)

type MarkdownConverter struct {
	markdown MarkdownConverterFunc
}

func NewMarkdownConverter(markdown MarkdownConverterFunc) *MarkdownConverter {
	return &MarkdownConverter{
		markdown: markdown,
	}
}

func (c *MarkdownConverter) Convert(ctx context.Context, src *bytes.Buffer) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, ErrMarkdownNilSource
	}

	return bytes.NewBuffer(append([]byte(nil), src.Bytes()...)), nil
}

func (c *MarkdownConverter) Name() string {
	return "markdown file converter"
}
