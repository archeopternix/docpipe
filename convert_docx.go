package docpipe

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/archeopternix/docpipe/clean"
)

func convertDocx(ctx context.Context, path string, src ImportSource, opt WordOptions) (importedDocument, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return importedDocument{}, err
	}
	if mdNormalizeExtension(filepath.Ext(path)) != ".docx" {
		return importedDocument{}, ErrUnsupported
	}

	pandocPath, err := requiredTool("pandoc")
	if err != nil {
		return importedDocument{}, err
	}

	meta, err := officeFrontmatter(path, src)
	if err != nil {
		return importedDocument{}, err
	}

	var mediaDir string
	if opt.IncludeImages {
		mediaDir, err = os.MkdirTemp("", "docx-media-*")
		if err != nil {
			return importedDocument{}, err
		}
		defer func() { _ = os.RemoveAll(mediaDir) }()
	}

	args := []string{
		path,
		"-t", "gfm",
		"--wrap=none",
	}
	if opt.IncludeImages {
		args = append(args, "--extract-media="+mediaDir)
	}

	cmdCtx, cancel, timeout := contextWithToolTimeout(ctx, defaultExternalToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, pandocPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	body, err := cmd.Output()
	if err != nil {
		return importedDocument{}, commandRunError(cmdCtx, "pandoc", timeout, err, stderr.Bytes())
	}

	imported := importedDocument{Media: map[string][]byte{}}
	if opt.IncludeImages {
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
			imported.Media[filepath.ToSlash(filepath.Join("media", relPath))] = body
			return nil
		}); err != nil {
			return importedDocument{}, err
		}
	}

	text := clean.Normalize(string(body), clean.Options{CleanTables: true})
	imported.Root = []byte(mdComposeMarkdownWithMeta(meta, text))
	return imported, nil
}
