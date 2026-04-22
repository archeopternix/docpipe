package io

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	stdio "io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type FileConverter func(path string, meta *MetaData) (*bytes.Buffer, error)

var (
	ErrMetaDataNil       = errors.New("docpipe: nil metadata")
	ErrUnsupportedFormat = errors.New("converter: unsupported format")

	officeMu = &sync.Mutex{}

	fileConverters = map[string]FileConverter{
		".docx":     WordFileConverter,
		".pptx":     PptxFileConverter,
		".md":       MarkdownFileConverter,
		".markdown": MarkdownFileConverter,
		".txt":      TextFileConverter,
	}
)

func ConvertFile(path string, meta *MetaData) (*bytes.Buffer, error) {
	if meta == nil {
		return nil, ErrMetaDataNil
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	ext := NormalizeExtension(filepath.Ext(path))
	converter, ok := fileConverters[ext]
	if !ok {
		return nil, ErrUnsupportedFormat
	}

	// Read metadata first to populate common fields, then call the converter to extract text.
	var err error
	*meta, err = ReadMetaData(path)
	if err != nil {
		return nil, err
	}

	return converter(path, meta)
}

func WordFileConverter(path string, meta *MetaData) (*bytes.Buffer, error) {
	if meta == nil {
		return nil, ErrMetaDataNil
	}

	officeMu.Lock()
	defer officeMu.Unlock()

	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	props, err := readOfficeCorePropertiesFromFiles(reader.File)
	if err != nil {
		return nil, err
	}
	applyOfficeCoreProperties(meta, props)

	text, err := extractWordText(reader.File)
	if err != nil {
		return nil, err
	}
	return bytes.NewBufferString(text), nil
}

func PptxFileConverter(path string, meta *MetaData) (*bytes.Buffer, error) {
	if meta == nil {
		return nil, ErrMetaDataNil
	}

	officeMu.Lock()
	defer officeMu.Unlock()

	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	props, err := readOfficeCorePropertiesFromFiles(reader.File)
	if err != nil {
		return nil, err
	}
	applyOfficeCoreProperties(meta, props)

	text, err := extractPptxText(reader.File)
	if err != nil {
		return nil, err
	}
	return bytes.NewBufferString(text), nil
}

func MarkdownFileConverter(path string, meta *MetaData) (*bytes.Buffer, error) {
	if meta == nil {
		return nil, ErrMetaDataNil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bytes.NewBuffer(append([]byte(nil), body...)), nil
}

func TextFileConverter(path string, meta *MetaData) (*bytes.Buffer, error) {
	if meta == nil {
		return nil, ErrMetaDataNil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bytes.NewBuffer(append([]byte(nil), body...)), nil
}

func extractWordText(files []*zip.File) (string, error) {
	file := findZipFile(files, "word/document.xml")
	if file == nil {
		return "", nil
	}
	return extractXMLText(file, xmlTextOptions{
		textNames:      map[string]bool{"t": true},
		tabNames:       map[string]bool{"tab": true},
		breakNames:     map[string]bool{"br": true, "cr": true},
		paragraphNames: map[string]bool{"p": true},
	})
}

func extractPptxText(files []*zip.File) (string, error) {
	var slides []*zip.File
	for _, file := range files {
		if strings.HasPrefix(file.Name, "ppt/slides/slide") && strings.HasSuffix(file.Name, ".xml") {
			slides = append(slides, file)
		}
	}
	slices.SortFunc(slides, func(a, b *zip.File) int {
		return strings.Compare(a.Name, b.Name)
	})

	parts := make([]string, 0, len(slides))
	for _, slide := range slides {
		text, err := extractXMLText(slide, xmlTextOptions{
			textNames:      map[string]bool{"t": true},
			tabNames:       map[string]bool{"tab": true},
			breakNames:     map[string]bool{"br": true},
			paragraphNames: map[string]bool{"p": true},
		})
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

type xmlTextOptions struct {
	textNames      map[string]bool
	tabNames       map[string]bool
	breakNames     map[string]bool
	paragraphNames map[string]bool
}

func extractXMLText(file *zip.File, options xmlTextOptions) (string, error) {
	rc, err := file.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	decoder := xml.NewDecoder(rc)
	var builder strings.Builder
	inText := false

	appendRune := func(r rune) {
		builder.WriteRune(r)
	}
	appendParagraphBreak := func() {
		if builder.Len() == 0 {
			return
		}
		if strings.HasSuffix(builder.String(), "\n") {
			return
		}
		builder.WriteByte('\n')
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == stdio.EOF {
				break
			}
			return "", err
		}

		switch typed := token.(type) {
		case xml.StartElement:
			local := typed.Name.Local
			switch {
			case options.textNames[local]:
				inText = true
			case options.tabNames[local]:
				appendRune('\t')
			case options.breakNames[local]:
				appendRune('\n')
			}
		case xml.EndElement:
			local := typed.Name.Local
			switch {
			case options.textNames[local]:
				inText = false
			case options.paragraphNames[local]:
				appendParagraphBreak()
			}
		case xml.CharData:
			if inText {
				builder.Write([]byte(typed))
			}
		}
	}

	return strings.TrimSpace(builder.String()), nil
}

func findZipFile(files []*zip.File, name string) *zip.File {
	for _, file := range files {
		if file.Name == name {
			return file
		}
	}
	return nil
}
