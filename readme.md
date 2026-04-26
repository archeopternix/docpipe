# docpipe

`docpipe` is a Go library for building document management systems. It converts
common document formats into Markdown, stores the result in a store-backed
document layout, and provides helpers for rendering, importing, exporting, and
updating stored documents.

## Supported Formats

`docpipe` can import and convert:

- Word documents (`.docx`)
- PowerPoint presentations (`.pptx`)
- Plain text and Markdown

Converted content is stored as Markdown and can include extracted media and
slide screenshots.

## Overview

The preferred runtime API is `Service`. A service owns a `Store`
implementation and persists documents by ID using a canonical layout:

```text
/root.md       Root Markdown file with YAML frontmatter
/media/*       Extracted images, when present
/slides/*      Slide screenshots, when present for PPTX documents
/versions/*    Archived prior Markdown versions, when present
```

Documents can be imported, mutated, rendered, and exported through the service
API:

- `Service.ImportDocument`
- `Service.ImportZip`
- `Service.ReadMarkdown`
- `Service.WriteMarkdown`
- `Service.UpdateFrontmatter`
- `Service.Clean`
- `Service.ExportZip`

ZIP handling is limited to import and export. Runtime documents are backed by
the configured store.

## Export Layout

Stored documents can be exported with `Service.ExportZip`. The exported ZIP uses
the same canonical layout used by the store-backed document representation.

## Metadata

Document metadata is represented as YAML frontmatter by `Frontmatter`. Supported
fields include:

- `title`
- `subtitle`
- `date`
- `changed_date`
- `original_document`
- `original_format`
- `version`
- `language`
- `abstract`
- `keywords`
- `author`

## Cleanup, Versioning, and AI

Markdown cleanup is available through the `clean` package and `Service.Clean`.
Before modifications, the current Markdown can be archived under `/versions`
through `UpdateOptions`.

AI-backed operations are exposed through service methods that accept an
`ai.Client` implementation for translation and language detection.

## External Dependencies

Some conversions require external tools at runtime:

- `pandoc` for DOCX to Markdown conversion
- `pptx2md` for PPTX to Markdown conversion
- PowerPoint on Windows or LibreOffice on Linux for slide screenshots

Make sure the required tools for the formats you process are installed and
available on the system path.

## Errors

The package defines sentinel errors for common failure classes:

- `ErrInvalidInput`
- `ErrUnsupported`
- `ErrAIUnavailable`

Some operations can also return formatted errors from underlying operating
system calls or external tool invocations.

## Typical Usage

Callers typically construct a `Service` with a filesystem-backed store, import a
source document, and then read, mutate, render, or export the stored document by
ID.
