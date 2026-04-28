package docpipe

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type importedDocument struct {
	Root         []byte
	Original     []byte
	OriginalName string
	Media        map[string][]byte
	Slides       map[string][]byte
	Versions     map[string][]byte
}

func (s Service) importDocx(ctx context.Context, doc Document, src ImportSource, opt WordOptions) error {
	file, _, cleanup, err := s.stageImportSource(ctx, src)
	if err != nil {
		return err
	}
	defer cleanup()

	converted, err := convertDocx(ctx, file.Name(), src, opt)
	if err != nil {
		return err
	}
	converted.OriginalName = filepath.Base(file.Name())
	return s.persistImportedDocument(ctx, doc, converted)
}

func (s Service) importPptx(ctx context.Context, doc Document, src ImportSource, opt PptxOptions) error {
	file, _, cleanup, err := s.stageImportSource(ctx, src)
	if err != nil {
		return err
	}
	defer cleanup()

	converted, err := convertPptx(ctx, file.Name(), src, opt)
	if err != nil {
		return err
	}
	converted.OriginalName = filepath.Base(file.Name())
	return s.persistImportedDocument(ctx, doc, converted)
}

func (s Service) importMarkdownFile(ctx context.Context, doc Document, src ImportSource) error {
	file, _, cleanup, err := s.stageImportSource(ctx, src)
	if err != nil {
		return err
	}
	defer cleanup()

	converted, err := convertMarkdownFile(file.Name(), src)
	if err != nil {
		return err
	}
	converted.OriginalName = filepath.Base(file.Name())
	return s.persistImportedDocument(ctx, doc, converted)
}

func (s Service) stageImportSource(ctx context.Context, src ImportSource) (*os.File, int64, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, func() {}, err
	}
	if src.Reader == nil {
		return nil, 0, func() {}, fmt.Errorf("%w: import source reader is nil", ErrInvalidInput)
	}

	cfg := s.importConfig()
	if src.Size > 0 && cfg.MaxBytes > 0 && src.Size > cfg.MaxBytes {
		return nil, 0, func() {}, fmt.Errorf("%w: import source exceeds %d bytes", ErrInvalidInput, cfg.MaxBytes)
	}

	ext := strings.ToLower(filepath.Ext(src.Name))
	pattern := "docpipe-src-*"
	if ext != "" {
		pattern += ext
	}
	tempFile, err := os.CreateTemp(cfg.TempDir, pattern)
	if err != nil {
		return nil, 0, func() {}, err
	}

	cleanup := func() {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
	}

	reader := io.Reader(src.Reader)
	if cfg.MaxBytes > 0 {
		reader = io.LimitReader(src.Reader, cfg.MaxBytes+1)
	}
	written, err := io.Copy(tempFile, reader)
	if err != nil {
		cleanup()
		return nil, 0, func() {}, err
	}
	if cfg.MaxBytes > 0 && written > cfg.MaxBytes {
		cleanup()
		return nil, 0, func() {}, fmt.Errorf("%w: import source exceeds %d bytes", ErrInvalidInput, cfg.MaxBytes)
	}
	if err := tempFile.Sync(); err != nil {
		cleanup()
		return nil, 0, func() {}, err
	}
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, func() {}, err
	}
	return tempFile, written, cleanup, nil
}

func (s Service) persistImportedDocument(ctx context.Context, doc Document, imported importedDocument) error {
	if len(imported.Root) == 0 {
		return fmt.Errorf("%w: imported document has no root markdown", ErrInvalidInput)
	}
	if err := s.resetDocument(ctx, doc); err != nil {
		return err
	}

	if err := s.Store.WriteFile(ctx, doc.ID, s.paths().RootMarkdown, imported.Root, 0o644); err != nil {
		return err
	}

	if err := s.Store.WriteFile(ctx, doc.ID, filepath.ToSlash(filepath.Join(s.paths().OriginalDir, imported.OriginalName)), imported.Original, 0o644); err != nil {
		return err
	}

	for name, body := range imported.Media {
		if err := s.Store.WriteFile(ctx, doc.ID, name, append([]byte(nil), body...), 0o644); err != nil {
			return err
		}
	}
	for name, body := range imported.Slides {
		if err := s.Store.WriteFile(ctx, doc.ID, name, append([]byte(nil), body...), 0o644); err != nil {
			return err
		}
	}
	for name, body := range imported.Versions {
		entryName := name
		if !strings.HasPrefix(entryName, s.paths().VersionsDir+"/") {
			entryName = filepath.ToSlash(filepath.Join(s.paths().VersionsDir, entryName))
		}
		if err := s.Store.WriteFile(ctx, doc.ID, entryName, append([]byte(nil), body...), 0o644); err != nil {
			return err
		}
	}
	return nil
}
