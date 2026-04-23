package processor

import (
	"bytes"
	"context"
	"docpipe/io"
)

type DetectFunc func(ctx context.Context, in *bytes.Buffer) (string, error)

type LangDetector struct {
	detect DetectFunc
}

func NewLangDetector(detect DetectFunc) *LangDetector {
	return &LangDetector{detect: detect}
}

func (p *LangDetector) Process(ctx context.Context, docs *io.Documents, params *PipelineParameters) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sourcelang := docs.MetaData.Language

	if sourcelang != "" {
		params.MetaData.Language = sourcelang
		return cloneBuffer(in), nil
	}

	lang, err := p.detect(ctx, in)
	if err != nil {
		return cloneBuffer(in), ErrLangDetectionFailed
	}

	params.MetaData.Language = lang
	return cloneBuffer(in), nil
}

func (p *LangDetector) Name() string {
	return "language detection"
}
