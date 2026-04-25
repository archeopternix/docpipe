### CODEX implementation plan 

#### Objective
1) Move all markdown cleaning/normalizing code out of the public API into `internal/cleaner`.
2) Introduce one canonical ZIP path sanitizer based on ZIP semantics and use it everywhere (`indexing`, `patching`, `listing`, `reading`).
3) Shrink `docpipe`’s exported surface (remove `NormalizeMarkdown` from public API).

---

## Phase 1 — Create `internal/cleaner` and rewire callers

1. **Create package**: `internal/cleaner/cleaner.go`
   - Define:
     - `type Options struct { CleanTables bool }`
     - `func Normalize(markdown string, opt Options) string`

2. **Move code** from `markdown_clean.go`:
   - Move the deterministic normalization pipeline + helpers into `internal/cleaner`:
     - regex patterns
     - HTML stripping + table conversion
     - image path normalization (`/media/...`)
     - escaped character unescaping
   - Keep OpenAI-related code (`DetectAI`, `openAIText`, AI prompts, `LanguageDetectionAI`, `TranslateTo`) in `docpipe` (for now).

3. **Delete/privatize exported function**:
   - Remove `func NormalizeMarkdown(input string, cleanTables bool) string` from `docpipe` (exported API).
   - If needed for internal call sites, replace with private wrapper:
     - `func normalizeMarkdown(input string, cleanTables bool) string { return cleaner.Normalize(input, cleaner.Options{CleanTables: cleanTables}) }`

4. **Update call sites**:
   - In `ParseWordFileContext` and `ParsePowerPointFileContext`, replace `NormalizeMarkdown(...)` usage with `cleaner.Normalize(...)`.
   - In `(*Markdown).CleanUpMarkdown`, replace `cleanMarkdownContent(...)` usage with `cleaner.Normalize(...)`.

---

## Phase 2 — Add canonical ZIP path sanitizer

5. **Create package**: `internal/bundlepath/path.go`
   - Implement:
     - `func Clean(name string) (string, bool)` using:
       - `strings.TrimSpace`
       - `strings.ReplaceAll(name, "\\", "/")`
       - trim leading `/`
       - `path.Clean`
       - reject `""`, `"."`, `".."`, any traversal (`../`, `/../`, segment `..`)
     - (Optional) `func CleanDirPrefix(prefix string, fallback string) string` to normalize prefixes for listing (ensures trailing `/`, returns `""` for invalid).

6. **Replace both existing cleaners**:
   - `markdownZipCleanEntryName` becomes a wrapper around `bundlepath.Clean`.
   - Replace `markdownZipNormalizeAssetPath` with a new helper that:
     - sanitizes via `bundlepath.Clean`
     - rejects directories (trailing `/`)
     - enforces `folder/` prefix (`media/` or `slides/`)

---

## Phase 3 — Apply sanitizer everywhere ZIP names are handled

7. **Indexing (`OpenZip`)**
   - When iterating `zr.File`, sanitize `file.Name` via `bundlepath.Clean` and use that as the map key.
   - Root markdown detection operates on sanitized names only.

8. **Reading assets (`assetReader`, `currentMarkdownBytes`)**
   - Ensure any lookup into `zipIndex` uses sanitized names produced by the same sanitizer (no ad-hoc cleaning).

9. **Listing (`ListImages`, `ListSlides`)**
   - Normalize `prefix` using ZIP-semantics helper (no `filepath.Clean`).
   - Ensure comparisons use sanitized entry names.

10. **Patching (`patchZipFile`, `PatchZip`, `markdownRootNameInZip`)**
   - Sanitize names from `reader.File` before comparing against `removeNames` / `replacements`.
   - Sanitize replacement keys before writing.

---

## Phase 4 — Public API cleanup (minimal breaking surface)

11. **Update `doc.go` package docs**
   - Remove mention of `NormalizeMarkdown` as a public entrypoint.
   - Document that cleaning is performed via `(*Markdown).CleanUpMarkdown`.
