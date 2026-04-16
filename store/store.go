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
