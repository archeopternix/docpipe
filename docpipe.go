package docpipe

import (
	"bytes"
	"context"
	docio "docpipe/io"
	"fmt"
	"time"

	"docpipe/processor"
	"docpipe/session"
)

type Pipeline struct {
	Nodes  []processor.Processor
	params processor.PipelineParameters
}

func NewPipeline(nodes ...processor.Processor) *Pipeline {
	return &Pipeline{
		Nodes: append([]processor.Processor(nil), nodes...),
	}
}

func New(opts ...Option) *Pipeline {
	p := NewPipeline()
	p.Apply(opts...)
	return p
}

func (p *Pipeline) Apply(opts ...Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
}

func (p *Pipeline) SetDocumentFormat(format processor.Format) {
	p.params.DocumentFormat = format
}

func (p *Pipeline) SetMetaData(meta docio.MetaData) {
	p.params.MetaData = cloneMetaData(meta)
}

func (p *Pipeline) SetConfig(config processor.Config) {
	p.params.Config = config
}

func (p *Pipeline) Parameters() processor.PipelineParameters {
	return clonePipelineParameters(p.params)
}

func (p *Pipeline) Run(ctx context.Context, input *bytes.Buffer) (*bytes.Buffer, error) {
	result, err := p.RunDetailed(ctx, input)
	if result == nil {
		return nil, err
	}
	return result.Output, err
}

func (p *Pipeline) RunDetailed(ctx context.Context, input *bytes.Buffer) (*session.Result, error) {
	runParams := clonePipelineParameters(p.params)
	result := &session.Result{
		Params: runParams,
	}

	if input == nil {
		result.Err = processor.ErrNilInput
		return result, result.Err
	}

	start := time.Now()
	current := cloneBuffer(input)
	steps := make([]processor.StepResult, 0, len(p.Nodes))

	for _, node := range p.Nodes {
		stepStart := time.Now()
		next, err := node.Process(ctx, current, &runParams)
		step := processor.StepResult{
			Name:     node.Name(),
			Duration: time.Since(stepStart),
			Err:      err,
		}
		steps = append(steps, step)
		if err != nil {
			result.Output = cloneBuffer(current)
			result.Params = clonePipelineParameters(runParams)
			result.Duration = time.Since(start)
			result.Steps = steps
			result.Err = fmt.Errorf("pipeline node %s: %w", node.Name(), err)
			return result, result.Err
		}
		current = next
	}

	result.Output = cloneBuffer(current)
	result.Params = clonePipelineParameters(runParams)
	result.Duration = time.Since(start)
	result.Steps = steps
	return result, nil
}

func (p *Pipeline) RunAsync(ctx context.Context, input *bytes.Buffer) *session.Session {
	s := session.New(input)

	go func() {
		s.MarkRunning()
		result, err := p.RunDetailed(ctx, input)
		if err != nil {
			s.MarkFailed(result, err)
			return
		}
		s.MarkDone(result)
	}()

	return s
}

func clonePipelineParameters(src processor.PipelineParameters) processor.PipelineParameters {
	dst := src
	dst.MetaData = cloneMetaData(src.MetaData)
	return dst
}

func cloneMetaData(src docio.MetaData) docio.MetaData {
	dst := src
	if src.Keywords != nil {
		dst.Keywords = append([]string(nil), src.Keywords...)
	}
	return dst
}

func cloneBuffer(src *bytes.Buffer) *bytes.Buffer {
	if src == nil {
		return bytes.NewBuffer(nil)
	}
	return bytes.NewBuffer(append([]byte(nil), src.Bytes()...))
}
