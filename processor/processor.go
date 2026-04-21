package processor

import (
	"bytes"
	"context"
	"errors"
	"time"
)

var ErrProcessorNotFound = errors.New("processor: not found")

type Processor interface {
	Name() string
	Process(ctx context.Context, in *bytes.Buffer, params *PipelineParameters) (*bytes.Buffer, error)
}

type PipelineParameters struct {
	Parameters map[string]string
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
