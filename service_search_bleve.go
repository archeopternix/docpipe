package docpipe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

type BleveSearch struct {
	mu    sync.RWMutex
	index bleve.Index
}

// NewBleveSearch opens or creates a Bleve index at indexPath.
func NewBleveSearch(indexPath string) (*BleveSearch, error) {
	indexPath = strings.TrimSpace(indexPath)
	if indexPath == "" {
		return nil, fmt.Errorf("%w: indexPath required", ErrInvalidInput)
	}
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return nil, err
	}

	var ix bleve.Index
	if _, err := os.Stat(indexPath); err == nil {
		ix, err = bleve.Open(indexPath)
		if err != nil {
			return nil, err
		}
	} else {
		m := defaultSectionMapping()
		var err error
		ix, err = bleve.New(indexPath, m)
		if err != nil {
			return nil, err
		}
	}

	return &BleveSearch{index: ix}, nil
}

func (b *BleveSearch) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.index == nil {
		return nil
	}
	err := b.index.Close()
	b.index = nil
	return err
}

// Upsert adds or updates a document in the index. It deletes any existing sections
// for the same doc ID before adding new ones.
func (b *BleveSearch) Upsert(ctx context.Context, doc SearchDocument) error {
	_ = ctx // bleve does not accept context; caller can enforce timeouts outside.

	if strings.TrimSpace(doc.ID) == "" {
		return fmt.Errorf("%w: missing doc.ID", ErrInvalidInput)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.index == nil {
		return errors.New("bleve search: index not initialized")
	}

	// Delete previous sections for this doc (simple + robust).
	if err := b.deleteLocked(doc.ID); err != nil {
		return err
	}

	sections := SplitMarkdownToSearchSections(doc.Body)
	batch := b.index.NewBatch()
	for i, sec := range sections {
		// Index even empty sections? Usually skip if no content/title.
		if strings.TrimSpace(sec.Title) == "" && strings.TrimSpace(sec.Content) == "" {
			continue
		}

		row := bleveSectionRow{
			BleveID:    sectionBleveID(doc.ID, i),
			DocID:      doc.ID,
			DocTitle:   doc.Title,
			DocPath:    doc.Path,
			Language:   doc.Language,
			UpdatedAt:  doc.UpdatedAt,
			Keywords:   doc.Keywords,
			SecIndex:   i,
			SecTitle:   sec.Title,
			SecAnchor:  sec.AnchorTag,
			SecContent: sec.Content,
			Fields:     doc.Fields,
		}

		// Use batch.Index(bleveDocID, rowStruct).
		batch.Index(row.BleveID, row)
	}

	return b.index.Batch(batch)
}

// Delete removes all sections for the given doc ID from the index.
func (b *BleveSearch) Delete(ctx context.Context, id string) error {
	_ = ctx
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: missing id", ErrInvalidInput)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.index == nil {
		return errors.New("bleve search: index not initialized")
	}
	return b.deleteLocked(id)
}

func (b *BleveSearch) deleteLocked(docID string) error {
	// We need to delete all sections belonging to docID.
	// Bleve doesn't provide "delete by query" directly, so we:
	// 1) search for all rows with doc_id=docID
	// 2) delete each hit by its bleve doc id
	q := bleve.NewTermQuery(docID)
	q.SetField("doc_id")

	req := bleve.NewSearchRequestOptions(q, 1000, 0, false)
	req.Fields = []string{"_id"} // we really want IDs; bleve returns hit.ID anyway.

	res, err := b.index.Search(req)
	if err != nil {
		return err
	}

	// If you expect more than 1000 sections, loop with paging.
	for res.Total > uint64(len(res.Hits)) {
		// Page through the rest.
		req.From = len(res.Hits)
		next, err := b.index.Search(req)
		if err != nil {
			return err
		}
		res.Hits = append(res.Hits, next.Hits...)
	}

	batch := b.index.NewBatch()
	for _, hit := range res.Hits {
		batch.Delete(hit.ID)
	}
	return b.index.Batch(batch)
}

func (b *BleveSearch) Search(ctx context.Context, q SearchQuery) ([]SearchHit, error) {
	_ = ctx

	queryStr := strings.TrimSpace(q.Query)
	if queryStr == "" {
		return []SearchHit{}, nil
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.index == nil {
		return nil, errors.New("bleve search: index not initialized")
	}

	bleveQ := buildBleveQuery(queryStr, q.Filters)

	req := bleve.NewSearchRequestOptions(bleveQ, limit, offset, false)
	req.Fields = []string{
		"doc_id", "doc_title", "doc_path", "lang",
		"sec_title", "sec_anchor", "sec_content", "sec_index",
	}
	req.Highlight = bleve.NewHighlightWithStyle("html")
	req.Highlight.Fields = []string{"sec_content"}

	res, err := b.index.Search(req)
	if err != nil {
		return nil, err
	}

	out := make([]SearchHit, 0, len(res.Hits))
	for _, hit := range res.Hits {
		docID := asString(hit.Fields["doc_id"])
		docTitle := asString(hit.Fields["doc_title"])
		docPath := asString(hit.Fields["doc_path"])

		secTitle := asString(hit.Fields["sec_title"])
		secAnchor := asString(hit.Fields["sec_anchor"])
		secContent := asString(hit.Fields["sec_content"])

		// Prefer highlight snippet if present.
		snippet := ""
		if frags, ok := hit.Fragments["sec_content"]; ok && len(frags) > 0 {
			snippet = frags[0]
		} else {
			snippet = makeSnippet(secContent, queryStr, 220)
		}

		out = append(out, SearchHit{
			ID:    docID,
			Score: hit.Score,
			Title: docTitle,
			Path:  docPath,
			SearchSection: SearchSection{
				Title:     secTitle,
				AnchorTag: secAnchor,
				Content:   "", // do not return full content by default; keep response light
			},
			Snippet: snippet,
		})
	}

	return out, nil
}

/*
 * ===== Internal representation for Bleve =====
 * We store one row per section. All fields are "store: true" so we can rebuild SearchHit without
 * reading the original document.
 */

type bleveSectionRow struct {
	BleveID string `json:"-"` // not indexed, used as batch key

	DocID     string `json:"doc_id"`
	DocTitle  string `json:"doc_title"`
	DocPath   string `json:"doc_path"`
	Language  string `json:"lang"`
	UpdatedAt int64  `json:"updated_at"`

	Keywords []string          `json:"keywords,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`

	SecIndex   int    `json:"sec_index"`
	SecTitle   string `json:"sec_title"`
	SecAnchor  string `json:"sec_anchor"`
	SecContent string `json:"sec_content"`
}

func defaultSectionMapping() mapping.IndexMapping {
	im := bleve.NewIndexMapping()

	// Default mapping is OK for many cases, but we explicitly ensure key fields are stored.
	dm := bleve.NewDocumentMapping()

	text := bleve.NewTextFieldMapping()
	text.Store = true
	text.Index = true

	keyword := bleve.NewKeywordFieldMapping()
	keyword.Store = true
	keyword.Index = true

	num := bleve.NewNumericFieldMapping()
	num.Store = true
	num.Index = true

	// doc-level
	dm.AddFieldMappingsAt("doc_id", keyword)
	dm.AddFieldMappingsAt("doc_title", text)
	dm.AddFieldMappingsAt("doc_path", keyword)
	dm.AddFieldMappingsAt("lang", keyword)
	dm.AddFieldMappingsAt("updated_at", num)

	// section-level
	dm.AddFieldMappingsAt("sec_index", num)
	dm.AddFieldMappingsAt("sec_title", text)
	dm.AddFieldMappingsAt("sec_anchor", keyword)
	dm.AddFieldMappingsAt("sec_content", text)

	// keywords (treat as keywords for filtering / term match)
	dm.AddFieldMappingsAt("keywords", keyword)

	// fields.* (dynamic metadata). Bleve mapping for dynamic object fields is a bit engine-specific;
	// easiest is to not rely on analysis here and just include them in query building as term queries.
	// We still store them for debugging or future.
	obj := bleve.NewDocumentMapping()
	obj.Dynamic = true
	obj.AddFieldMappingsAt("*", keyword)
	dm.AddSubDocumentMapping("fields", obj)

	im.DefaultMapping = dm
	im.TypeField = "" // we don't use types
	return im
}

func buildBleveQuery(q string, filters map[string]string) query.Query {
	// Search across section content + titles; boost titles.
	contentQ := bleve.NewMatchQuery(q)
	contentQ.SetField("sec_content")
	contentQ.SetBoost(1.0)

	secTitleQ := bleve.NewMatchQuery(q)
	secTitleQ.SetField("sec_title")
	secTitleQ.SetBoost(2.0)

	docTitleQ := bleve.NewMatchQuery(q)
	docTitleQ.SetField("doc_title")
	docTitleQ.SetBoost(1.5)

	disj := bleve.NewDisjunctionQuery(contentQ, secTitleQ, docTitleQ)

	// Apply filters (exact match).
	if len(filters) == 0 {
		return disj
	}

	conj := bleve.NewConjunctionQuery(disj)
	for k, v := range filters {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}

		tq := bleve.NewTermQuery(v)
		switch k {
		case "lang", "language":
			tq.SetField("lang")
		case "path":
			tq.SetField("doc_path")
		default:
			tq.SetField("fields." + k)
		}
		conj.AddQuery(tq)
	}
	return conj
}

func sectionBleveID(docID string, secIndex int) string {
	// Keep it simple; ensure no accidental collisions.
	return docID + "::" + strconv.Itoa(secIndex)
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

func makeSnippet(content, q string, max int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if max <= 0 {
		max = 200
	}
	lc := strings.ToLower(content)
	lq := strings.ToLower(strings.TrimSpace(q))
	if lq == "" {
		if len(content) > max {
			return content[:max] + "..."
		}
		return content
	}
	i := strings.Index(lc, lq)
	if i < 0 {
		if len(content) > max {
			return content[:max] + "..."
		}
		return content
	}
	start := i - 60
	if start < 0 {
		start = 0
	}
	end := i + len(lq) + (max - 60)
	if end > len(content) {
		end = len(content)
	}
	s := strings.TrimSpace(content[start:end])
	if start > 0 {
		s = "..." + s
	}
	if end < len(content) {
		s = s + "..."
	}
	return s
}
