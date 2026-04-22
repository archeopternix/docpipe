package session

import (
	"bytes"
	"context"
	"sync"
	"time"

	"docpipe/processor"
)

type Status int

const (
	StatusPending Status = iota
	StatusRunning
	StatusDone
	StatusFailed
)

type Result struct {
	Output   *bytes.Buffer
	Params   processor.PipelineParameters
	Err      error
	Duration time.Duration
	Steps    []processor.StepResult
}

type Session struct {
	ID     string
	Status Status

	input  *bytes.Buffer
	result *Result

	done chan struct{}
	mu   sync.Mutex
}

func New(input *bytes.Buffer) *Session {
	return &Session{
		ID:     newSessionID(),
		Status: StatusPending,
		input:  cloneBuffer(input),
		done:   make(chan struct{}),
	}
}

func (s *Session) Result() *Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneResult(s.result)
}

func (s *Session) Wait(ctx context.Context) (*Result, error) {
	select {
	case <-s.done:
		return s.Result(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Session) MarkRunning() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusRunning
}

func (s *Session) MarkDone(result *Result) {
	s.mu.Lock()
	s.Status = StatusDone
	s.result = cloneResult(result)
	s.mu.Unlock()
	closeOnce(s.done)
}

func (s *Session) MarkFailed(result *Result, err error) {
	s.mu.Lock()
	s.Status = StatusFailed
	if result == nil {
		result = &Result{}
	}
	result.Err = err
	s.result = cloneResult(result)
	s.mu.Unlock()
	closeOnce(s.done)
}

func cloneResult(src *Result) *Result {
	if src == nil {
		return nil
	}

	dst := *src
	dst.Output = cloneBuffer(src.Output)
	dst.Params = clonePipelineParameters(src.Params)
	if src.Steps != nil {
		dst.Steps = append([]processor.StepResult(nil), src.Steps...)
	}

	return &dst
}

func clonePipelineParameters(src processor.PipelineParameters) processor.PipelineParameters {
	dst := src
	if src.MetaData.Keywords != nil {
		dst.MetaData.Keywords = append([]string(nil), src.MetaData.Keywords...)
	}
	return dst
}

func cloneBuffer(src *bytes.Buffer) *bytes.Buffer {
	if src == nil {
		return bytes.NewBuffer(nil)
	}
	return bytes.NewBuffer(append([]byte(nil), src.Bytes()...))
}

func closeOnce(ch chan struct{}) {
	defer func() {
		_ = recover()
	}()
	close(ch)
}

func newSessionID() string {
	return time.Now().UTC().Format("20060102150405.000000000")
}
