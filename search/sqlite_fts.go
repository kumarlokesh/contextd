package search

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// FTSSearcher is a Searcher backed by SQLite FTS5 (BM25).
type FTSSearcher struct {
	db *sql.DB

	stmtSearch          *sql.Stmt
	stmtSearchTimeRange *sql.Stmt
}

// NewFTSSearcher creates a FTSSearcher that shares the given *sql.DB.
// The database must already have the chats_fts virtual table (migration 002).
func NewFTSSearcher(db *sql.DB) (*FTSSearcher, error) {
	s := &FTSSearcher{db: db}
	if err := s.prepare(); err != nil {
		return nil, fmt.Errorf("preparing FTS statements: %w", err)
	}
	return s, nil
}

func (s *FTSSearcher) prepare() error {
	var err error

	// Base query: FTS match + project filter, ranked by BM25.
	// bm25() returns negative values; ORDER BY ASC gives best matches first.
	// snippet() produces a highlighted excerpt (32 tokens, HTML-safe markers).
	s.stmtSearch, err = s.db.Prepare(`
		SELECT c.id, c.session_id, c.timestamp,
		       snippet(chats_fts, 0, '<mark>', '</mark>', '...', 32),
		       bm25(chats_fts)
		FROM chats_fts
		JOIN chats c ON chats_fts.rowid = c.rowid
		WHERE chats_fts MATCH ?
		  AND c.project_id = ?
		ORDER BY bm25(chats_fts)
		LIMIT ?`)
	if err != nil {
		return err
	}

	s.stmtSearchTimeRange, err = s.db.Prepare(`
		SELECT c.id, c.session_id, c.timestamp,
		       snippet(chats_fts, 0, '<mark>', '</mark>', '...', 32),
		       bm25(chats_fts)
		FROM chats_fts
		JOIN chats c ON chats_fts.rowid = c.rowid
		WHERE chats_fts MATCH ?
		  AND c.project_id = ?
		  AND c.timestamp BETWEEN ? AND ?
		ORDER BY bm25(chats_fts)
		LIMIT ?`)
	return err
}

// Search executes a full-text BM25 search against the chats_fts index.
func (s *FTSSearcher) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	if req.Query == "" {
		return []SearchHit{}, nil
	}

	ftsQuery := buildFTSQuery(req.Query)
	limit := req.MaxResults
	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error

	if req.TimeRange != nil {
		rows, err = s.stmtSearchTimeRange.QueryContext(ctx,
			ftsQuery,
			req.ProjectID,
			req.TimeRange.Start.UnixMilli(),
			req.TimeRange.End.UnixMilli(),
			limit,
		)
	} else {
		rows, err = s.stmtSearch.QueryContext(ctx, ftsQuery, req.ProjectID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()

	var hits []SearchHit
	for rows.Next() {
		var (
			chatID    string
			sessionID string
			tsMillis  int64
			snippet   string
			bm25Raw   float64
		)
		if err := rows.Scan(&chatID, &sessionID, &tsMillis, &snippet, &bm25Raw); err != nil {
			return nil, err
		}
		// bm25Raw is negative (more negative = better). Negate to get a
		// positive score where higher is better.
		score := -bm25Raw
		hits = append(hits, SearchHit{
			ChatID:     chatID,
			SessionID:  sessionID,
			Timestamp:  time.UnixMilli(tsMillis).UTC(),
			Snippet:    snippet,
			BM25Score:  score,
			FinalScore: score,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []SearchHit{}
	}
	return hits, nil
}

// Close releases prepared statements. The underlying *sql.DB is not closed
// here - it is owned by the store.
func (s *FTSSearcher) Close() error {
	if s.stmtSearch != nil {
		s.stmtSearch.Close()
	}
	if s.stmtSearchTimeRange != nil {
		s.stmtSearchTimeRange.Close()
	}
	return nil
}

// buildFTSQuery converts a free-text query string into an FTS5 query
// expression. Each word is cleaned of FTS5 operator characters and emitted as
// an implicit AND term. Quoted phrases are preserved.
func buildFTSQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}

	// If the query already looks like a structured FTS5 expression, pass it
	// through unchanged. Indicators: a leading quote (phrase search), or
	// explicit boolean operators between tokens.
	upper := strings.ToUpper(q)
	if strings.HasPrefix(q, `"`) ||
		strings.Contains(upper, " AND ") ||
		strings.Contains(upper, " OR ") ||
		strings.Contains(upper, " NOT ") ||
		strings.Contains(upper, "NEAR(") {
		return q
	}

	// Plain text: strip FTS5 special chars from each token so they're
	// treated as literal term searches (implicit AND between tokens).
	tokens := strings.Fields(q)
	clean := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.Map(func(r rune) rune {
			switch r {
			case '"', '^', '(', ')', '*', '{', '}', '[', ']', ':', '\\':
				return -1
			}
			return r
		}, tok)
		if tok != "" {
			clean = append(clean, tok)
		}
	}
	return strings.Join(clean, " ")
}
