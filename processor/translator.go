package processor

import (
	"bytes"
	"context"
)

type TranslateFunc func(ctx context.Context, in *bytes.Buffer, targetLang string) (*bytes.Buffer, error)

type Translator struct {
	translate TranslateFunc
}

func NewTranslator(translate TranslateFunc, targetLang string) *Translator {
	return &Translator{
		translate: translate,
	}
}

func (p *Translator) Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if params.Config.TargetLanguage == "" {
		return nil, ErrTargetLangMissing
	}

	return p.translate(ctx, in, params.Config.TargetLanguage)
}

func (p *Translator) Name() string {
	return "markdown language translator"
}
