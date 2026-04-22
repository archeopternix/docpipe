package processor

import (
	"bytes"
	"context"
)

type TransformerFunc func(ctx context.Context, in *bytes.Buffer) (*bytes.Buffer, error)

type Transformer struct {
	translate TransformerFunc
}

func NewTransformer(translate TransformerFunc) *Transformer {
	return &Transformer{
		translate: translate,
	}
}

func (p *Transformer) Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return p.translate(ctx, in)
}

func (p *Transformer) Name() string {
	return "markdown transformer"
}
