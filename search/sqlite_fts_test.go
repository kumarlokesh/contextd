package search

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kumarlokesh/contextd/store"
	sqlitestore "github.com/kumarlokesh/contextd/store/sqlite"
)

// openTestSearcher opens a fresh SQLite store (which runs all migrations
// including 002_fts.sql) and returns both the store and an FTSSearcher that
// share the same DB connection.
func openTestSearcher(t *testing.T) (*sqlitestore.Store, *FTSSearcher) {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	fts, err := NewFTSSearcher(st.DB())
	if err != nil {
		t.Fatalf("new fts searcher: %v", err)
	}
	t.Cleanup(func() {
		fts.Close()
		st.Close()
	})
	return st, fts
}

func seed(t *testing.T, st *sqlitestore.Store, project, session, content string, ts time.Time) string {
	t.Helper()
	id, err := st.StoreChat(context.Background(), store.ChatInput{
		ProjectID: project,
		SessionID: session,
		Timestamp: ts,
		Messages:  []store.Message{{Role: "user", Content: content}},
	})
	if err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	return id
}

// TestFTSSimpleQuery verifies a basic single-term search returns the expected chat.
func TestFTSSimpleQuery(t *testing.T) {
	st, fts := openTestSearcher(t)
	ctx := context.Background()

	seed(t, st, "p1", "s1", "golang concurrency patterns", time.Now())
	seed(t, st, "p1", "s1", "python machine learning basics", time.Now())

	hits, err := fts.Search(ctx, SearchRequest{ProjectID: "p1", Query: "golang", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].Snippet == "" {
		t.Error("expected non-empty snippet")
	}
	if hits[0].BM25Score <= 0 {
		t.Errorf("expected positive BM25 score, got %f", hits[0].BM25Score)
	}
}

// TestFTSMultiTermQuery verifies multi-word queries use implicit AND.
func TestFTSMultiTermQuery(t *testing.T) {
	st, fts := openTestSearcher(t)
	ctx := context.Background()

	seed(t, st, "p1", "s1", "sqlite full text search tutorial", time.Now())
	seed(t, st, "p1", "s1", "sqlite performance tuning tips", time.Now())
	seed(t, st, "p1", "s1", "postgres full text search", time.Now())

	hits, err := fts.Search(ctx, SearchRequest{ProjectID: "p1", Query: "sqlite search", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Only the first chat contains both "sqlite" AND "search".
	if len(hits) != 1 {
		t.Errorf("expected 1 hit for sqlite+search, got %d", len(hits))
	}
}

// TestFTSNoResults confirms an empty result set for an unmatched query.
func TestFTSNoResults(t *testing.T) {
	st, fts := openTestSearcher(t)
	ctx := context.Background()

	seed(t, st, "p1", "s1", "hello world", time.Now())

	hits, err := fts.Search(ctx, SearchRequest{ProjectID: "p1", Query: "zzznomatch", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(hits))
	}
}

// TestFTSProjectIsolation ensures queries don't cross project boundaries.
func TestFTSProjectIsolation(t *testing.T) {
	st, fts := openTestSearcher(t)
	ctx := context.Background()

	seed(t, st, "proj-A", "s1", "secret internal data", time.Now())
	seed(t, st, "proj-B", "s1", "public information", time.Now())

	hits, err := fts.Search(ctx, SearchRequest{ProjectID: "proj-B", Query: "secret", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for cross-project query, got %d", len(hits))
	}
}

// TestFTSTimeRangeFilter confirms time_range filters out chats outside the window.
func TestFTSTimeRangeFilter(t *testing.T) {
	st, fts := openTestSearcher(t)
	ctx := context.Background()

	old := time.Now().Add(-72 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)

	seed(t, st, "p1", "s1", "deployment pipeline automation", old)
	seed(t, st, "p1", "s1", "deployment configuration changes", recent)

	hits, err := fts.Search(ctx, SearchRequest{
		ProjectID:  "p1",
		Query:      "deployment",
		MaxResults: 10,
		TimeRange: &TimeRange{
			Start: time.Now().Add(-2 * time.Hour),
			End:   time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("expected 1 hit within time range, got %d", len(hits))
	}
}

// TestFTSSpecialCharEscaping confirms queries with special chars don't error.
func TestFTSSpecialCharEscaping(t *testing.T) {
	st, fts := openTestSearcher(t)
	ctx := context.Background()

	seed(t, st, "p1", "s1", "hello world test content", time.Now())

	// These would crash an unescaped FTS5 query.
	for _, q := range []string{"hello (world)", "hello*", "hello^world", "hello[test]"} {
		_, err := fts.Search(ctx, SearchRequest{ProjectID: "p1", Query: q, MaxResults: 10})
		if err != nil {
			t.Errorf("Search(%q) returned unexpected error: %v", q, err)
		}
	}
}

// TestFTSBM25Ranking verifies that the highest-relevance document is ranked first.
func TestFTSBM25Ranking(t *testing.T) {
	st, fts := openTestSearcher(t)
	ctx := context.Background()

	// First doc mentions "rust" many times; second only once.
	seed(t, st, "p1", "s1", "rust rust rust rust rust programming language systems", time.Now())
	seed(t, st, "p1", "s1", "rust is one of many programming languages", time.Now())

	hits, err := fts.Search(ctx, SearchRequest{ProjectID: "p1", Query: "rust", MaxResults: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected at least 2 hits, got %d", len(hits))
	}
	if hits[0].BM25Score < hits[1].BM25Score {
		t.Errorf("expected first hit to have higher score: %.4f < %.4f", hits[0].BM25Score, hits[1].BM25Score)
	}
}

// TestBuildFTSQuery covers the query builder edge cases.
func TestBuildFTSQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"golang", "golang"},
		{"golang concurrency", "golang concurrency"},
		{"  spaces  ", "spaces"},
		{"hello (world)", "hello world"},
		{"foo*bar", "foobar"},
		{`foo"bar`, "foobar"},
		// Structured queries passed through as-is.
		{`"exact phrase"`, `"exact phrase"`},
		{"foo AND bar", "foo AND bar"},
		{"foo OR bar", "foo OR bar"},
	}
	for _, tc := range cases {
		got := buildFTSQuery(tc.in)
		if got != tc.want {
			t.Errorf("buildFTSQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
