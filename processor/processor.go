package processor

import (
	"bytes"
	"context"
	"docpipe/io"
	"time"
)

type Processor interface {
	Name() string
	Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error)
}

type MetaData = io.MetaData

// Config holds the configuration for the processing pipeline.
type Config struct {
	TargetLanguage    string
	IncludeImages     bool
	IncludeSourceFile bool
}

type PipelineParameters struct {
	DocumentFormat Format
	io.MetaData
	Config
}

type StepResult struct {
	Name     string
	Duration time.Duration
	Err      error
}

func cloneBuffer(src *bytes.Buffer) *bytes.Buffer {
	if src == nil {
		return bytes.NewBuffer(nil)
	}
	return bytes.NewBuffer(append([]byte(nil), src.Bytes()...))
}
