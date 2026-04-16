// Package api defines the HTTP handler layer for contextd.
package api

import (
	"time"

	"github.com/kumarlokesh/contextd/store"
)

// StoreChatRequest is the payload for POST /v1/store_chat.
type StoreChatRequest struct {
	ProjectID string            `json:"project_id"`
	SessionID string            `json:"session_id"`
	Timestamp *time.Time        `json:"timestamp,omitempty"` // nil = server time
	Messages  []store.Message   `json:"messages"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// StoreChatResponse is the response for POST /v1/store_chat.
type StoreChatResponse struct {
	ChatID   string    `json:"chat_id"`
	StoredAt time.Time `json:"stored_at"`
}

// SearchRequest is the payload for POST /v1/conversation_search.
type SearchRequest struct {
	ProjectID  string     `json:"project_id"`
	Query      string     `json:"query"`
	MaxResults int        `json:"max_results,omitempty"`
	TimeRange  *TimeRange `json:"time_range,omitempty"`
}

// SearchResponse is the response for POST /v1/conversation_search.
type SearchResponse struct {
	Results   []SearchResult `json:"results"`
	QueryHash string         `json:"query_hash"` // for audit linkage (M6)
	TookMS    int64          `json:"took_ms"`
}

// SearchResult is a single hit returned by a search.
type SearchResult struct {
	ChatID    string    `json:"chat_id"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Snippet   string    `json:"snippet"`
	Score     float64   `json:"score"`
}

// RecentChatsRequest is the payload for POST /v1/recent_chats.
type RecentChatsRequest struct {
	ProjectID string  `json:"project_id"`
	SessionID *string `json:"session_id,omitempty"`
	Limit     int     `json:"limit,omitempty"`
}

// RecentChatsResponse is the response for POST /v1/recent_chats.
type RecentChatsResponse struct {
	Chats []store.Chat `json:"chats"`
}

// TimeRange is an optional time filter for search requests.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ErrorResponse is the standard error envelope for all 4xx/5xx responses.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody contains the error code, message, and optional details.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}
