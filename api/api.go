package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/kumarlokesh/contextd/audit"
	"github.com/kumarlokesh/contextd/search"
	"github.com/kumarlokesh/contextd/store"
)

// Router returns a chi.Mux with all v1 API routes mounted.
// Pass nil for sr to use the substring fallback; nil for al to disable audit.
// The caller should mount this under a prefix via server.MountAPI.
func Router(st store.Store, sr search.Searcher, al audit.Logger, maxResultsPerQuery int) *chi.Mux {
	h := NewHandlers(st, sr, al, maxResultsPerQuery)

	r := chi.NewRouter()
	r.Post("/store_chat", h.handleStoreChat)
	r.Post("/conversation_search", h.handleConversationSearch)
	r.Post("/recent_chats", h.handleRecentChats)
	r.Delete("/chats/{id}", h.handleDeleteChat)
	r.Delete("/projects/{id}", h.handleDeleteProject)
	r.Post("/audit/logs", h.handleAuditLogs)
	r.Post("/audit/verify", h.handleAuditVerify)
	return r
}
