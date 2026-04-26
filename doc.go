// Package docpipe is intended to be used as library that provides the foundation for full blown
// document management systems. For this purpose it provides functions to render the content as
// HTML including, images and screenshots of slides. It takes different formats:
//
//   - Word (.docx)
//   - PowerPoint (.pptx)
//   - Text + Markdown
//
// and converts them into markdown which is stored by "Service"  into a store-backed
// document layout with ZIP import/export helpers.
//
// # Overview
//
// The preferred runtime API is Service, which owns a Store implementation and
// persists documents by ID using a canonical layout:
//
//   - /root.md                         Root markdown file (with YAML frontmatter)
//   - /media/*                         Extracted images (optional)
//   - /slides/*                        Slide screenshots (optional, PPTX only)
//   - /versions/*                      Archived prior markdown versions (optional)
//
// Documents can be imported through Service.ImportDocument or Service.ImportZip,
// mutated by ID, rendered, and exported with Service.ExportZip. ZIP handling is
// limited to import/export; runtime documents are storage-backed.
//
// # Output format (ZIP layout)
//
// Exporting a stored document is done by using Service.ExportZip.
//
// # Metadata and file naming
//
// YAML frontmatter is represented by Frontmatter and includes fields such as
// title, subtitle, date, changed_date, original_document, original_format,
// version, language, abstract, keywords, and author.
//
// # Cleanup, versioning, and AI features
//
// Stored markdown can be read and updated through Service.ReadMarkdown,
// Service.WriteMarkdown, and Service.UpdateFrontmatter. Pure markdown cleanup is
// available through package clean and Service.Clean. Before modifications, the
// current markdown may be archived under /versions through UpdateOptions.
//
// AI-backed operations are exposed through service methods that accept an
// ai.Client implementation for translation and language detection. Package ai
// includes a ChatGPT-backed client for OpenAI's Responses API.
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
// This package is intended to be used as a library; callers typically construct
// a Service with a filesystem-backed store, import a source document, and then
// read, mutate, render, or export the stored document by ID.
package docpipe
