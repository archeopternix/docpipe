package io

import "testing"

func TestZipFileName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain name", input: "archive", want: "archive.zip"},
		{name: "existing extension replaced", input: "archive.md", want: "archive.zip"},
		{name: "path stripped", input: "nested/report.txt", want: "report.zip"},
		{name: "blank fallback", input: "   ", want: "document.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := zipFileName(tt.input); got != tt.want {
				t.Fatalf("zipFileName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarkdownEntryName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain name", input: "archive", want: "archive.md"},
		{name: "existing extension replaced", input: "archive.zip", want: "archive.md"},
		{name: "path stripped", input: "nested/report.txt", want: "report.md"},
		{name: "blank fallback", input: "", want: "document.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := markdownEntryName(tt.input); got != tt.want {
				t.Fatalf("markdownEntryName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDocumentEntryName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain file", input: "source.docx", want: "document/source.docx"},
		{name: "path stripped", input: "nested/path/source.docx", want: "document/source.docx"},
		{name: "blank fallback", input: " ", want: "document/document.bin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := documentEntryName(tt.input); got != tt.want {
				t.Fatalf("documentEntryName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPrefixedEntryName(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		input  string
		want   string
	}{
		{name: "simple join", prefix: "media", input: "image.png", want: "media/image.png"},
		{name: "leading slash removed", prefix: "slides", input: "/slide1.png", want: "slides/slide1.png"},
		{name: "existing prefix removed", prefix: "versions", input: "versions/EN_v1.0.md", want: "versions/EN_v1.0.md"},
		{name: "blank fallback", prefix: "media", input: " ", want: "media/file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefixedEntryName(tt.prefix, tt.input); got != tt.want {
				t.Fatalf("prefixedEntryName(%q, %q) = %q, want %q", tt.prefix, tt.input, got, tt.want)
			}
		})
	}
}
