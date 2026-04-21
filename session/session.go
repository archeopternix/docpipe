package session

import (
	"bytes"
	"context"
	"sync"
	"time"

	"docpipe/converter"
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
	Metadata *processor.Metadata
	Err      error
	Duration time.Duration
	Steps    []processor.StepResult
}

type Session struct {
	ID     string
	Status Status

	input  *bytes.Buffer
	format converter.Format
	result *Result

	done chan struct{}
	mu   sync.Mutex
}

func New(format converter.Format, input *bytes.Buffer) *Session {
	return &Session{
		ID:     newSessionID(),
		Status: StatusPending,
		input:  cloneBuffer(input),
		format: format,
		done:   make(chan struct{}),
	}
}

func (s *Session) Result() *Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result
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
	s.result = result
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
	s.result = result
	s.mu.Unlock()
	closeOnce(s.done)
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
