package processor

import (
	"bytes"
	"context"
	"docpipe/io"
	"time"
)

type Processor interface {
	Name() string
	Process(ctx context.Context, documents *io.Documents, params *PipelineParameters) (StepResult, error)
}

// Config holds the configuration for the processing pipeline.
type Config struct {
	TargetLanguage    string
	IncludeImages     bool
	IncludeSourceFile bool
}

type PipelineParameters struct {
	DocumentFormat Format
	Config
}

type StepResult struct {
	Name      string
	Duration  time.Duration
	ErrorText string
}

func cloneBuffer(src *bytes.Buffer) *bytes.Buffer {
	if src == nil {
		return bytes.NewBuffer(nil)
	}
	return bytes.NewBuffer(append([]byte(nil), src.Bytes()...))
}
