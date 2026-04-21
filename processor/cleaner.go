package processor

import (
	"bytes"
	"context"
)

type CleanFunc func(ctx context.Context, in *bytes.Buffer) (*bytes.Buffer, error)

type Cleaner struct {
	rules []CleanFunc
}

func NewCleaner(rules ...CleanFunc) *Cleaner {
	return &Cleaner{rules: append([]CleanFunc(nil), rules...)}
}

func (p *Cleaner) Name() string {
	return "cleaner"
}

func (p *Cleaner) Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	current := cloneBuffer(in)
	for _, rule := range p.rules {
		if rule == nil {
			continue
		}
		next, err := rule(ctx, current)
		if err != nil {
			return current, err
		}
		current = next
	}

	return current, nil
}
