package docpipe

import (
	"runtime"

	"docpipe/converter"
	"docpipe/processor"
)

type Option func(*Engine)

func WithMaxParallel(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.maxParallel = n
		}
	}
}

func WithConverter(c converter.Converter) Option {
	return func(e *Engine) {
		if c != nil {
			e.RegisterConverter(c)
		}
	}
}

func WithProcessors(ps ...processor.Processor) Option {
	return func(e *Engine) {
		e.processorRegistry = make(map[string]processor.Processor)
		e.processorOrder = e.processorOrder[:0]
		for _, p := range ps {
			if p != nil {
				e.RegisterProcessor(p)
			}
		}
	}
}

func WithTranslationBackend(translate processor.TranslateFunc) Option {
	return func(e *Engine) {
		e.translation = translate
	}
}

func defaultMaxParallel() int {
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	return n
}
