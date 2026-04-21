package processor

import (
	"bytes"
	"context"
)

type TranslateFunc func(ctx context.Context, in *bytes.Buffer, sourceLang, targetLang string) (*bytes.Buffer, error)

type Translator struct {
	translate TranslateFunc
}

func NewTranslator(translate TranslateFunc, targetLang string) *Translator {
	return &Translator{
		translate: translate,
	}
}

func (p *Translator) Name() string {
	return "translator"
}

func (p *Translator) Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var ok bool
	var sourceLang, targetLang string

	if sourceLang, ok = params.Parameters["detected_lang"]; !ok {
		return nil, ErrLangDetectionFailed
	}
	if targetLang, ok = params.Parameters["target_lang"]; !ok {
		return nil, ErrParameterMissing
	}

	return p.translate(ctx, cloneBuffer(in), sourceLang, targetLang)
}
