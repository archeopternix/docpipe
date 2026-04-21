package docpipe

import (
	"bytes"
	"context"
	"fmt"

	"docpipe/processor"
)

type Pipeline struct {
	Nodes  []processor.Processor
	params processor.PipelineParameters
}

func NewPipeline(nodes ...processor.Processor) *Pipeline {
	return &Pipeline{
		Nodes: nodes,
		params: processor.PipelineParameters{
			Parameters: make(map[string]string),
		},
	}
}

func (p *Pipeline) AddParameter(key string, value string) {
	p.params.Parameters[key] = value
}

func (p *Pipeline) Run(ctx context.Context, input *bytes.Buffer) (*bytes.Buffer, error) {
	var err error

	current := input

	for _, node := range p.Nodes {
		current, err = node.Process(ctx, current, &p.params)
		if err != nil {
			return nil, fmt.Errorf("pipeline node %s: %w", node.Name(), err)
		}
	}

	return current, nil
}
