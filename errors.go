package docpipe

import "errors"

var (
	ErrInvalidInput  = errors.New("docpipe: invalid input")
	ErrUnsupported   = errors.New("docpipe: unsupported format")
	ErrAIUnavailable = errors.New("docpipe: AI unavailable")
)
