package embed

import (
	"context"
	"log/slog"
	"time"

	"github.com/kumarlokesh/contextd/store"
)

// Worker polls for chats that need embeddings and generates them in batches.
type Worker struct {
	vs           store.VectorStore
	embedder     Embedder
	batchSize    int
	pollInterval time.Duration
	logger       *slog.Logger
}

// NewWorker creates a Worker. pollInterval controls how often the worker checks
// for pending chats; batchSize caps how many are processed per tick.
func NewWorker(vs store.VectorStore, e Embedder, batchSize int, pollInterval time.Duration, logger *slog.Logger) *Worker {
	if batchSize <= 0 {
		batchSize = 32
	}
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	return &Worker{
		vs:           vs,
		embedder:     e,
		batchSize:    batchSize,
		pollInterval: pollInterval,
		logger:       logger,
	}
}

// Run starts the embedding loop. It returns when ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	// Process any backlog immediately on start.
	w.processBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	pending, err := w.vs.PendingChats(ctx, w.batchSize)
	if err != nil {
		w.logger.Error("embed worker: listing pending chats", "err", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	w.logger.Debug("embed worker: processing batch", "count", len(pending))

	for _, pc := range pending {
		if ctx.Err() != nil {
			return
		}
		if err := w.embedOne(ctx, pc); err != nil {
			w.logger.Warn("embed worker: embedding failed",
				"chat_id", pc.ChatID, "err", err)
			// Mark as failed so the chat is not retried indefinitely in this session.
			// A future run (or restart) can reset status to 'pending' for manual retry.
			_ = w.vs.SetEmbeddingStatus(ctx, pc.ChatID, "failed")
		}
	}
}

func (w *Worker) embedOne(ctx context.Context, pc store.PendingChat) error {
	vec, err := w.embedder.Embed(ctx, pc.ContentText)
	if err != nil {
		return err
	}

	rowID, err := w.vs.ChatRowID(ctx, pc.ChatID)
	if err != nil {
		return err
	}

	if err := w.vs.InsertEmbedding(ctx, rowID, vec); err != nil {
		return err
	}

	return w.vs.SetEmbeddingStatus(ctx, pc.ChatID, "done")
}
