package processor

import (
	"bytes"
	"context"
)

type TextConverterFunc func(ctx context.Context, in *bytes.Buffer) (*bytes.Buffer, error)

type TextConverter struct {
	text TextConverterFunc
}

func NewTextConverter(text TextConverterFunc) *TextConverter {
	return &TextConverter{
		text: text,
	}
}

func (c *TextConverter) Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in == nil {
		return nil, ErrTextNilSource
	}

	return c.text(ctx, in)
}

func (c *TextConverter) Name() string {
	return "text file converter"
}
