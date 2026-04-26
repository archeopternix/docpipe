package store

import (
	"context"
	"io/fs"
)

type Store interface {
	Open(ctx context.Context, docID, name string) (fs.File, error)
	ReadDir(ctx context.Context, docID, dir string) ([]fs.DirEntry, error)

	WriteFile(ctx context.Context, docID, name string, data []byte, perm fs.FileMode) error
	MkdirAll(ctx context.Context, docID, dir string, perm fs.FileMode) error
	Remove(ctx context.Context, docID, name string) error
}
