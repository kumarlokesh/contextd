// Package search defines the search interface for contextd.
// Implements FTS5 BM25 full-text search.
// Adds sqlite-vec vector search and the hybrid ranker.
package search

import (
	"context"
	"time"
)

// Searcher executes queries against the search index.
// All implementations must be safe for concurrent use.
type Searcher interface {
	Search(ctx context.Context, req SearchRequest) ([]SearchHit, error)
	Close() error
}

// SearchRequest is the input to Searcher.Search.
type SearchRequest struct {
	ProjectID  string
	Query      string
	MaxResults int
	TimeRange  *TimeRange
}

// TimeRange is an optional time window filter.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// SearchHit is a single result returned by a search.
type SearchHit struct {
	ChatID      string
	SessionID   string
	Timestamp   time.Time
	Snippet     string
	BM25Score   float64 // positive; higher is better
	VectorScore float64 // 0.0-1.0; higher is better (after negation in ranker)
	FinalScore  float64 // composite; populated by hybrid ranker in FTS5 mode
}
