# docpipe

`docpipe` converts Office documents and Markdown into a portable, ZIP-backed Markdown bundle with optional extracted assets (images and slide screenshots). The resulting ZIP contains:

- a root Markdown file (with YAML frontmatter metadata),
- optionally the original source document,
- optionally extracted images,
- optionally slide screenshots (PPTX),
- optionally archived prior Markdown versions.

## Features

- **DOCX → Markdown** via `pandoc`
- **PPTX → Markdown** via `pptx2md`
- **MD → docpipe bundle** (normalizes frontmatter)
- **Import/export docpipe ZIP bundles**
- **Cleanup**: normalize Markdown, strip HTML, convert simple HTML tables to Markdown tables
- **AI (optional)**:
  - language detection
  - cleanup/reformatting without changing meaning
  - translation (optionally with rephrasing)

## ZIP bundle format

When exporting a document with `(*Markdown).SaveAsZip` or `(*Markdown).CreateZipBytes`, entries are written using this layout:

| File / Folder                    | Description                 |
| --------------------------------- | -------------------------------------- |
| /<title>_<lang>_v<version>.md     | Root markdown file (YAML frontmatter) |
| /document/<original>             | Original input document (optional) |
| /media/*                         | Extracted images (optional) |
| /slides/*                        | Slide screenshots (optional, PPTX only) |
| /versions/*                      | Archived prior markdown versions (optional) |

## Metadata (YAML frontmatter)

The root Markdown file contains YAML frontmatter derived from `MetaData`, typically including:

- `title`, `subtitle`
- `date`, `changed_date`
- `original_document`, `original_format`
- `version`, `language`
- `abstract`, `keywords`
- `author`

## Usage

### Convert a Word document (.docx) to a bundle

```go
doc, err := docpipe.ParseWordFile("input.docx", &docpipe.WordParams{
    IncludeImages: true,
})
if err != nil {
    // handle
}

if err := doc.SaveAsZip("out"); err != nil {
    // handle
}
```

### Convert a PowerPoint (.pptx) to a bundle

```go
doc, err := docpipe.ParsePowerPointFile("slides.pptx", &docpipe.PowerPointParams{
    IncludeImages: true,
    IncludeSlides: true, // attempts slide screenshots when supported
})
if err != nil {
    // handle
}

if err := doc.SaveAsZip("out"); err != nil {
    // handle
}
```

### Import an existing docpipe ZIP bundle

```go
doc, err := docpipe.ParseZipFile("bundle.zip")
if err != nil {
    // handle
}

```

### Parse a markdown file

```go
doc, err := docpipe.ParseMarkdownFile("notes.md")
if err != nil {
    // handle
}

if err := doc.SaveAsZip("out"); err != nil {
    // handle
}
```

### Cleanup and normalize markdown

```go
err := doc.CleanUpMarkdown(&docpipe.CleanUpParameters{
    UsingAI:       false,
    CleanUpTables: true,
})
if err != nil {
    // handle
}
```
## Optional AI features (OpenAI)

AI-backed functions require environment configuration.

Environment variables
* OPENAI_API_KEY (required to enable AI)
* OPENAI_MODEL (optional; defaults to gpt-4.1-mini if empty)
If __OPENAI_API_KEY__ is not set, __DetectAI()__ returns __false__ and AI functions will fail.

###Translate

```go
err := doc.TranslateTo(&docpipe.TranslationParameters{
    TargetLang: "de",
    Rephrase:   true,
})
if err != nil {
    // handle
}
```
## External dependencies
Some conversions rely on external tools at runtime:

* pandoc (DOCX → Markdown)
* pptx2md (PPTX → Markdown)
* Slide screenshots:
    * Windows: PowerPoint available (COM automation via PowerShell)
    * Linux: LibreOffice available (soffice or libreoffice)
    * If these tools are missing, the relevant conversion will return an error (or skip optional outputs, depending on the feature).

### Error handling
The package defines sentinel errors:

* ErrInvalidInput
* ErrUnsupported
* ErrAIUnavailable

Some functions also return wrapped or formatted errors from underlying filesystem or command execution.