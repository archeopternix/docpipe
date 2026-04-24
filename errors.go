package docpipe

import "errors"

var (
	ErrNilInput            = errors.New("docpipe: nil input buffer")
	ErrParameterMissing    = errors.New("docpipe: missing parameter")
	ErrDocxNilSource       = errors.New("docx file converter: nil source")
	ErrPptxNilSource       = errors.New("pptx file converter: nil source")
	ErrMarkdownNilSource   = errors.New("markdown file converter: nil source")
	ErrTextNilSource       = errors.New("text file converter: nil source")
	ErrLangDetectionFailed = errors.New("language detection failed")
	ErrTargetLangMissing   = errors.New("translator: missing target language")
	ErrProcessorNotFound   = errors.New("processor: not found")
	ErrNoFrontmatter       = errors.New("markdown file converter: no frontmatter found")
)
