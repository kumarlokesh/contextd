package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/kumarlokesh/contextd/store"
)

// Router returns a chi.Mux with all v1 API routes mounted.
// The caller should mount this under a prefix via server.MountAPI.
func Router(st store.Store, maxResultsPerQuery int) *chi.Mux {
	h := NewHandlers(st, maxResultsPerQuery)

	r := chi.NewRouter()
	r.Post("/store_chat", h.handleStoreChat)
	r.Post("/conversation_search", h.handleConversationSearch)
	r.Post("/recent_chats", h.handleRecentChats)
	r.Delete("/chats/{id}", h.handleDeleteChat)
	r.Delete("/projects/{id}", h.handleDeleteProject)
	return r
}
