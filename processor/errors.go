package processor

import "errors"

var (
	ErrNilInput          = errors.New("docpipe: nil input buffer")
	ErrParameterMissing  = errors.New("docpipe: missing parameter")
	ErrUnsupportedFormat = errors.New("converter: unsupported format")
)
