package store

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/archeopternix/docpipe/internal/pathutil"
)

type FS struct {
	BasePath string
}

func (s FS) Open(ctx context.Context, docID, name string) (fs.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fullPath, err := s.resolveFile(docID, name)
	if err != nil {
		return nil, err
	}
	return os.Open(fullPath)
}

func (s FS) ReadDir(ctx context.Context, docID, dir string) ([]fs.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fullPath, err := s.resolveDir(docID, dir)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(fullPath)
}

func (s FS) WriteFile(ctx context.Context, docID, name string, data []byte, perm fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := s.resolveFile(docID, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, data, perm)
}

func (s FS) MkdirAll(ctx context.Context, docID, dir string, perm fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := s.resolveDir(docID, dir)
	if err != nil {
		return err
	}
	return os.MkdirAll(fullPath, perm)
}

func (s FS) Remove(ctx context.Context, docID, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := s.resolveAny(docID, name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s FS) resolveFile(docID, name string) (string, error) {
	cleanID, err := pathutil.CleanDocID(docID)
	if err != nil {
		return "", err
	}
	cleanName, err := pathutil.CleanName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.basePath(), filepath.FromSlash(cleanID), filepath.FromSlash(cleanName)), nil
}

func (s FS) resolveDir(docID, dir string) (string, error) {
	cleanID, err := pathutil.CleanDocID(docID)
	if err != nil {
		return "", err
	}
	cleanDir, err := pathutil.CleanDir(dir)
	if err != nil {
		return "", err
	}
	if cleanDir == "." {
		return filepath.Join(s.basePath(), filepath.FromSlash(cleanID)), nil
	}
	return filepath.Join(s.basePath(), filepath.FromSlash(cleanID), filepath.FromSlash(cleanDir)), nil
}

func (s FS) resolveAny(docID, name string) (string, error) {
	if strings.TrimSpace(name) == "" || name == "." {
		return s.resolveDir(docID, ".")
	}
	if fullPath, err := s.resolveFile(docID, name); err == nil {
		return fullPath, nil
	}
	return s.resolveDir(docID, name)
}

func (s FS) basePath() string {
	if strings.TrimSpace(s.BasePath) == "" {
		return "."
	}
	return s.BasePath
}
