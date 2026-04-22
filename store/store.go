// Package store defines the storage interface for contextd.
// All implementations must be safe for concurrent use.
package store

import (
	"context"
	"time"
)

// Store is the append-only transcript store interface.
type Store interface {
	// StoreChat persists a chat and returns its generated ID.
	StoreChat(ctx context.Context, chat ChatInput) (string, error)
	// GetChat retrieves a single chat by project and chat ID.
	GetChat(ctx context.Context, projectID, chatID string) (*Chat, error)
	// RecentChats returns up to limit recent chats for the project, optionally
	// filtered by session. Results are ordered newest-first.
	RecentChats(ctx context.Context, projectID string, sessionID *string, limit int) ([]Chat, error)
	// DeleteChat removes a single chat by project and chat ID.
	DeleteChat(ctx context.Context, projectID, chatID string) error
	// DeleteProject removes all chats, sessions, and the project itself.
	// Returns the number of chats deleted.
	DeleteProject(ctx context.Context, projectID string) (int, error)
	// Close releases the underlying resources.
	Close() error
}

// ChatInput is the payload for storing a new chat.
type ChatInput struct {
	ProjectID string         `json:"project_id"`
	SessionID string         `json:"session_id"`
	Timestamp time.Time      `json:"timestamp"`
	Messages  []Message      `json:"messages"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Chat is a stored conversation record.
type Chat struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	SessionID string         `json:"session_id"`
	Timestamp time.Time      `json:"timestamp"`
	Messages  []Message      `json:"messages"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Message is a single turn in a conversation.
type Message struct {
	Role    string `json:"role"`    // "user", "assistant", "system"
	Content string `json:"content"`
}

// VectorStore manages vector embeddings for semantic search.
// Implementations are safe for concurrent use.
type VectorStore interface {
	// InsertEmbedding stores the embedding for a chat, keyed by its integer rowid.
	InsertEmbedding(ctx context.Context, chatRowID int64, embedding []float32) error
	// KNNSearch returns the nearest-neighbour chats to the query embedding,
	// restricted to the given project. Candidates are overfetched by fetchK then
	// filtered; the caller receives at most limit results.
	KNNSearch(ctx context.Context, projectID string, embedding []float32, fetchK, limit int) ([]VectorHit, error)
	// PendingChats returns up to limit chats whose embeddings have not yet been
	// generated (status = 'pending').
	PendingChats(ctx context.Context, limit int) ([]PendingChat, error)
	// SetEmbeddingStatus updates the status of a chat's embedding job.
	// Valid values: "pending", "done", "failed".
	SetEmbeddingStatus(ctx context.Context, chatID, status string) error
	// ChatRowID resolves a chat UUID to its SQLite integer rowid.
	ChatRowID(ctx context.Context, chatID string) (int64, error)
	// Close releases any resources held by the VectorStore.
	Close() error
}

// VectorHit is a single result from a KNN vector search.
type VectorHit struct {
	ChatID    string
	SessionID string
	Timestamp time.Time
	Distance  float64 // L2 distance; lower is more similar
}

// PendingChat is a chat that needs an embedding generated.
type PendingChat struct {
	ChatID      string
	ProjectID   string
	ContentText string // pre-flattened text stored in chats.content_text
}
