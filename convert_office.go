package docpipe

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"strings"
)

type officeCoreProperties struct {
	XMLName     xml.Name `xml:"coreProperties"`
	Title       string   `xml:"title"`
	Subject     string   `xml:"subject"`
	Creator     string   `xml:"creator"`
	Keywords    string   `xml:"keywords"`
	Description string   `xml:"description"`
	Language    string   `xml:"language"`
	Revision    string   `xml:"revision"`
}

func officeFrontmatter(path string, src ImportSource) (Frontmatter, error) {
	meta := mdDefaultFrontmatter(src.Name, src.ModTime)

	reader, err := zip.OpenReader(path)
	if err != nil {
		return Frontmatter{}, err
	}
	defer reader.Close()

	var props officeCoreProperties
	for _, file := range reader.File {
		if file.Name != "docProps/core.xml" {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return Frontmatter{}, err
		}
		decoder := xml.NewDecoder(rc)
		decodeErr := decoder.Decode(&props)
		closeErr := rc.Close()
		if decodeErr != nil && decodeErr != io.EOF {
			return Frontmatter{}, decodeErr
		}
		if closeErr != nil {
			return Frontmatter{}, closeErr
		}
		break
	}

	if author := strings.TrimSpace(props.Creator); author != "" {
		meta.Author = author
	}
	if title := strings.TrimSpace(props.Title); title != "" {
		meta.Title = title
	}
	if meta.Version == "" {
		meta.Version = mdNormalizeVersion(props.Revision)
	}
	if meta.Language == "" {
		meta.Language = mdNormalizeLanguageCode(props.Language)
	}

	description := strings.TrimSpace(props.Description)
	if description != "" {
		meta.Abstract = description
	} else {
		meta.Abstract = strings.TrimSpace(props.Subject)
	}
	meta.Keywords = mdNormalizeKeywords(props.Keywords)

	return mdEnsureFrontmatterDefaults(meta, src.Name, src.ModTime), nil
}
