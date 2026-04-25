# CODEX Implementation Plan — Incremental ZIP access + HTML rendering + versioned patching (`docpipe`)

## Goal
Extend `docpipe` so a webserver can:

- Open a document **ZIP** without loading all assets.
- List/read assets (images/slides) **on demand**.
- Render Markdown to HTML with deterministic anchors and image URL rewriting.
- Provide frontmatter/body accessors.
- Produce a heading index tree (H1–H3).
- Update markdown/meta with **versioning** by **patching** an existing ZIP (stream copy-through + replace only changed entries).

This plan assumes the current ZIP layout described in the requirements:

- Root current markdown file (e.g. `document.md` or similar)
- `media/...`
- optional `slides/...`
- `document/...` original source
- `versions/...` archived markdown versions


---

## Deliverables

1. New public APIs (exact signatures as requested).
2. New internal ZIP patching mechanism (streaming, no full-load).
3. Deterministic anchor + heading index implementation shared by HTML rendering and `/index`.


---

## Proposed file/module layout (Go)

Add new files (suggested):

- `zip.go` — implements `OpenZip` and zip index caching; ListImages/ListSlides/ImageReader/SlideReader/Get*Bytes ; PatchZip primitive (copy-through + replacements)
- `render.go` — RenderHTML + helpers; GetMarkdownBody/SetFrontmatter/RenderFrontmatter; HeadingIndex + anchor generation
- `markdown.go` — `changeMarkdown` orchestration (versioning + patch)

Keep existing files (`markdown.go`, `markdown_clean.go`, `markdown_md.go`, …) untouched.


---

## Data structure changes

### Extend `Markdown` struct (unexported fields)
Add fields so `OpenZip` can be lazy:

- ZIP backing:
  - `zipr *zip.Reader`
  - `zipIndex map[string]*zip.File` (name → entry pointer)
  - `zipSize int64` (needed if reader requires size; see notes)

---

## API Additions — required public functions

> Implement exactly these signatures.

### 1) Lazy open
```go
// create a Markdown structure, but just read the Metadata, the other assets are read when needed
func OpenZip(r io.ReaderAt) (*Markdown, error)
```

**Implementation notes**
- `archive/zip.NewReader` requires `(r io.ReaderAt, size int64)`.
- Since the signature omits size, implement `OpenZip` using one of these strategies:
  1) **If `r` is also `io.Seeker`**: determine size via `Seek(0, io.SeekEnd)`.
  2) Otherwise: introduce an internal adapter that requires callers (webserver) to pass a `ReaderAt` that also provides size (e.g. `*os.File` does). Detect and error if size cannot be determined.

**Plan**
- Implement internal:
  - `openZipWithSize(r io.ReaderAt, size int64) (*Markdown, error)`
  - `readerAtSize(r io.ReaderAt) (size int64, ok bool)` — attempts `Stat()` or `Seek` if available.
- `OpenZip(r)` calls `readerAtSize` and then `zip.NewReader`.

**Behavior**
- Build `zipIndex` by iterating `zipr.File` once (metadata only).
- Locate current markdown file:
  - it is the only .md file in the root of the zip
- Load **frontmatter only** from current markdown entry.


### 2) List assets without reading full content
```go
func (m *Markdown) ListImages(prefix string) ([]string, error) // e.g. "media/" dont read the full Content just needed info
func (m *Markdown) ListSlides(prefix string) ([]string, error) // e.g. "slides/" dont read the full Content just needed info
```

**Implementation**
- Iterate `zipIndex` keys (or `zipr.File`) and filter:
  - `strings.HasPrefix(name, prefix)`
  - Exclude directories (`strings.HasSuffix(name, "/")`)
  - Optionally filter extensions for images: `.png .jpg .jpeg .gif .svg .webp`
- Return sorted list for determinism.


### 3) Read assets on demand
```go
// Reads the identified image from ZIP file into markdown structure
func (m *Markdown) ImageReader(assetPath string) (io.ReadCloser, error)
// Reads the identified Slide from ZIP file into markdown structure
func (m *Markdown) SlideReader(assetPath string) (io.ReadCloser, error)

func (m *Markdown) GetImageBytes(assetPath string) (*bytes.Buffer, error) // convenience
func (m *Markdown) GetSlideBytes(assetPath string) (*bytes.Buffer, error) // convenience
```

**Implementation**
- `ImageReader` / `SlideReader` delegate to a shared internal:
  - `assetReader(assetPath string) (io.ReadCloser, error)`
- Validate path:
  - Must not contain `..` segments
  - Must exist in `zipIndex`
  - Must be within expected folder (`media/` for images, `slides/` for slides) unless you explicitly allow any path
- `Get*Bytes` uses `io.Copy` into a `bytes.Buffer`.


---

## HTML Rendering

### Required types
```go
type RenderOptions struct {
    AnchorifyHeadings bool
    RewriteImageURLs  func(orig string) (string, bool) // return (newURL, changed)
    SplitSections     bool // Title + Frontmatter + Body
}

type Rendered struct {
    TitleHTML       string
    FrontmatterHTML string
    BodyHTML        string
}

func (m *Markdown) RenderHTML(opt RenderOptions) (Rendered, error)
```

### Implementation plan

1. **Parse markdown** into an AST (recommended: `goldmark` + extensions). If you prefer stdlib-only, you can do a simpler renderer but anchors + image rewrites are much easier with a real AST.
2. If `SplitSections` is true:
   - Title comes from `MetaData.Title`.
   - Frontmatter rendered as HTML-friendly key/value list (see `RenderFrontmatter`).
   - Body rendered from markdown body (frontmatter stripped).
3. If `AnchorifyHeadings` is true:
   - Generate deterministic anchor IDs using a shared `slugify` function.
   - Ensure uniqueness within a document by suffixing `-2`, `-3`, … when duplicates occur.
4. Image rewriting:
   - For each image node, call `RewriteImageURLs` for the destination URL.

### Deterministic anchor algorithm (shared)
- Input: heading text
- Normalize:
  - trim
  - lowercase
  - replace whitespace with `-`
  - remove punctuation except `-` and `_`
  - optionally transliterate non-ascii to ascii
- Deduplicate:
  - first occurrence: `id`
  - subsequent: `id-2`, `id-3`

**Important:** use the same function in `HeadingIndex()`.


---

## Frontmatter helpers

### Required public methods
```go
func (m *Markdown) RenderFrontmatter() (MetaData, error) // Returns the current frontmatter rendered HTML friendly
func (m *Markdown) GetMarkdownBody() (string, error)     // return markdown without frontmatter

// sets the new frontmatter, and calls changeMarkdown
func (m *Markdown) SetFrontmatter(md MetaData) error     // update metadata (and re-serialize frontmatter)
```

### Implementation plan

- `GetMarkdownBody()`
  - If markdown not loaded: stream-read current markdown entry.
  - Strip frontmatter:
    - Support `---\n...\n---\n` at top (YAML style)
    - If no frontmatter, return full content

- `RenderFrontmatter()`
  - Ensure metadata is loaded.
  - Return `MetaData` (caller renders). Despite comment "rendered HTML friendly", returning `MetaData` is fine; ensure strings are sanitized/escaped in the web layer.

- `SetFrontmatter(md)`
  - Load existing markdown body.
  - Load metadata 
  - Serialize md to metadata
  - Compose new markdown = frontmatter + body.
  - Call `changeMarkdown(newMarkdown)`.


---

## Heading index extraction

### Required types/method
```go
type HeadingNode struct {
    Level    int    // 1..6
    Text     string
    AnchorID string
    Children []HeadingNode
}

func (m *Markdown) HeadingIndex(maxLevel int) ([]HeadingNode, error) // maxLevel=3
```

### Implementation plan

- Parse markdown body to AST.
- Collect headings with level <= maxLevel.
- For each heading:
  - Extract text content
  - Generate `AnchorID` using the same deterministic slug function used in `RenderHTML`.
- Build a tree using a stack:
  - Maintain last node per level
  - Attach as child to nearest prior heading of lower level


---

## Internal ZIP patching + versioning

### Required internal funcs
```go
// Move the current Markdown into Archive, increase the minor version, add the frontmatter to the new new markdown and save this without loading the full ZIP file (call PatchZip)
func (m *Markdown) changeMarkdown(md string) (versionID string, err error)

// PatchZip streams the old zip Content (media, Version, original, slides..) updates Meta, Markdown and writes to new zip. deletes the old zip and renames the new one to the Name of the old
func (m *Markdown) PatchZip(archivepath string)
```

### Design: do not modify ZIP in place
ZIP central directory is written at the end. “Patch” means:

- open old zip for reading
- create new zip for writing
- copy all entries unchanged **except** those being replaced
- write replacement/new entries
- close, then atomic rename

### PatchZip implementation (core)
Implement internal helper:

- `patchZipFile(srcPath string, replacements map[string]func() (io.ReadCloser, *zip.FileHeader, error)) error`

Rules:
- Copy-through uses streaming (`f.Open()` + `io.Copy`).
- For replacements:
  - root markdown path replaced with new markdown content
  - add `versions/<versionID>.md` with archived old markdown
  - (optional) update metadata-only files if you introduce them later

### `changeMarkdown(md string)` workflow

1. Load current markdown entry **only** (stream read).
2. Derive `versionID`:
   - parse existing frontmatter `Version` (e.g. `1.4`)
   - bump minor: `1.(minor+1)`
   - or if empty: start `1.0` then bump to `1.1` on first change
   - embed new version into frontmatter
3. Create archive entry name:
   - `versions/<timestamp>_v<oldVersion>.md` OR `versions/v<oldVersion>.md` (pick one and standardize)
4. Prepare `replacements`:
   - replace current markdown entry with `md` (with updated frontmatter)
   - add archive entry with previous markdown bytes
5. Call `PatchZip` (or internal patch helper).

### `PatchZip(archivepath string)` specifics

- Input `archivepath` = existing zip file path.
- Output writes to temp file in same directory:
  - `<archivepath>.tmp.<pid>.<rand>`
- After write/close:
  - `os.Rename(tmp, archivepath)`

**Concurrency**
- Caller (webserver) must lock per document ID to avoid concurrent patching.


---

## Integration points with existing API

- Update `CleanUpMarkdown`:
  - after producing cleaned markdown string, call `changeMarkdown(cleaned)`
- Update `TranslateTo`:
  - after translation, call `changeMarkdown(translated)`
- Update any “save markdown” or “save meta” helper to use `changeMarkdown`.

This guarantees the versioning rule automatically.



---

## Notes / open decisions

1. **ZIP size discovery for `OpenZip(io.ReaderAt)`**
   - If you cannot reliably infer size, consider documenting that `OpenZip` expects `*os.File` or any `ReaderAt` that also supports `Stat()` or `Seek`.

2. **Current markdown filename**
   - Define a stable name in the ZIP (recommended), e.g. `document.md`.
   - Otherwise implement heuristics but that’s brittle.

3. **Frontmatter format**
   - Standardize YAML keys to match `MetaData` fields.

4. **Security**
   - Asset path validation prevents traversal-like behavior even though it’s inside zip.


---

## Work breakdown (suggested sequencing)

1. Implement `OpenZip` + zip index + current markdown discovery.
2. Implement `GetMarkdownBody` + frontmatter parse/serialize.
3. Implement `ListImages/ListSlides` + `ImageReader/SlideReader`.
4. Implement deterministic anchor generator.
5. Implement `HeadingIndex`.
6. Implement `RenderHTML` with image rewrite hook.
7. Implement `zip_patch.go` streaming patch primitive.
8. Implement `changeMarkdown` + wire into cleanup/translate/meta/save flows.
9. Add tests + golden test fixtures (sample zips).

