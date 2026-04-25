package docpipe

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	stdio "io"
	"os"
	"os/exec"
	"path/filepath"
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

// WordParams and IncludeImages controls whether embedded images should be extracted and
// stored into the resulting Markdown ZIP (under /media).
type WordParams struct {
	IncludeImages bool
}

// ParseWordFile converts a .docx file into a Markdown ZIP document.
//
// Behavior:
//   - Only ".docx" is supported; other extensions return an error.
//   - Uses `pandoc` to convert the document to GitHub-Flavored Markdown (GFM)
//     with wrapping disabled.
//   - If params.IncludeImages is true, images are extracted and added to the
//     Markdown object under /media.
//   - Produces cleaned markdown and injects YAML frontmatter based on document
//     metadata.
//
// params:
//   - If params is nil, defaults are used (IncludeImages=true).
func ParseWordFile(path string, params *WordParams) (*Markdown, error) {
	return ParseWordFileContext(context.Background(), path, params)
}

// ParseWordFileContext converts a .docx file into a Markdown ZIP document.
func ParseWordFileContext(ctx context.Context, path string, params *WordParams) (*Markdown, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if params == nil {
		params = &WordParams{
			IncludeImages: true,
		}
	}

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrInvalidInput, path, err)
	}
	if mdNormalizeExtension(filepath.Ext(path)) != ".docx" {
		return nil, fmt.Errorf("%w: word conversion not supported for %q", ErrUnsupported, filepath.Ext(path))
	}

	pandocPath, err := requiredTool("pandoc")
	if err != nil {
		return nil, err
	}

	doc, err := officeNewDocument(path)
	if err != nil {
		return nil, err
	}
	var mediaDir string
	if params.IncludeImages {
		var err error
		mediaDir, err = os.MkdirTemp("", "docx-media-*")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(mediaDir) }()
	}

	args := []string{
		path,
		"-t", "gfm",
		"--wrap=none",
	}
	if params.IncludeImages {
		args = append(args, "--extract-media="+mediaDir)
	}

	cmdCtx, cancel, timeout := contextWithToolTimeout(ctx, defaultExternalToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, pandocPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	body, err := cmd.Output()
	if err != nil {
		return nil, commandRunError(cmdCtx, "pandoc", timeout, err, stderr.Bytes())
	}

	if params.IncludeImages {
		if err := filepath.Walk(mediaDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info == nil || info.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(mediaDir, path)
			if err != nil {
				return err
			}
			relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "media/")

			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			doc.extractedImages[filepath.ToSlash(filepath.Join("media", relPath))] = bytes.NewBuffer(body)
			return nil
		}); err != nil {
			return nil, err
		}
	}

	doc.markdownFile = bytes.NewBufferString(NormalizeMarkdown(string(body), true))
	mdApplyMetaDataFrontmatter(doc)
	zipName, err := markdownZipFileName(mdFileName(doc.metaData))
	if err != nil {
		return nil, err
	}
	doc.fileName = zipName

	return doc, nil
}

// --------------------------------------------------------------------

func officeNewDocument(path string) (*Markdown, error) {
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	doc := &Markdown{
		originalFile:     bytes.NewBuffer(append([]byte(nil), original...)),
		extractedImages:  make(map[string]*bytes.Buffer),
		extractedSlides:  make(map[string]*bytes.Buffer),
		markdownVersions: make(map[string]*bytes.Buffer),
		metaData:         *mdDefaultMetaData(path),
	}

	var props officeCoreProperties
	for _, file := range reader.File {
		if file.Name != "docProps/core.xml" {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		decoder := xml.NewDecoder(rc)
		decodeErr := decoder.Decode(&props)
		closeErr := rc.Close()
		if decodeErr != nil && decodeErr != stdio.EOF {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		break
	}

	if author := strings.TrimSpace(props.Creator); author != "" {
		doc.metaData.Author = author
	}
	if title := strings.TrimSpace(props.Title); title != "" {
		doc.metaData.Title = title
	}
	if doc.metaData.Version == "" {
		doc.metaData.Version = mdNormalizeVersion(props.Revision)
	}
	if doc.metaData.Language == "" {
		doc.metaData.Language = mdNormalizeLanguageCode(props.Language)
	}

	description := strings.TrimSpace(props.Description)
	if description != "" {
		doc.metaData.Abstract = description
	} else {
		doc.metaData.Abstract = strings.TrimSpace(props.Subject)
	}
	doc.metaData.Keywords = mdNormalizeKeywords(props.Keywords)

	return doc, nil
}
