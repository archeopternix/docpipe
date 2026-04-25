// Package docpipe converts Office documents and markdown into a portable, ZIP-backed
// Markdown bundle with optional extracted assets (images and slide screenshots).
//
// # Overview
//
// The central type is Markdown, which represents a document plus its metadata and
// optional associated files. Documents can be created from:
//
//   - .docx (Word) via ParseWordFile (pandoc-backed)
//   - .pptx (PowerPoint) via ParsePowerPointFile (pptx2md-backed, optional screenshots)
//   - .md/.markdown via ParseMarkdownFile
//   - an existing docpipe ZIP bundle via ParseZipFile
//
// # Output format (ZIP layout)
//
// When exporting a document with (*Markdown).SaveAsZip or (*Markdown).CreateZipBytes,
// entries are written using the following layout:
//
//   - /<title>_<lang>_v<version>.md     Root markdown file (with YAML frontmatter)
//   - /document/<original>             Original source file (optional)
//   - /media/*                         Extracted images (optional)
//   - /slides/*                        Slide screenshots (optional, PPTX only)
//   - /versions/*                      Archived prior markdown versions (optional)
//
// # Metadata and file naming
//
// Markdown files are named based on MetaData (Title, Language, Version). YAML
// frontmatter is (re)written to match MetaData, and includes fields such as
// title, subtitle, date, changed_date, original_document, original_format,
// version, language, abstract, keywords, and author.
//
// # Cleanup, versioning, and AI features
//
// Markdown can be normalized and reformatted with (*Markdown).CleanUpMarkdown.
// Before modifications, the current markdown is archived under /versions.
//
// AI-backed features use OpenAI when enabled/configured:
//   - language detection via (*Markdown).LanguageDetectionAI
//   - optional cleanup reformatting via CleanUpMarkdown(UsingAI=true)
//   - translation via (*Markdown).TranslateTo
//
// AI availability is determined by DetectAI, which checks environment variables
// such as OPENAI_API_KEY and OPENAI_MODEL.
//
// # External dependencies
//
// Some conversions rely on external tools being available at runtime:
//
//   - pandoc (for DOCX -> Markdown)
//   - pptx2md (for PPTX -> Markdown)
//   - PowerPoint (Windows, for slide screenshots) or LibreOffice (Linux)
//
// # Errors
//
// The package defines sentinel errors (ErrInvalidInput, ErrUnsupported,
// ErrAIUnavailable) for common failure classes, but some functions also return
// formatted errors from underlying OS/tool invocations.
//
// This package is intended to be used as a library; callers typically parse an
// input file into a *Markdown and then export it as a ZIP bundle.

package docpipe
