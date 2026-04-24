package docpipe

import (
	docio "docpipe/io"
)

type Markdown struct {
	docio.Documents
}

type WordParams struct {
	IncludeImages bool
}

func CreateFromWord(path string, params *WordParams) (*Markdown, error) {
	if params == nil {
		params = &WordParams{
			IncludeImages: true,
		}
	}

	docs, err := docio.CreateFromWord(path, params)
	if err != nil {
		return nil, err
	}
	return &Markdown{Documents: docs}, nil
}

func CreateFromMarkdown(path string) (*Markdown, error) {
	docs, err := docio.CreateFromMarkdown(path)
	if err != nil {
		return nil, err
	}

	return &Markdown{Documents: docs}, nil
}

type PowerPointParams struct {
	IncludeSlides bool
	IncludeImages bool
}

func CreateFromPowerPoint(path string, params *PowerPointParams) (*Markdown, error) {
	if params == nil {
		params = &PowerPointParams{
			IncludeSlides: true,
			IncludeImages: true,
		}
	}

	docs, err := docio.CreateFromPowerPoint(path, params)
	if err != nil {
		return nil, err
	}
	return &Markdown{Documents: docs}, nil
}

func CreateFromZip(path string) (*Markdown, error) {
	docs, err := docio.CreateFromZip(path)
	if err != nil {
		return nil, err
	}
	return &Markdown{Documents: docs}, nil
}

func (d *Markdown) SaveAsZip(path string) error {
	return d.Documents.SaveAsZip(path)
}
