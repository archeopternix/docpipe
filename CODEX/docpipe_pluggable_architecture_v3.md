# docpipe — Pluggable Stream Import/Export Architecture (v3 Blueprint)

This document is a **recreation blueprint** for a Go module named `docpipe` that provides document ingestion and lifecycle management with **pluggable importers and exporters**.

v3 incorporates these constraints:

- External modules must be able to implement their own **Store**, **SearchProvider**, **AI client**, **Importer**, and **Exporter**.
- All extension-point **interfaces live in the root package** `docpipe`.
- Concrete built-in implementations live under `internal/`.
- Import/export APIs are **streaming** (`io.Reader` / `io.Writer`).
- **Staging is internal** (stream is spooled to temp), but **sniff is external** (passed to importers for `Accept`).
- Prefer **functional options** over exported fields.
- Reduce visibility: most helper structs are **unexported**.
- Keep only one public types file: `types.go`.
- Built-in importers/exporters are made accessible via **root-level constructor functions**.
- Rendering and heading extraction are **standalone helpers** that take `Markdown` as input (not Service responsibilities).
- Service exposes:
  - `OpenOriginal(ctx, doc, name) (fs.File, error)`
  - `OpenStaged(ctx) (fs.File, int64, error)` for importer implementations.

---

## 1) Package layout (required)

```text
docpipe/
  service.go
  types.go                 // ONLY file that defines exported types
  interfaces.go            // exported interfaces (Store, Importer, ...)
  options.go               // functional options + option structs (unexported)
  registry.go              // importer/exporter registries (minimal)
  errors.go
  helpers.go               // standalone helpers: RenderHTML, HeadingIndex

  internal/
    pathutil/
    tools/

    docio/                 // canonical layout ops + staging + OpenOriginal/OpenStaged implementations
    frontmatter/           // parse/compose/normalize/bump
    renderer/              // implementation used by helpers.go (optional)
    clean/                 // markdown normalization

    storefs/               // filesystem Store implementation
    searchbleve/           // Bleve SearchProvider implementation
    aiopenai/              // OpenAI Responses AIClient implementation

    importers/
      markdown/
      zip/
      docx/
      pptx/

    exporters/
      zip/
      // (optional later) pandoc/ etc.
```

Notes:
- External extension points do **not** import anything under `internal/`.
- Built-ins are registered by default in `NewService`, and also available via root-level constructors.

---

## 2) Public types (types.go)

`types.go` is the only file that defines exported types.

### Document and source

```go
package docpipe

import (
    "io"
    "time"
)

type Document struct { ID string }

type ImportSource struct {
    Reader   io.Reader
    Name     string
    Size     int64
    MimeType string
    ModTime  time.Time
}
```

### Markdown + frontmatter

```go
type Markdown struct {
    Full           string
    Body           string
    Frontmatter    Frontmatter
    HasFrontmatter bool
}

type Frontmatter struct {
    Author           string
    Title            string
    Subtitle         string
    Date             string
    ChangedDate      string
    OriginalDocument string
    OriginalFormat   string
    Version          string
    Language         string
    Abstract         string
    Keywords         []string
}
```

### Rendering helpers

```go
type RenderOptions struct {
    AnchorifyHeadings bool
    RewriteImageURLs  func(orig string) (string, bool)
    SplitSections     bool
}

type Rendered struct {
    TitleHTML       string
    FrontmatterHTML string
    BodyHTML        string
}

type HeadingNode struct {
    Level    int
    Text     string
    AnchorID string
    Children []HeadingNode
}
```

### Sniff (external)

`Sniff` is public so external importers can implement `Accept`.

```go
type Sniff struct {
    Name string
    Ext  string
    Mime string
    Head []byte
}
```

Keep `Sniff` minimal. Do not expose staging structs.

---

## 3) Public interfaces (interfaces.go)

All extension-point interfaces live in the root package.

```go
package docpipe

import (
    "context"
    "io"
    "io/fs"
)

type Store interface {
    Open(ctx context.Context, path string) (fs.File, error)
    WriteFile(ctx context.Context, path string, r io.Reader) error
    MkdirAll(ctx context.Context, path string) error
    ListDir(ctx context.Context, path string) ([]DirEntry, error)
    Remove(ctx context.Context, path string) error
}

type SearchProvider interface {
    Upsert(ctx context.Context, docID string, sections []SearchSection) error
    Delete(ctx context.Context, docID string) error
    Search(ctx context.Context, q SearchQuery) ([]SearchHit, error)
}

type AIClient interface {
    Generate(ctx context.Context, instructions, input string) (string, error)
}

type Importer interface {
    Name() string
    Accept(ctx context.Context, sn Sniff) bool

    Import(ctx context.Context, s *Service, src ImportSource) (*Document, error)
    ImportInto(ctx context.Context, s *Service, doc *Document, src ImportSource) error
}

type Exporter interface {
    Name() string
    Accept(ctx context.Context, format string) bool

    Export(ctx context.Context, s *Service, doc Document, w io.Writer) error
}
```

---

## 4) Service (service.go)

### 4.1 Service responsibilities

`Service` orchestrates:

- validation and canonical layout
- internal staging (spool to temp)
- sniff generation (first N bytes)
- importer/exporter selection via registries
- persistence (root.md, media, slides, versions, original)
- optional cleanup and AI operations

`Service` does **not** perform HTML rendering or heading-tree computation.

### 4.2 Service shape

To reduce exported surface, `Service` exposes only essential methods and keeps config in unexported fields.

```go
type Service struct {
    store   Store
    search  SearchProvider

    paths   Paths

    imps *ImporterRegistry
    exps *ExporterRegistry

    // unexported options
    opt serviceOptions
}
```

### 4.3 Functional options (options.go)

Prefer:

```go
type Option func(*serviceOptions)

func WithMaxImportBytes(n int64) Option
func WithTempDir(dir string) Option
func WithSniffBytes(n int) Option
func WithIncludeImages(v bool) Option
func WithIncludeSlides(v bool) Option
func WithPaths(p Paths) Option

func WithImporter(i Importer) Option
func WithExporter(e Exporter) Option
```

Keep `serviceOptions` unexported.

### 4.4 NewService default wiring (required)

`NewService` registers built-ins and allows user overrides through options.

```go
func NewService(st Store, sp SearchProvider, opts ...Option) *Service
```

Behavior:

- defaults:
  - max import bytes = 512 MiB
  - sniff bytes = 64 KiB
  - include images = true
  - include slides = true
  - temp dir = "" (system default)
  - paths = DefaultPaths()

- registry defaults:
  - registers built-in importers and exporters

### 4.5 Root-level constructors for built-ins (required)

External packages must be able to register built-ins without importing `internal/`.

Provide root-level functions:

```go
func NewMarkdownImporter() Importer
func NewZipImporter() Importer
func NewDocxImporter() Importer
func NewPptxImporter() Importer

func NewZipExporter() Exporter
```

Implementation delegates to `internal/importers/...` and `internal/exporters/...`.

---

## 5) Registries (registry.go)

Registries are minimal and hide internal details.

```go
type ImporterRegistry struct { list []Importer }

type ExporterRegistry struct { list []Exporter }

func NewImporterRegistry(imps ...Importer) *ImporterRegistry
func NewExporterRegistry(exps ...Exporter) *ExporterRegistry

func (r *ImporterRegistry) Register(i Importer)
func (r *ExporterRegistry) Register(e Exporter)

func (r *ImporterRegistry) Select(ctx context.Context, sn Sniff) (Importer, error)
func (r *ExporterRegistry) Select(ctx context.Context, format string) (Exporter, error)
```

Selection:
- importer: first `Accept(ctx, sn) == true`
- exporter: first `Accept(ctx, format) == true`

---

## 6) Staging and sniffing

### 6.1 Staging (internal)

Staging is an internal mechanism that spools `ImportSource.Reader` to a temp file to:
- enforce size limits
- allow sniffing without consuming the stream
- support tools that require a file
- support formats needing seeking / random access

Staging lives in `internal/docio`.

### 6.2 Sniffing (external)

`Service` produces a `Sniff` value (public type) from the staged file:
- `Ext` from `src.Name`
- `Mime` from `src.MimeType`
- `Head` from first `SniffBytes`

`SniffBytes` is configurable via options.

---

## 7) Service import API

### 7.1 Import methods

```go
func (s *Service) Import(ctx context.Context, src ImportSource) (*Document, error)
func (s *Service) ImportInto(ctx context.Context, doc *Document, src ImportSource) error
```

Required semantics:

- `Import` creates a new document ID and calls `ImportInto`.
- `ImportInto` stages the stream internally, generates `Sniff`, selects importer, resets doc content, then calls `Importer.ImportInto`.

### 7.2 OpenStaged (required)

Importers may need access to the staged input.

```go
func (s *Service) OpenStaged(ctx context.Context) (fs.File, int64, error)
```

Rules:
- Returns a readable file representing the staged import payload and its size.
- Must not expose staging paths or staging structs.
- Concrete return type may also implement `io.Seeker` and `io.ReaderAt`. Importers may type-assert when needed.

---

## 8) Service export API

```go
func (s *Service) Export(ctx context.Context, doc Document, format string, w io.Writer) error
```

Rules:
- Select exporter by `format`.
- Exporter streams to `w`.

If an exporter requires a file-based tool:
- it must spool output to a temp file internally, then stream the file to `w`.

---

## 9) Canonical document IO helpers (internal/docio)

Keep most layout operations internal.

Required internal capabilities:
- reset document directory
- write root markdown (`root.md`)
- write media/slides/versions files (streaming)
- write original file (streaming copy from staged input)

### 9.1 OpenOriginal (required on Service)

Expose a read operation for originals:

```go
func (s Service) OpenOriginal(ctx context.Context, doc Document, name string) (fs.File, error)
```

Rules:
- Opens file under canonical `original/` directory.
- Must reject traversal and invalid names.

---

## 10) Importer behavior guidelines

All importers must:

- implement `Accept(ctx, sn Sniff) bool` using extension, mime, and `sn.Head`.
- implement `Import` by delegating to `Service.ImportInto` or implementing equivalent behavior.
- use `s.OpenStaged(ctx)` inside `ImportInto` if they need to read the payload.
- write outputs via Service methods (which route to internal/docio).
- never load the full source into memory.

### 10.1 Suggested sniff patterns

- ZIP: magic bytes `PK\x03\x04`
- Markdown: begins with `---\n` within head window, or `Ext` is `.md/.markdown/.txt`
- DOCX/PPTX: both are zip containers; rely on extension primarily in `Accept`, and do full validation after selection.

---

## 11) Exporter behavior guidelines

Exporters must:

- implement `Accept` for `format` values like `"zip"`, `".zip"`, or mime types.
- stream output to `io.Writer`.
- avoid building whole archives in memory.

---

## 12) Standalone helpers (helpers.go)

Provide standalone functions that operate on `Markdown` input:

```go
func RenderHTML(md Markdown, opt RenderOptions) (Rendered, error)
func HeadingIndex(md Markdown, maxLevel int) ([]HeadingNode, error)
```

Rules:
- Do not access `Store`.
- Do not mutate documents.

---

## 13) Error model (errors.go)

Define sentinel errors:

- `ErrInvalidInput`
- `ErrUnsupported`
- `ErrAIUnavailable`
- `ErrTimeout`
- `ErrToolMissing`

Rules:
- registry selection failures wrap `ErrUnsupported`
- missing external tools wrap `ErrToolMissing` and `ErrUnsupported`
- deadline exceeded wraps `ErrTimeout`

---

## 14) Recreate-from-scratch checklist

1. Create module `docpipe`.
2. Implement public types in `types.go`.
3. Implement public interfaces in `interfaces.go`.
4. Implement options and `NewService` with functional options and default wiring.
5. Implement registries.
6. Implement internal staging and canonical doc IO in `internal/docio`.
7. Implement built-in importers/exporters in `internal/importers/*` and `internal/exporters/*`.
8. Add root-level constructor functions for built-ins returning interface types.
9. Implement standalone helpers in `helpers.go`.
10. Implement optional internal packages (frontmatter, clean, renderer, storefs, aiopenai, searchbleve).
