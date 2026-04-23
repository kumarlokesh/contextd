// Package audit provides the hash-chained audit log for contextd.
// Every memory access (search, retrieve, delete, export) is recorded with a
// cryptographic hash linking each entry to the one before it, making
// undetected tampering with historical records impossible.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"time"
)

// Logger records audit events.
// All implementations must be safe for concurrent use.
type Logger interface {
	// Log appends an audit entry. Timestamp and Actor are populated with
	// defaults if zero/empty. The implementation fills PrevHash and EntryHash.
	Log(ctx context.Context, entry Entry) error
	// Query returns audit entries matching filter, ordered by id DESC by default.
	Query(ctx context.Context, filter Filter) ([]Entry, error)
	// Close releases any held resources.
	Close() error
}

// Entry is one record in the audit chain.
type Entry struct {
	ID           int64          // set by the implementation after insert
	Timestamp    time.Time      // populated by Log if zero
	ProjectID    string
	Action       string         // one of the Action* constants
	Actor        string         // "anonymous" if unauthenticated
	QueryHash    string         // SHA-256(query) for search actions
	ResultHashes []string       // SHA-256(chat_id) for each returned result
	Metadata     map[string]any // action-specific context (e.g. {"count": 5})
	PrevHash     string         // set by the implementation
	EntryHash    string         // set by the implementation
}

// Filter selects audit entries for Query.
type Filter struct {
	ProjectID string
	Action    string     // empty matches all
	TimeRange *TimeRange
	Limit     int        // 0 defaults to 50
	Offset    int
	Ascending bool       // false = newest first (default); true = oldest first
}

// TimeRange is a closed interval filter.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// Action constants.
const (
	ActionSearch   = "search"
	ActionRetrieve = "retrieve"
	ActionDelete   = "delete"
	ActionExport   = "export"
)

// GenesisHash is used as the prev_hash for the very first entry in the chain.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// ComputeEntryHash computes the SHA-256 entry hash from the entry's fields and
// the serialized metadata JSON. metadataJSON must be stable (same bytes every
// time for the same logical value - use standard json.Marshal which sorts map
// keys deterministically in Go).
func ComputeEntryHash(e Entry, metadataJSON []byte) string {
	h := sha256.New()
	_ = binary.Write(h, binary.BigEndian, e.Timestamp.UnixMilli())
	h.Write([]byte(e.ProjectID))
	h.Write([]byte(e.Action))
	h.Write([]byte(e.Actor))
	h.Write([]byte(e.QueryHash))
	for _, rh := range e.ResultHashes {
		h.Write([]byte(rh))
	}
	if len(metadataJSON) == 0 {
		metadataJSON = []byte("{}")
	}
	h.Write(metadataJSON)
	h.Write([]byte(e.PrevHash))
	return hex.EncodeToString(h.Sum(nil))
}

// HashChatID returns the SHA-256 hex digest of a chat ID, used to record
// which results were returned without exposing the raw IDs in the audit log.
func HashChatID(chatID string) string {
	sum := sha256.Sum256([]byte(chatID))
	return hex.EncodeToString(sum[:])
}
