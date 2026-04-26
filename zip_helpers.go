package docpipe

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

func markdownZipReadFile(file *zip.File) ([]byte, error) {
	if file == nil {
		return nil, fmt.Errorf("%w: zip entry is nil", ErrInvalidInput)
	}

	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var buf bytes.Buffer
	limited := io.LimitReader(rc, defaultMaxZipEntryReadBytes+1)
	if _, err := io.CopyBuffer(&buf, limited, make([]byte, 32*1024)); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > defaultMaxZipEntryReadBytes {
		return nil, fmt.Errorf("%w: zip entry %q exceeds %d bytes", ErrInvalidInput, file.Name, defaultMaxZipEntryReadBytes)
	}
	return append([]byte(nil), buf.Bytes()...), nil
}

func markdownZipCleanEntryName(name string) string {
	clean := filepath.ToSlash(strings.TrimSpace(name))
	clean = strings.TrimPrefix(clean, "/")
	clean = filepath.Clean(clean)
	clean = filepath.ToSlash(clean)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return ""
	}
	return clean
}

func markdownZipIsRootMarkdown(name string) bool {
	if strings.Contains(name, "/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func markdownWriteZipEntry(writer *zip.Writer, name string, body []byte) error {
	if writer == nil {
		return fmt.Errorf("%w: zip writer is nil", ErrInvalidInput)
	}

	header := &zip.FileHeader{
		Name:   filepath.ToSlash(name),
		Method: zip.Deflate,
	}
	entryWriter, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = bytes.NewReader(body).WriteTo(entryWriter)
	return err
}

func markdownUUID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}

	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}
