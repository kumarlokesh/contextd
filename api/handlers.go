package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kumarlokesh/contextd/audit"
	"github.com/kumarlokesh/contextd/search"
	"github.com/kumarlokesh/contextd/store"
)

// mustJSON encodes v as a JSON string literal (used for inline JSON building).
func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

const maxBodyBytes = 1 << 20 // 1 MB

// Handlers holds the dependencies for all API handlers.
type Handlers struct {
	store    store.Store
	searcher search.Searcher // nil → substring fallback
	auditor  audit.Logger   // nil → audit disabled
	policy   policyConfig
}

type policyConfig struct {
	maxResults          int
	defaultRetentionDays int
}

// NewHandlers constructs Handlers. Pass nil for searcher or auditor to
// disable those features.
func NewHandlers(st store.Store, sr search.Searcher, al audit.Logger, maxResultsPerQuery, defaultRetentionDays int) *Handlers {
	if maxResultsPerQuery <= 0 {
		maxResultsPerQuery = 100
	}
	if defaultRetentionDays <= 0 {
		defaultRetentionDays = 90
	}
	return &Handlers{
		store:    st,
		searcher: sr,
		auditor:  al,
		policy: policyConfig{
			maxResults:           maxResultsPerQuery,
			defaultRetentionDays: defaultRetentionDays,
		},
	}
}

// handleStoreChat handles POST /v1/store_chat.
func (h *Handlers) handleStoreChat(w http.ResponseWriter, r *http.Request) {
	var req StoreChatRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project_id is required")
		return
	}
	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "session_id is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "messages must not be empty")
		return
	}

	ts := time.Now().UTC()
	if req.Timestamp != nil {
		ts = req.Timestamp.UTC()
	}

	input := store.ChatInput{
		ProjectID: req.ProjectID,
		SessionID: req.SessionID,
		Timestamp: ts,
		Messages:  req.Messages,
		Metadata:  req.Metadata,
	}

	chatID, err := h.store.StoreChat(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to store chat")
		return
	}

	writeJSON(w, http.StatusCreated, StoreChatResponse{
		ChatID:   chatID,
		StoredAt: ts,
	})
}

// handleConversationSearch handles POST /v1/conversation_search.
// When a Searcher is configured it uses FTS5 BM25 (or hybrid); otherwise it
// falls back to a case-insensitive substring scan over recent chats.
func (h *Handlers) handleConversationSearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req SearchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project_id is required")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "query is required")
		return
	}

	limit := h.policy.maxResults
	if req.MaxResults > 0 && req.MaxResults < limit {
		limit = req.MaxResults
	}

	queryHash := hashQuery(req.ProjectID, req.Query, req.TimeRange)

	var results []SearchResult
	var searchErr error

	if h.searcher != nil {
		results, searchErr = h.ftsSearch(r, req, limit)
	} else {
		results, searchErr = h.substringSearch(r, req, limit)
	}
	if searchErr != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "search failed")
		return
	}
	if results == nil {
		results = []SearchResult{}
	}

	// Audit: log the search with hashed result IDs.
	h.logAudit(r, audit.Entry{
		ProjectID: req.ProjectID,
		Action:    audit.ActionSearch,
		QueryHash: queryHash,
		ResultHashes: resultHashes(results),
		Metadata: map[string]any{
			"result_count": len(results),
		},
	})

	writeJSON(w, http.StatusOK, SearchResponse{
		Results:   results,
		QueryHash: queryHash,
		TookMS:    time.Since(start).Milliseconds(),
	})
}

// ftsSearch delegates to the configured Searcher (FTS5 BM25 or hybrid).
func (h *Handlers) ftsSearch(r *http.Request, req SearchRequest, limit int) ([]SearchResult, error) {
	sreq := search.SearchRequest{
		ProjectID:  req.ProjectID,
		Query:      req.Query,
		MaxResults: limit,
	}
	if req.TimeRange != nil {
		sreq.TimeRange = &search.TimeRange{
			Start: req.TimeRange.Start,
			End:   req.TimeRange.End,
		}
	}

	hits, err := h.searcher.Search(r.Context(), sreq)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		results = append(results, SearchResult{
			ChatID:    h.ChatID,
			SessionID: h.SessionID,
			Timestamp: h.Timestamp,
			Snippet:   h.Snippet,
			Score:     h.FinalScore,
		})
	}
	return results, nil
}

// substringSearch is the pre-FTS fallback: case-insensitive substring scan.
func (h *Handlers) substringSearch(r *http.Request, req SearchRequest, limit int) ([]SearchResult, error) {
	candidateLimit := limit * 10
	if candidateLimit < 100 {
		candidateLimit = 100
	}
	chats, err := h.store.RecentChats(r.Context(), req.ProjectID, nil, candidateLimit)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(req.Query)
	var results []SearchResult
	for _, c := range chats {
		if req.TimeRange != nil {
			if c.Timestamp.Before(req.TimeRange.Start) || c.Timestamp.After(req.TimeRange.End) {
				continue
			}
		}
		snippet := matchSnippet(c.Messages, queryLower)
		if snippet == "" {
			continue
		}
		results = append(results, SearchResult{
			ChatID:    c.ID,
			SessionID: c.SessionID,
			Timestamp: c.Timestamp,
			Snippet:   snippet,
			Score:     1.0,
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// handleRecentChats handles POST /v1/recent_chats.
func (h *Handlers) handleRecentChats(w http.ResponseWriter, r *http.Request) {
	var req RecentChatsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project_id is required")
		return
	}

	limit := h.policy.maxResults
	if req.Limit > 0 && req.Limit < limit {
		limit = req.Limit
	}

	chats, err := h.store.RecentChats(r.Context(), req.ProjectID, req.SessionID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to retrieve chats")
		return
	}
	if chats == nil {
		chats = []store.Chat{}
	}

	// Audit the retrieval.
	rh := make([]string, len(chats))
	for i, c := range chats {
		rh[i] = audit.HashChatID(c.ID)
	}
	h.logAudit(r, audit.Entry{
		ProjectID:    req.ProjectID,
		Action:       audit.ActionRetrieve,
		ResultHashes: rh,
		Metadata:     map[string]any{"result_count": len(chats)},
	})

	writeJSON(w, http.StatusOK, RecentChatsResponse{Chats: chats})
}

// handleDeleteChat handles DELETE /v1/chats/{id}.
func (h *Handlers) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	chatID := chi.URLParam(r, "id")
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project_id query param is required")
		return
	}
	if chatID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "chat id is required")
		return
	}

	if err := h.store.DeleteChat(r.Context(), projectID, chatID); err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to delete chat")
		return
	}

	h.logAudit(r, audit.Entry{
		ProjectID: projectID,
		Action:    audit.ActionDelete,
		Metadata:  map[string]any{"chat_id": chatID},
	})

	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteProject handles DELETE /v1/projects/{id}.
func (h *Handlers) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project id is required")
		return
	}

	count, err := h.store.DeleteProject(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to delete project")
		return
	}

	h.logAudit(r, audit.Entry{
		ProjectID: projectID,
		Action:    audit.ActionDelete,
		Metadata:  map[string]any{"project_id": projectID, "chats_deleted": count},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"project_id":    projectID,
		"chats_deleted": count,
	})
}

// handleAuditLogs handles POST /v1/audit/logs.
func (h *Handlers) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if h.auditor == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeInternal, "audit log not enabled")
		return
	}

	var req AuditLogsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project_id is required")
		return
	}

	limit := h.policy.maxResults
	if req.Limit > 0 && req.Limit < limit {
		limit = req.Limit
	}

	filter := audit.Filter{
		ProjectID: req.ProjectID,
		Action:    req.Action,
		Limit:     limit,
		Offset:    req.Offset,
	}
	if req.TimeRange != nil {
		filter.TimeRange = &audit.TimeRange{
			Start: req.TimeRange.Start,
			End:   req.TimeRange.End,
		}
	}

	entries, err := h.auditor.Query(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to query audit log")
		return
	}

	out := make([]AuditEntry, len(entries))
	for i, e := range entries {
		out[i] = AuditEntry{
			ID:           e.ID,
			Timestamp:    e.Timestamp,
			ProjectID:    e.ProjectID,
			Action:       e.Action,
			Actor:        e.Actor,
			QueryHash:    e.QueryHash,
			ResultHashes: e.ResultHashes,
			Metadata:     e.Metadata,
			PrevHash:     e.PrevHash,
			EntryHash:    e.EntryHash,
		}
	}
	writeJSON(w, http.StatusOK, AuditLogsResponse{Entries: out})
}

// handleAuditVerify handles POST /v1/audit/verify.
func (h *Handlers) handleAuditVerify(w http.ResponseWriter, r *http.Request) {
	if h.auditor == nil {
		writeError(w, http.StatusServiceUnavailable, ErrCodeInternal, "audit log not enabled")
		return
	}

	result, err := audit.Verify(r.Context(), h.auditor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "verification failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, VerifyAuditResponse{
		Valid:          result.Valid,
		FirstInvalidID: result.FirstInvalidID,
		Reason:         result.Reason,
		EntriesChecked: result.EntriesChecked,
	})
}

// handleExportProject handles GET /v1/projects/{id}/export.
// Query param ?format=ndjson (default) or ?format=json.
func (h *Handlers) handleExportProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project id is required")
		return
	}

	format := r.URL.Query().Get("format")
	switch format {
	case "", "ndjson":
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="`+projectID+`.ndjson"`)
		enc := json.NewEncoder(w)
		if err := h.store.ForEachChat(r.Context(), projectID, func(c store.Chat) error {
			return enc.Encode(c)
		}); err != nil {
			slog.Error("export ndjson: stream error", "project", projectID, "err", err)
		}
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="`+projectID+`.json"`)
		fmt.Fprintf(w, `{"project_id":%s,"exported_at":%s,"chats":[`,
			mustJSON(projectID), mustJSON(time.Now().UTC().Format(time.RFC3339)))
		first := true
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		if err := h.store.ForEachChat(r.Context(), projectID, func(c store.Chat) error {
			if !first {
				w.Write([]byte(",")) //nolint:errcheck
			}
			first = false
			return enc.Encode(c)
		}); err != nil {
			slog.Error("export json: stream error", "project", projectID, "err", err)
		}
		w.Write([]byte("]}")) //nolint:errcheck
	default:
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, `unknown format; use "ndjson" or "json"`)
		return
	}

	h.logAudit(r, audit.Entry{
		ProjectID: projectID,
		Action:    audit.ActionExport,
		Metadata:  map[string]any{"format": format},
	})
}

// handleGetRetention handles GET /v1/projects/{id}/retention.
func (h *Handlers) handleGetRetention(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project id is required")
		return
	}

	override, err := h.store.ProjectRetention(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to read retention")
		return
	}

	days := h.policy.defaultRetentionDays
	isOverride := override > 0
	if isOverride {
		days = override
	}

	writeJSON(w, http.StatusOK, RetentionResponse{
		ProjectID:     projectID,
		RetentionDays: days,
		IsOverride:    isOverride,
	})
}

// handleSetRetention handles PUT /v1/projects/{id}/retention.
func (h *Handlers) handleSetRetention(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "project id is required")
		return
	}

	var req RetentionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RetentionDays < 0 {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "retention_days must be >= 0")
		return
	}

	if err := h.store.SetProjectRetention(r.Context(), projectID, req.RetentionDays); err != nil {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to set retention")
		return
	}

	// Return the new effective retention.
	days := h.policy.defaultRetentionDays
	isOverride := req.RetentionDays > 0
	if isOverride {
		days = req.RetentionDays
	}
	writeJSON(w, http.StatusOK, RetentionResponse{
		ProjectID:     projectID,
		RetentionDays: days,
		IsOverride:    isOverride,
	})
}

// --- helpers -----------------------------------------------------------------

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "http: request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, ErrCodePayloadLimit, "request body exceeds 1MB")
			return false
		}
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON: "+msg)
		return false
	}
	return true
}

func matchSnippet(msgs []store.Message, queryLower string) string {
	for _, m := range msgs {
		lower := strings.ToLower(m.Content)
		idx := strings.Index(lower, queryLower)
		if idx == -1 {
			continue
		}
		start := idx - 40
		if start < 0 {
			start = 0
		}
		end := idx + len(queryLower) + 40
		if end > len(m.Content) {
			end = len(m.Content)
		}
		snippet := m.Content[start:end]
		if start > 0 {
			snippet = "..." + snippet
		}
		if end < len(m.Content) {
			snippet += "..."
		}
		return snippet
	}
	return ""
}

// hashQuery generates a deterministic SHA-256 hex string for audit linkage.
func hashQuery(projectID, query string, tr *TimeRange) string {
	h := sha256.New()
	h.Write([]byte(projectID))
	h.Write([]byte(query))
	if tr != nil {
		h.Write([]byte(tr.Start.UTC().Format(time.RFC3339)))
		h.Write([]byte(tr.End.UTC().Format(time.RFC3339)))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// resultHashes returns the SHA-256 hashes of the chat IDs in results.
func resultHashes(results []SearchResult) []string {
	hashes := make([]string, len(results))
	for i, r := range results {
		hashes[i] = audit.HashChatID(r.ChatID)
	}
	return hashes
}

// logAudit fires an audit log entry, ignoring errors (best-effort).
func (h *Handlers) logAudit(r *http.Request, entry audit.Entry) {
	if h.auditor == nil {
		return
	}
	if err := h.auditor.Log(r.Context(), entry); err != nil {
		slog.Warn("audit log failed", "action", entry.Action, "project", entry.ProjectID, "err", err)
	}
}
