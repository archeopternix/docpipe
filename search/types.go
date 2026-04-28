package search

import (
	"context"
)

type SearchProvider interface {
	Upsert(ctx context.Context, doc SearchDocument) error
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, q SearchQuery) ([]SearchHit, error)
}

// SearchDocument represents a document to be indexed for search.
// The body is split into sections to allow for more granular search results,
// and the Fields map allows for arbitrary metadata that can be used for filtering.
type SearchDocument struct {
	ID        string // doc id
	Title     string
	Path      string            // optional (folder/path)
	Language  string            // optional
	Body      string            // full text to index
	Keywords  []string          // optional
	UpdatedAt int64             // unix millis or seconds
	Fields    map[string]string // arbitrary filterable fields (author, tags, etc)
}

// SearchSection represents a section of a document, which can be used for more granular search results.
// Evry document can have multiple sections, e.g. for markdown files, each h1..h3 section
// can be a separate SearchSection with its own title and content and anchor tag.
// This allows the search results to point to specific sections of a document rather than just the document as a whole.
type SearchSection struct {
	Title     string // h1..h3
	AnchorTag string // for markdown files
	Content   string // text content of the section
}

// SearchHit represents a single search result.
type SearchHit struct {
	ID            string
	Score         float64
	Title         string
	Path          string
	SearchSection SearchSection
	Snippet       string
}

// SearchQuery represents a search query with optional filters and pagination.
type SearchQuery struct {
	Query   string
	Limit   int
	Offset  int
	Filters map[string]string // optional: tag=foo, lang=en, etc
}
