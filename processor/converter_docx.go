package processor

import (
	"bytes"
	"context"
)

type DocxConverterFunc func(ctx context.Context, in *bytes.Buffer) (*bytes.Buffer, error)

type DocxConverter struct {
	docx DocxConverterFunc
}

func NewDocxConverter(docx DocxConverterFunc) *DocxConverter {
	return &DocxConverter{
		docx: docx,
	}
}

func (c *DocxConverter) Convert(ctx context.Context, src *bytes.Buffer) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, ErrDocxNilSource
	}

	return c.docx(ctx, src)
}

func (c *DocxConverter) Name() string {
	return "docx file converter"
}
