package search

import (
	"context"
	"math"
	"time"

	"github.com/kumarlokesh/contextd/embed"
	"github.com/kumarlokesh/contextd/store"
)

// HybridSearcher combines FTS5 BM25 and vector KNN results using a weighted
// linear combination with temporal decay.
//
// Final score = α·norm(BM25) + β·norm(vecSim) + γ·temporalDecay
//
// where norm() divides by the max score among all hits (so each component is
// in [0, 1]), and temporalDecay = exp(-0.05 * daysOld).
type HybridSearcher struct {
	fts      *FTSSearcher
	vs       store.VectorStore
	embedder embed.Embedder

	alpha float64 // BM25 weight
	beta  float64 // vector weight
	gamma float64 // temporal decay weight
}

// NewHybridSearcher creates a HybridSearcher.
func NewHybridSearcher(
	fts *FTSSearcher,
	vs store.VectorStore,
	embedder embed.Embedder,
	alpha, beta, gamma float64,
) *HybridSearcher {
	return &HybridSearcher{
		fts:      fts,
		vs:       vs,
		embedder: embedder,
		alpha:    alpha,
		beta:     beta,
		gamma:    gamma,
	}
}

type ftsResult struct {
	hits []SearchHit
	err  error
}

type vecResult struct {
	hits []store.VectorHit
	err  error
}

// candidate holds a merged result during hybrid score computation.
type candidate struct {
	hit    SearchHit
	bm25   float64
	vecSim float64 // converted from L2 distance: 1/(1+d)
}

// Search runs FTS5 and vector KNN concurrently, then merges and re-ranks the
// results. The effective limit passed to each sub-searcher is req.MaxResults*3
// to ensure good coverage before merging.
func (h *HybridSearcher) Search(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	if req.Query == "" {
		return []SearchHit{}, nil
	}

	limit := req.MaxResults
	if limit <= 0 {
		limit = 100
	}
	fetchN := limit * 3 // overfetch before merge

	// Embed the query for the vector search arm.
	queryVec, embedErr := h.embedder.Embed(ctx, req.Query)

	// FTS and vector arms run concurrently.
	ftsCh := make(chan ftsResult, 1)
	vecCh := make(chan vecResult, 1)

	go func() {
		hits, err := h.fts.Search(ctx, SearchRequest{
			ProjectID:  req.ProjectID,
			Query:      req.Query,
			MaxResults: fetchN,
			TimeRange:  req.TimeRange,
		})
		ftsCh <- ftsResult{hits, err}
	}()

	go func() {
		if embedErr != nil || queryVec == nil {
			vecCh <- vecResult{nil, embedErr}
			return
		}
		hits, err := h.vs.KNNSearch(ctx, req.ProjectID, queryVec, fetchN, fetchN)
		vecCh <- vecResult{hits, err}
	}()

	fr := <-ftsCh
	vr := <-vecCh

	// Surface FTS errors; log vector errors but degrade gracefully.
	if fr.err != nil {
		return nil, fr.err
	}

	// Merge into a map keyed by ChatID.
	byID := make(map[string]*candidate, len(fr.hits))

	for _, h := range fr.hits {
		c := &candidate{
			hit:  h,
			bm25: h.BM25Score,
		}
		byID[h.ChatID] = c
	}

	if vr.err == nil {
		for _, vh := range vr.hits {
			sim := 1.0 / (1.0 + vh.Distance) // convert L2 distance to similarity
			if c, ok := byID[vh.ChatID]; ok {
				c.vecSim = sim
			} else {
				// Vector-only hit; add with zero BM25.
				byID[vh.ChatID] = &candidate{
					hit: SearchHit{
						ChatID:    vh.ChatID,
						SessionID: vh.SessionID,
						Timestamp: vh.Timestamp,
					},
					vecSim: sim,
				}
			}
		}
	}

	if len(byID) == 0 {
		return []SearchHit{}, nil
	}

	// Normalise BM25 and vecSim independently.
	var maxBM25, maxVec float64
	for _, c := range byID {
		if c.bm25 > maxBM25 {
			maxBM25 = c.bm25
		}
		if c.vecSim > maxVec {
			maxVec = c.vecSim
		}
	}

	now := time.Now()
	candidates := make([]*candidate, 0, len(byID))
	for _, c := range byID {
		normBM25 := safeDiv(c.bm25, maxBM25)
		normVec := safeDiv(c.vecSim, maxVec)
		decay := temporalDecay(now, c.hit.Timestamp)

		c.hit.BM25Score = c.bm25
		c.hit.VectorScore = c.vecSim
		c.hit.FinalScore = h.alpha*normBM25 + h.beta*normVec + h.gamma*decay
		candidates = append(candidates, c)
	}

	// Sort descending by FinalScore.
	sortByFinalScore(candidates)

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	hits := make([]SearchHit, len(candidates))
	for i, c := range candidates {
		hits[i] = c.hit
	}
	return hits, nil
}

// Close releases the FTS searcher; the VectorStore and Embedder are owned
// by the caller.
func (h *HybridSearcher) Close() error {
	return h.fts.Close()
}

// temporalDecay returns exp(-0.05 * daysOld), giving a ~14-day half-life.
func temporalDecay(now, ts time.Time) float64 {
	ms := now.UnixMilli() - ts.UnixMilli()
	if ms < 0 {
		ms = 0
	}
	days := float64(ms) / 86_400_000.0
	return math.Exp(-0.05 * days)
}

// safeDiv returns num/den, or 0 if den is 0.
func safeDiv(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}

// sortByFinalScore performs an in-place descending sort by FinalScore.
// Uses insertion sort (acceptable for typical result set sizes ≤ 300).
func sortByFinalScore(cs []*candidate) {
	for i := 1; i < len(cs); i++ {
		key := cs[i]
		j := i - 1
		for j >= 0 && cs[j].hit.FinalScore < key.hit.FinalScore {
			cs[j+1] = cs[j]
			j--
		}
		cs[j+1] = key
	}
}
