# Universal Document Processing API — Architecture Concept

## Overview

A Go library that accepts documents (Word, PowerPoint, Markdown, Text) as `bytes.Buffer`, converts them to Markdown, and runs a blocking pipeline of processing steps (clean → transform → translate → detect language). Each session is independent and can run in parallel.

---

## 1. Core Design Principles

- **Pipeline pattern** — each step is a `Processor` interface, composable and ordered
- **Session-based** — each document submission creates an isolated `Session` with its own state
- **Blocking per session, concurrent across sessions** — callers block until their pipeline completes; many sessions run simultaneously via goroutines
- **`bytes.Buffer` in / `bytes.Buffer` out** — every step reads and returns `bytes.Buffer`
- **Converter registry** — pluggable converters per MIME/file type

---

## 2. Package Structure

```
docpipe/
├── docpipe.go            // Public API: Engine, Session, Submit()
├── converter/
│   ├── converter.go      // Converter interface + registry
│   ├── docx.go           // Word → Markdown
│   ├── pptx.go           // PowerPoint → Markdown
│   ├── markdown.go       // Passthrough (already MD)
│   └── text.go           // Plain text → Markdown
├── processor/
│   ├── processor.go      // Processor interface + Chain
│   ├── cleaner.go        // Markdown cleanup (strip bad HTML, normalize)
│   ├── transformer.go    // Structural transforms (heading levels, TOC, etc.)
│   ├── translator.go     // Translation via pluggable backend
│   └── langdetect.go     // Language detection, attaches metadata
├── session/
│   └── session.go        // Session state, result, metadata
├── options.go            // Functional options for Engine & processors
└── errors.go             // Sentinel errors
```

---

## 3. Key Interfaces & Types

### 3.1 Converter

```go
// converter/converter.go

type Format string

const (
    FormatDOCX     Format = "docx"
    FormatPPTX     Format = "pptx"
    FormatMarkdown Format = "markdown"
    FormatText     Format = "text"
)

// Converter turns a raw document into Markdown.
type Converter interface {
    // Convert reads raw document bytes and returns Markdown bytes.
    Convert(ctx context.Context, src *bytes.Buffer) (*bytes.Buffer, error)
    // Supports returns the format this converter handles.
    Supports() Format
}

// Registry maps formats to converters.
type Registry struct {
    mu         sync.RWMutex
    converters map[Format]Converter
}

func NewRegistry() *Registry
func (r *Registry) Register(c Converter)
func (r *Registry) Get(f Format) (Converter, error)
```

### 3.2 Processor

```go
// processor/processor.go

// Processor performs a single processing step on Markdown content.
type Processor interface {
    // Name returns a human-readable identifier for logging/metrics.
    Name() string
    // Process takes Markdown in, returns processed Markdown out.
    Process(ctx context.Context, in *bytes.Buffer, meta *Metadata) (*bytes.Buffer, error)
}

// Metadata carries accumulated state across the pipeline.
type Metadata struct {
    SourceFormat   converter.Format
    DetectedLang   string            // ISO 639-1, set by LangDetector
    TargetLang     string            // desired translation target
    Custom         map[string]any    // extensible
}

// Chain executes processors sequentially.
type Chain struct {
    processors []Processor
}

func NewChain(ps ...Processor) *Chain
func (c *Chain) Run(ctx context.Context, in *bytes.Buffer, meta *Metadata) (*bytes.Buffer, error)
```

`Chain.Run` iterates through processors, feeding each output as the next input — fully blocking.

### 3.3 Session

```go
// session/session.go

type Status int

const (
    StatusPending Status = iota
    StatusRunning
    StatusDone
    StatusFailed
)

type Result struct {
    Output   *bytes.Buffer
    Metadata *processor.Metadata
    Err      error
    Duration time.Duration
    Steps    []StepResult // per-step timing & status
}

type StepResult struct {
    Name     string
    Duration time.Duration
    Err      error
}

type Session struct {
    ID        string
    Status    Status
    input     *bytes.Buffer
    format    converter.Format
    result    *Result
    mu        sync.Mutex
}
```

### 3.4 Engine (Top-Level API)

```go
// docpipe.go

type Engine struct {
    registry    *converter.Registry
    chain       *processor.Chain
    maxParallel int              // semaphore width
    sem         chan struct{}
}

func New(opts ...Option) *Engine

// Submit is the main entry point. It blocks until the pipeline finishes
// and returns the final Markdown as a bytes.Buffer.
func (e *Engine) Submit(
    ctx context.Context,
    format converter.Format,
    input *bytes.Buffer,
) (*session.Result, error)

// SubmitAsync returns immediately with a Session handle.
// The caller can poll or wait on the session.
func (e *Engine) SubmitAsync(
    ctx context.Context,
    format converter.Format,
    input *bytes.Buffer,
) *session.Session
```

---

## 4. Pipeline Flow

```
┌──────────┐     ┌───────────┐     ┌─────────┐     ┌─────────────┐     ┌────────────┐     ┌────────────┐
│  Client   │────▶│  Engine   │────▶│ Convert │────▶│   Clean     │────▶│ LangDetect │────▶│ Transform  │
│ (Buffer)  │     │  .Submit()│     │ →  MD   │     │  (sanitize) │     │ (metadata) │     │(restructure)│
└──────────┘     └───────────┘     └─────────┘     └─────────────┘     └────────────┘     └────────────┘
                                                                                                │
                       ┌──────────┐◀────────────────────────────────────────────────────────────┘
                       │ Translate │
                       │(optional) │
                       └─────┬────┘
                             ▼
                      Result{Buffer, Metadata}
```

Each arrow is a `*bytes.Buffer` handoff. The entire flow blocks within one goroutine per session.

---

## 5. Concurrency Model

```go
// Engine.Submit — blocking with bounded parallelism
func (e *Engine) Submit(ctx context.Context, f converter.Format, in *bytes.Buffer) (*session.Result, error) {
    // Acquire semaphore slot (respects ctx cancellation)
    select {
    case e.sem <- struct{}{}:
        defer func() { <-e.sem }()
    case <-ctx.Done():
        return nil, ctx.Err()
    }

    // 1. Convert
    conv, err := e.registry.Get(f)
    if err != nil {
        return nil, fmt.Errorf("no converter for %s: %w", f, err)
    }
    md, err := conv.Convert(ctx, in)
    if err != nil {
        return nil, fmt.Errorf("conversion failed: %w", f, err)
    }

    // 2. Run processor chain (clean → detect → transform → translate)
    meta := &processor.Metadata{SourceFormat: f}
    out, err := e.chain.Run(ctx, md, meta)

    return &session.Result{Output: out, Metadata: meta, Err: err}, err
}
```

Callers wanting parallelism simply call `Submit` from multiple goroutines — the semaphore bounds concurrency:

```go
var wg sync.WaitGroup
for _, doc := range documents {
    wg.Add(1)
    go func(d Document) {
        defer wg.Done()
        result, err := engine.Submit(ctx, d.Format, d.Buffer)
        // handle result
    }(doc)
}
wg.Wait()
```

---

## 6. Functional Options

```go
// options.go

type Option func(*Engine)

func WithMaxParallel(n int) Option           // default: runtime.NumCPU()
func WithConverter(c converter.Converter) Option
func WithProcessors(ps ...processor.Processor) Option
func WithTranslationBackend(t translator.Backend) Option
```

---

## 7. Processor Implementations

| Processor | Responsibility | Key Fields |
|---|---|---|
| `Cleaner` | Strip invalid HTML, normalize whitespace, fix broken links, remove zero-width chars | `rules []CleanRule` |
| `LangDetector` | Detect language, write to `meta.DetectedLang` | detector backend (e.g. lingua-go) |
| `Transformer` | Remap heading levels, inject/strip TOC, normalize bullet styles | `TransformConfig` |
| `Translator` | Translate MD content, skip code blocks/URLs | backend interface, `targetLang` |

Each implements `Processor` and is independently testable.

---

## 8. Error Handling Strategy

- Each `Processor` returns `(*bytes.Buffer, error)` — on error the chain **short-circuits** and the partial result plus error propagate up.
- `converter.ErrUnsupportedFormat` — sentinel for unknown format.
- `processor.ErrLangDetectionFailed` — non-fatal; chain can continue with empty `DetectedLang` if configured with `ContinueOnDetectFailure`.
- Context cancellation is respected at every step.

---

## 9. Example Usage

```go
package main

import (
    "bytes"
    "context"
    "fmt"
    "log"

    "github.com/yourorg/docpipe"
    "github.com/yourorg/docpipe/converter"
    "github.com/yourorg/docpipe/processor"
)

func main() {
    engine := docpipe.New(
        docpipe.WithMaxParallel(8),
        docpipe.WithProcessors(
            processor.NewCleaner(),
            processor.NewLangDetector(lingua.NewDetector()),
            processor.NewTransformer(processor.TransformConfig{
                NormalizeHeadings: true,
            }),
            processor.NewTranslator(openai.NewBackend(apiKey), "en"),
        ),
    )

    ctx := context.Background()
    docBytes := bytes.NewBuffer(rawDocx)

    result, err := engine.Submit(ctx, converter.FormatDOCX, docBytes)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Output.String())          // final Markdown
    fmt.Println(result.Metadata.DetectedLang)    // e.g. "de"
}
```

---

## 10. Implementation Roadmap

| Phase | Scope | Effort |
|---|---|---|
| **1** | Core types: `Engine`, `Session`, `Converter` interface, `Processor` interface, `Chain` | 1 day |
| **2** | `TextConverter`, `MarkdownConverter` (trivial), `Cleaner` processor | 1 day |
| **3** | `DOCXConverter` (using go-docx or pandoc subprocess), `PPTXConverter` | 2 days |
| **4** | `LangDetector`, `Transformer` | 1 day |
| **5** | `Translator` with pluggable backend | 1 day |
| **6** | Integration tests, benchmarks, semaphore tuning | 1 day |
