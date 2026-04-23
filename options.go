package docpipe

import (
	docio "docpipe/io"
	"docpipe/processor"
)

type Option func(*Pipeline)

func WithProcessor(node processor.Processor) Option {
	return func(p *Pipeline) {
		if p == nil || node == nil {
			return
		}
		p.Nodes = append(p.Nodes, node)
	}
}

func WithProcessors(nodes ...processor.Processor) Option {
	return func(p *Pipeline) {
		if p == nil {
			return
		}
		p.Nodes = p.Nodes[:0]
		for _, node := range nodes {
			if node != nil {
				p.Nodes = append(p.Nodes, node)
			}
		}
	}
}

func WithDocumentFormat(format processor.Format) Option {
	return func(p *Pipeline) {
		if p == nil {
			return
		}
		p.params.DocumentFormat = format
	}
}

func WithMetaData(meta docio.MetaData) Option {
	return func(p *Pipeline) {
		if p == nil {
			return
		}
		p.params.MetaData = cloneMetaData(meta)
	}
}

func WithConfig(config processor.Config) Option {
	return func(p *Pipeline) {
		if p == nil {
			return
		}
		p.params.Config = config
	}
}

func WithTargetLanguage(lang string) Option {
	return func(p *Pipeline) {
		if p == nil {
			return
		}
		p.params.TargetLanguage = lang
	}
}

func WithIncludeImages(include bool) Option {
	return func(p *Pipeline) {
		if p == nil {
			return
		}
		p.params.IncludeImages = include
	}
}

func WithIncludeSourceFile(include bool) Option {
	return func(p *Pipeline) {
		if p == nil {
			return
		}
		p.params.IncludeSourceFile = include
	}
}
