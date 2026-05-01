# docpipe_doku

## Goal
`docpipe` is a Go library for document ingestion and lifecycle management. It imports office/text sources, converts them to canonical Markdown with YAML frontmatter, stores assets in a deterministic layout, renders HTML, supports ZIP import/export, and provides optional AI operations (translate/language-detect).

This document is written as a recreation blueprint: you should be able to rebuild the package from scratch with equivalent behavior.

## High-Level Architecture
Core package: `docpipe`

Subpackages:
- `store`: storage abstraction + filesystem implementation
- `search`: search abstraction + Bleve implementation + markdown sectioning
- `ai`: generic AI client interface + OpenAI Responses client
- `clean`: markdown/html normalization helpers
- `internal/pathutil`: path safety guards
- `internal/tools`: text/slug helpers used by search/cleanup

Main orchestration type:
- `Service` in `service.go`

Service responsibilities:
- Import document sources (`.docx`, `.pptx`, `.md/.markdown/.txt`, `.zip`)
- Persist canonical store layout
- Read/update markdown and frontmatter
- Render HTML and heading tree
- Clean text and run AI operations
- Export back to canonical ZIP

## Canonical Data Layout
Per document ID (`doc.ID`) in the configured store:
- `root.md`: primary markdown file (always includes frontmatter after import/write)
- `media/*`: extracted/attached images (optional)
- `slides/*`: PPTX slide screenshots (optional)
- `versions/*`: archived historical markdown versions (optional)
- `original/*`: original imported source file

Default paths are defined by `DefaultPaths()` and overridable via `Service.Paths`.

## Public Data Types
### Service and config
- `type Service struct`
  - `Store store.Store`
  - `Search search.SearchProvider`
  - `Paths Paths`
  - `Import struct { IncludeImages, IncludeSlides bool; MaxBytes int64; TempDir string }`

- `type Paths struct`
  - `RootMarkdown`, `MediaDir`, `SlidesDir`, `VersionsDir`, `OriginalDir`

- `type UpdateOptions struct`
  - `ArchivePrevious bool`
  - `BumpVersion bool`
  - `Now func() time.Time`

- `type ImportSource struct`
  - `Reader io.Reader`
  - `Name string`
  - `Size int64`
  - `MimeType string`
  - `ModTime time.Time`

- `type Document struct { ID string }`

### Markdown + metadata
- `type Markdown struct`
  - `Full string`
  - `Body string`
  - `Frontmatter Frontmatter`
  - `HasFrontmatter bool`

- `type Frontmatter struct`
  - `Author, Title, Subtitle, Date, ChangedDate, OriginalDocument, OriginalFormat, Version, Language, Abstract string`
  - `Keywords []string`

### Rendering
- `type RenderOptions struct`
  - `AnchorifyHeadings bool`
  - `RewriteImageURLs func(orig string) (string, bool)`
  - `SplitSections bool`

- `type Rendered struct`
  - `TitleHTML`, `FrontmatterHTML`, `BodyHTML string`

- `type HeadingNode struct`
  - `Level int`
  - `Text string`
  - `AnchorID string`
  - `Children []HeadingNode`

### Search package
- `type SearchProvider interface { Upsert; Delete; Search }`
- `type SearchDocument`, `SearchSection`, `SearchHit`, `SearchQuery`

### AI package
- `type Client interface { Generate(ctx, instructions, input string) (string, error) }`

## Public Service API and Behavior
Constructors:
- `NewService(st store.Store, sp search.SearchProvider) Service`
  - defaults: `Import.IncludeImages=true`, `IncludeSlides=true`, `MaxBytes=512MiB`

Utility:
- `Doc(id string) Document`: trims whitespace only

Read/write:
- `ReadMarkdownParts(ctx, doc)`
  - reads `root.md`
  - parses frontmatter (parse errors produce zero-value fm but body/full still returned)
- `WriteMarkdown(ctx, doc, root, opt)`
  - optionally archives old `root.md`
  - optionally bumps version/date metadata
- `WriteFrontmatter(ctx, doc, fm, opt)`
  - merges provided fields into existing frontmatter
  - preserves markdown body

Assets:
- `ListMedia`, `OpenMedia`
- `ListSlides`, `OpenSlide`
- `Open*` rejects traversal (`..`, absolute, invalid prefixes)

Import/export:
- `ImportDocument(ctx, src)`
  - creates new UUID document
  - extension from filename, else MIME fallback
  - supported: `.docx`, `.pptx`, `.md`, `.markdown`, `.txt`, `.zip`
- `ImportZip(ctx, readerAt, size)`
- `ImportZipInto(ctx, doc, readerAt, size)`
  - requires exactly one top-level root markdown file
  - imports only root/media/slides/versions entries
  - resets existing document content first
- `ExportZip(ctx, doc, zipWriter)`
  - emits `root.md` + all files under media/slides/versions if present

Rendering:
- `RenderHTML(ctx, doc, opt)`
  - simple markdown-to-HTML renderer (headings/lists/paragraphs/inline/link/image/code)
- `HeadingIndex(ctx, doc, maxLevel)`
  - extracts heading tree up to level 1..6
  - duplicate headings get suffixed anchors (`intro`, `intro-2`)

Cleanup + AI:
- `Clean(ctx, doc, upd)`
  - normalizes body heuristically into markdown structure
- `Translate(ctx, doc, client, targetLang, rephrase, upd)`
  - full-doc translation prompt, preserves structure constraints
  - writes merged frontmatter and normalizes `language`
- `DetectLanguage(ctx, doc, client)`
  - asks AI for ISO-639-1 lower-case two-letter code

Directory listing:
- `ListDir(ctx, dir)` proxies to store root/subdir listing.

## Import Pipelines
### Staging (`stageImportSource`)
All imports first stream to temp file:
- validates context and reader
- enforces size limits from declared `src.Size` and actual copied bytes
- preserves extension in temp filename when possible
- fsync + seek to beginning

### DOCX (`convertDocx`)
- requires `pandoc` in PATH
- extracts Office core metadata from `docProps/core.xml`
- command: `pandoc <file> -t gfm --wrap=none [--extract-media=<tmp>]`
- optional media extraction into `media/*`
- runs `clean.Normalize(..., CleanTables=true)`
- writes composed root markdown + attaches original `.docx`

### PPTX (`convertPptx`)
- requires `pptx2md` in PATH
- runs in temp dir and reads generated markdown file name from metadata (`<Title>_<lang>_v<version>.md`)
- optional image extraction to `media/*`
- optional slide screenshots:
  - Windows: PowerPoint COM via generated PowerShell script
  - Linux: LibreOffice/soffice headless PNG export with slide-count verification
- appends `[Slide screenshot](slides/slide-XYZ.png)` links to markdown
- normalizes with `clean.Normalize(..., CleanTables=true)`
- attaches original `.pptx`

### Markdown/text (`convertMarkdownFile`)
- reads file as-is
- parses frontmatter if present, else uses defaults from filename/modtime
- ensures required defaults and composes canonical frontmatter
- attaches original file bytes

### ZIP import rules
- entry names are cleaned and traversal-protected
- each entry max size is 512MiB
- exactly one top-level markdown root is required (`*.md` or `*.markdown` at zip root)

## Frontmatter Rules
Core functions in `frontmatter.go`:
- `parseFrontmatter`, `stripFrontmatter`, `hasFrontmatter`
- `mdComposeMarkdownWithMeta`: always writes normalized frontmatter block
- `mdDefaultFrontmatter`: defaults from source filename and modtime
- `mdEnsureFrontmatterDefaults`: fills missing title/original/date/version/language

Normalization:
- Language: maps common names (`english`->`en`, `german/deutsch`->`de`), otherwise `xx` fallback for invalid patterns
- Version: expects dotted numeric forms (`1`, `1.0`, `1.2.3`)
- Date output format: `2006-01-02 15:04:05`
- Original path normalized to `document/<basename>`

Version bump (`WriteMarkdown` with `BumpVersion`):
- minor/last numeric segment increment
- sets `ChangedDate` to current UTC time
- preserves/infers missing root metadata from previous frontmatter

Archiving (`ArchivePrevious`):
- writes previous markdown into `versions/<UTC timestamp>_v<version>.md`

## Store Abstraction
`store.Store` contract:
- `Open`, `ListDir`, `ReadDir`, `WriteFile`, `MkdirAll`, `Remove`

Filesystem implementation (`store/FS`):
- root at `BasePath` (default `.`)
- strong path cleaning via `internal/pathutil`
- `Remove` uses `os.RemoveAll` and ignores not-exist

Path safety (`internal/pathutil`):
- doc IDs must be single path segment, no slash/colon
- names/dirs reject absolute/traversal patterns

## Search Module
`search/BleveSearch` indexes one row per markdown section.

Behavior:
- `Upsert`: deletes existing rows for `doc_id`, splits markdown via `SplitMarkdownToSearchSections`, indexes each section
- `Delete`: query by `doc_id`, delete all hits
- `Search`: full-text over section content/title + doc title with boosts, optional exact filters

Section splitter (`search/tools.go`):
- splits on `#`, `##`, `###`
- introduction before first heading becomes `Introduction`
- no headings => single `Content` section

## Rendering Engine (Built-in)
Renderer in `render.go` is custom (not CommonMark-complete):
- block support: headings, paragraphs, ordered/unordered lists, fenced code blocks
- inline support: links, images, emphasis, strong, inline code
- optional heading IDs and image URL rewrite hook
- heading extraction ignores code-fence content

## AI Client Module
`ai/ChatGPTClient` uses OpenAI Responses endpoint:
- defaults:
  - `BaseURL`: `https://api.openai.com/v1`
  - `Model`: `gpt-5.4-mini`
  - HTTP timeout: 2 minutes
- env constructor supports:
  - `OPENAI_API_KEY` (required for requests)
  - `OPENAI_MODEL`
  - `OPENAI_BASE_URL`

Request payload:
- `model`, `instructions`, `input`, optional `max_output_tokens`

Response handling:
- prefers `output_text`
- fallback to concatenated `output[].content[type=output_text]`
- surfaces refusal and API errors with parsed message

## Error Model
Sentinel errors in `errors.go`:
- `ErrInvalidInput`
- `ErrUnsupported`
- `ErrAIUnavailable`
- `ErrTimeout`
- `ErrToolMissing`

Common patterns:
- invalid service/doc args => `ErrInvalidInput`
- missing external tools => wrapped `ErrUnsupported` + `ErrToolMissing`
- AI client nil/invalid outputs => `ErrAIUnavailable`
- external command deadline exceeded => `ErrTimeout`

## External Runtime Requirements
For full format support:
- DOCX import: `pandoc`
- PPTX import text/images: `pptx2md`
- PPTX slide screenshots:
  - Windows: PowerPoint COM + `powershell`/`pwsh`
  - Linux: `libreoffice` or `soffice`

Without these tools, related features fail with wrapped unsupported/tool-missing errors.

## Recreate-From-Scratch Plan
1. Create module `github.com/archeopternix/docpipe` with package layout listed above.
2. Implement `store.Store` and `store.FS` with strict path normalization.
3. Implement `Service` with default path/import config and validation helpers.
4. Implement frontmatter parsing/composition and normalization helpers.
5. Implement import staging and persistence (`importedDocument`, reset-and-write semantics).
6. Implement format converters:
   - DOCX via pandoc
   - PPTX via pptx2md (+ optional screenshots)
   - markdown/text direct conversion
7. Implement ZIP helper functions and ZIP import/export invariants.
8. Implement markdown renderer and heading index extraction.
9. Implement clean/translate/detect language service methods.
10. Implement `search` package abstraction and Bleve adapter.
11. Implement `ai` package client + OpenAI Responses integration.
12. Add tests mirroring `service_test.go` and `ai/chatgpt_test.go` behavior.

## Behavior Notes from Current Repository
- `Service.Search` is injected but not yet used by `Service` methods.
- `search/service_search_bleve.go` currently contains format-string defects that break `go test ./...` build.
- Some service tests currently fail in the checked-in state; this documentation describes intended behavior from code + tests, not a fully green branch snapshot.
