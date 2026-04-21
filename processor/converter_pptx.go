package processor

import (
	"bytes"
	"context"
)

type PptxConverterFunc func(ctx context.Context, in *bytes.Buffer) (*bytes.Buffer, error)

type PptxConverter struct {
	pptx PptxConverterFunc
}

func NewPptxConverter(pptx PptxConverterFunc) *PptxConverter {
	return &PptxConverter{
		pptx: pptx,
	}
}

func (c *PptxConverter) Convert(ctx context.Context, src *bytes.Buffer) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, ErrPptxNilSource
	}

	return c.pptx(ctx, src)
}

func (c *PptxConverter) Name() string {
	return "pptx file converter"
}
