package processor

import (
	"bytes"
	"context"
)

type DetectFunc func(ctx context.Context, in *bytes.Buffer) (string, error)

type LangDetector struct {
	detect                  DetectFunc
	continueOnDetectFailure bool
}

func NewLangDetector(detect ...DetectFunc) *LangDetector {
	var selected DetectFunc
	if len(detect) > 0 {
		selected = detect[0]
	}
	return &LangDetector{detect: selected}
}

func (p *LangDetector) Name() string {
	return "langdetect"
}

func (p *LangDetector) Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if params == nil {
		return cloneBuffer(in), nil
	}

	if p.detect == nil {
		return cloneBuffer(in), nil
	}

	lang, err := p.detect(ctx, in)
	if err != nil {
		if p.continueOnDetectFailure {
			return cloneBuffer(in), nil
		}
		return cloneBuffer(in), ErrLangDetectionFailed
	}

	params.Parameters["detected_lang"] = lang
	return cloneBuffer(in), nil
}
