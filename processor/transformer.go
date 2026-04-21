package processor

import (
	"bytes"
	"context"
)

type Transformer struct {
}

func NewTransformer() *Transformer {
	return &Transformer{}
}

func (p *Transformer) Name() string {
	return "transformer"
}

func (p *Transformer) Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return cloneBuffer(in), nil
}
