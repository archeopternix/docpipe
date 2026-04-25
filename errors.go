package docpipe

import "errors"

var (
	ErrInvalidInput  = errors.New("docpipe: invalid input")
	ErrUnsupported   = errors.New("docpipe: unsupported format")
	ErrAIUnavailable = errors.New("docpipe: AI unavailable")
	ErrTimeout       = errors.New("docpipe: timeout")
	ErrToolMissing   = errors.New("docpipe: required tool missing")
)
