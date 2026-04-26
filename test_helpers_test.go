package docpipe

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func sampleMarkdown(title, version, body string) string {
	return strings.Join([]string{
		"---",
		`title: "` + title + `"`,
		`version: "` + version + `"`,
		`language: "en"`,
		"---",
		"",
		body,
	}, "\n")
}

func makeTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedStringKeys(entries) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
		if _, err := io.WriteString(w, entries[name]); err != nil {
			t.Fatalf("WriteString(%q) error = %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return buf.Bytes()
}

func readTestZip(t *testing.T, path string) map[string]string {
	t.Helper()

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer reader.Close()

	entries := make(map[string]string)
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("Open(%q) error = %v", file.Name, err)
		}
		body, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			t.Fatalf("ReadAll(%q) error = %v", file.Name, err)
		}
		if closeErr != nil {
			t.Fatalf("Close(%q) error = %v", file.Name, closeErr)
		}
		entries[file.Name] = string(body)
	}
	return entries
}

func writeTestZipFile(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	if err := os.WriteFile(path, makeTestZip(t, entries), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		value := values[i]
		j := i - 1
		for j >= 0 && values[j] > value {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = value
	}
}
