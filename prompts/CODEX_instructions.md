### CODEX instructions (implementation plan)

Scope: apply the user-approved changes to the Go package `docpipe` across the attached files (`markdown.go`, `markdown_docx.go`, `markdown_pptx.go`, `markdown_clean.go`, `markdown_md.go`, `errors.go`).

---

## 1) Normalize media map keys (Finding **1.3**)

**Goal:** Store extracted images consistently as **`"media/foo.png"`** keys everywhere, and when writing ZIP entries, **write entries “as-is”** (do not re-prefix).

### 1.3.a — Standardize internal conventions
- `Markdown.extractedImages` keys must always be a forward-slash path rooted at `media/`, e.g.:
  - `media/image-001.png`
  - `media/subdir/x.png` (if you ever keep subdirs)
- `Markdown.extractedSlides` keys must likewise be `slides/slide-001.png` 

### 1.3.b — Update writers (ZIP creation)
In `(*Markdown).CreateZipBytes` (`markdown.go`):
- Replace logic that prefixes `media` and `slides` using `markdownPrefixedEntryName(...)`.
- Instead: write the entry using the key exactly.

**Change:**
- Before:
  - `markdownWriteZipEntry(writer, markdownPrefixedEntryName("media", name), ...)`
- After:
  - `markdownWriteZipEntry(writer, filepath.ToSlash(name), ...)`

Do the same for slides and versions.

### 1.3.c — Update all producers/consumers of extractedImages
- **DOCX importer** (`ParseWordFile`): currently writes `doc.extractedImages["media/.."]` already—keep it, but ensure it is *always* `media/...` and never bare filenames.
- **PPTX importer**: currently uses bare relpaths from the `media` dir (`doc.extractedImages[relPath]`).
  - Update it to store `doc.extractedImages["media/"+filepath.ToSlash(relPath)]`.
- **ZIP importer** (`ParseZipFile`): currently stores media keys as *without* prefix (`TrimPrefix("media/")`).
  - Update it to store the full cleaned entry name, i.e. `doc.extractedImages[name]` where `name` is `media/...`.

### 1.3.d — Remove / deprecate helper(s) if no longer needed
- `markdownPrefixedEntryName` may become unused. Remove it if no longer referenced.
- Keep `markdownZipCleanEntryName` as the security guardrail.

---

## 2) External tool execution hardening (Finding **2.1**)

You asked for three things:
1) `exec.LookPath` for `pandoc` / `pptx2md` before running  
2) Adjust Parse* calls to accept a context / timeout policy  
3) Use `exec.CommandContext` for `pandoc`, `pptx2md`, `soffice/libreoffice`

### 2.1.a — Introduce context-aware Parse APIs
Add new signatures and do not change existing signatures (write a wrapper to the new functions):

- `ParseWordFileContext(ctx context.Context, path string, params *WordParams) (*Markdown, error)`
- `ParsePowerPointFileContext(ctx context.Context, path string, params *PowerPointParams) (*Markdown, error)`
- `ParseMarkdownFileContext(ctx context.Context, path string, params *PowerPointParams) (*Markdown, error)`

Then:
- Keep existing `ParseMarkdownFile` / `ParseWordFile` / `ParsePowerPointFile` as wrappers calling the context variants with `context.Background()` (or with a default timeout—see next item).

### 2.1.b — Define timeout behavior
Create a central config (choose one of these patterns):


**Option B:** constants in code
- `const defaultExternalToolTimeout = 1 * time.Minute` etc.

Implementation requirement:
- If caller passes a context with deadline, respect it.
- If caller passes a context **without** deadline, wrap with `context.WithTimeout` using the tool-specific default.

### 2.1.c — Add tool availability checks
Before running:
- For DOCX: `exec.LookPath("pandoc")` must succeed; otherwise return a clear error `ErrUnsupported`.
- For PPTX: `exec.LookPath("pptx2md")` must succeed; otherwise return a clear error `ErrUnsupported`.
- For Linux screenshots: check `soffice` or `libreoffice` with `LookPath` (you already do a search; keep it but ensure you return `ErrUnsupported` or a clear message).

Return style:
- Wrap with sentinel errors (see section 4.2) so callers can detect unsupported-tool situations.

### 2.1.d — Use `exec.CommandContext` everywhere
Update command creation sites:

- DOCX conversion (`pandoc`) currently uses `exec.Command("pandoc", args...)`  
  → change to `exec.CommandContext(ctx, "pandoc", args...)`.

- PPTX conversion (`pptx2md`) currently uses `exec.Command("pptx2md", args...)`  
  → change to `exec.CommandContext(ctx, "pptx2md", args...)`.

- PPTX screenshots Linux export calls `exec.Command(command, "--headless", ...)`  
  → change to `exec.CommandContext(ctx, command, ...)` and thread `ctx` down into `pptxExportSlideScreenshots*`.

**Also required:** when a context deadline is hit, return a meaningful error:
- e.g. `fmt.Errorf("%w: pandoc timed out after %s", ErrTimeout, timeout)` or at least wrap `context.DeadlineExceeded`.

---

## 3) OpenAI HTTP handling fixes (Finding **2.2**)

### 2.2.a — Check HTTP status *before* decoding JSON
In `openAIText` (`markdown_clean.go`) currently you decode then check status.

Change flow:
1. `resp, err := client.Do(req)`
2. Read body bytes **once**: `raw, err := io.ReadAll(resp.Body)`
3. If `resp.StatusCode` not in `[200..299]`:
   - Attempt to parse OpenAI error schema from `raw` (best-effort).
   - If parse succeeds and message present: return `fmt.Errorf("%w: openai request failed: %s", ErrAIUnavailable, msg)` (or new error).
   - Else: return `fmt.Errorf("%w: openai request failed: %s: %s", ErrAIUnavailable, resp.Status, string(raw))` (truncate raw to a safe length).
4. If status is 2xx:
   - Decode JSON from `raw` into `openAIResponsesResponse`
   - Validate schema expectations (next).

### 2.2.b — Validate schema on non-2xx
When non-2xx:
- Validate that the body is either:
  - valid JSON with `error.message` (preferred), or
  - otherwise treat as unexpected response and include status + snippet.

When 2xx:
- Ensure you can produce output text by either:
  - `OutputText != ""`, OR
  - collecting `output[].content[].text`.
- If neither yields text, return a structured error like:
  - `fmt.Errorf("openai response did not contain text output")` but add status / maybe response id if present (not currently captured).

---

## 4) Deduplicate cleanup logic (Finding **4.1**)

**Goal:** make `cleanMarkdownContent` the canonical implementation. Remove/replace `officeCleanupMarkdownContent` duplication.

### 4.1.a — Create a shared cleanup entrypoint
In `markdown_clean.go`:
- reuse `cleanMarkdownContent` directly:

```go
func NormalizeMarkdown(input string, cleanTables bool) string {
    return cleanMarkdownContent(input, cleanTables)
}
```

### 4.1.b — Update DOCX and PPTX conversion to use canonical cleanup
- In `ParseWordFile`, after pandoc output:
  - Replace `officeCleanupMarkdownContent(...)` with `cleanMarkdownContent(..., true)`.
- In `ParsePowerPointFile`, replace `officeCleanupMarkdownContent` with `cleanMarkdownContent`.

### 4.1.c — Remove duplicated helpers
After the switch:
- Delete the `office*` cleanup regexes and helpers from `markdown_docx.go` (and any similar copy) if they are no longer used.
- Keep only DOCX-specific things (core properties parsing, pandoc execution, media extraction).

---

## 5) Sentinel errors and consistent wrapping (Finding **4.2**)

**Goal:** callers can reliably distinguish invalid input, unsupported formats/tools, AI availability, and timeouts.

### 4.2.a — Expand `errors.go`
Keep existing:
- `ErrInvalidInput`, `ErrUnsupported`, `ErrAIUnavailable`

Add:
- `ErrTimeout = errors.New("docpipe: timeout")`
- `ErrToolMissing = errors.New("docpipe: required tool missing")` 

### 4.2.b — Wrap errors in key places
Examples of required wrapping patterns:

- Unsupported file extension:
  - `return nil, fmt.Errorf("%w: word conversion not supported for %q", ErrUnsupported, ext)`
- Tool missing:
  - `return nil, fmt.Errorf("%w: pandoc not found in PATH", ErrUnsupported)` (or `ErrToolMissing`)
- AI not active:
  - return `fmt.Errorf("%w: AI is not active", ErrAIUnavailable)` instead of plain `fmt.Errorf`.

### 4.2.c — Update DetectAI / AI flows
`DetectAI` currently returns `(false, nil)` if key missing; ensure a `ErrAIUnavailable` is raised at this time as well for consistent handling.

---

## 6) PPTX screenshots: single LibreOffice export (Finding **3.2**)

**Goal:** On Linux, export **all slides in one LibreOffice invocation**, then collect/rename outputs.

### 3.2.a — Replace per-page conversion loop
In `pptxExportSlideScreenshotsLinux`:
- Remove the `for page := 1; page <= slideCount; page++ { ... }` loop with PageNumber filter.
- Run one command like:

```bash
soffice --headless --convert-to png --outdir <workdir> <sourcePath>
```

### 3.2.b — Map exported PNGs to slide order
After export:
- list PNG files in `workDir`
- sort deterministically:
  - best-effort: sort by name; LibreOffice typically outputs something like `<base>.png`, `<base>1.png`, etc., but it varies.
- rename/copy into `outputDir` as:
  - `slide-001.png`, `slide-002.png`, ...

If reliable ordering is uncertain, implement a fallback:
- still parse slide count from ZIP entries to know expected number.
- if exported count mismatches expected, return an error with actionable message.

### 3.2.c — Thread context into screenshot export
Update `pptxExportSlideScreenshots` and platform-specific functions to accept a `ctx context.Context` and use `exec.CommandContext`.

---

## 7) Stream ZIP reads (Finding **5**)

**Goal:** Implement “streaming file behaviour” for `markdownZipReadFile` to avoid reading entire ZIP entries into memory in one shot.

### 5.a — Decide a strategy
Because the code stores buffers (`*bytes.Buffer`) everywhere, you can’t be fully streaming end-to-end without refactoring the data model. Implement a **bounded streaming read**:

- Replace `io.ReadAll(rc)` with a streaming copy into a buffer using `io.CopyBuffer`, optionally with a limit.

**Minimum acceptable change:**
- use `bytes.Buffer` + `io.Copy` rather than `ReadAll` (still ends in memory, but streams and avoids intermediate allocations).


### 5.b — New helper: read zip entry with limit
Implement:

```go
func markdownZipReadFile(file *zip.File) ([]byte, error)
```

→ becomes:

- open rc
- stream into buffer:
  - `var buf bytes.Buffer`
  - `_, err := io.Copy(&buf, io.LimitReader(rc, max+1))`
- if `buf.Len() > max` return error
- return `buf.Bytes()`

Then:
- use it in `ParseZipFile` and anywhere else.

---

## Acceptance checklist (what CODEX should verify)

1. All `extractedImages` keys are `media/...` across docx, pptx, and zip import.
2. ZIP creation writes media entries exactly as key names (no double prefixing).
3. `pandoc`, `pptx2md`, `soffice/libreoffice` calls:
   - check `LookPath` before run
   - use `CommandContext`
   - respect `context.WithTimeout` defaults if no deadline is provided
4. OpenAI calls:
   - non-2xx: status checked before JSON decoding; schema validated best-effort
   - 2xx: robust text extraction; clear error if missing
5. Cleanup dedup:
   - DOCX/PPTX conversion uses `cleanMarkdownContent`
   - old office cleanup code removed (or fully unused)
6. Errors:
   - exported sentinel errors used consistently (`%w`)
7. ZIP reads:
   - `markdownZipReadFile` uses streaming copy and (ideally) size limit
