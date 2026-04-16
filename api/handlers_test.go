package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	sqlitestore "github.com/kumarlokesh/contextd/store/sqlite"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return httptest.NewServer(Router(st, 100))
}

func post(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestStoreChatHappyPath(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp := post(t, srv, "/store_chat", map[string]any{
		"project_id": "p1",
		"session_id": "s1",
		"messages":   []map[string]string{{"role": "user", "content": "hello"}},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	var out StoreChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ChatID == "" {
		t.Error("expected non-empty chat_id")
	}
}

func TestStoreChatMissingProjectID(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp := post(t, srv, "/store_chat", map[string]any{
		"session_id": "s1",
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	var errResp ErrorResponse
	json.NewDecoder(resp.Body).Decode(&errResp)
	if errResp.Error.Code != ErrCodeBadRequest {
		t.Errorf("unexpected error code %q", errResp.Error.Code)
	}
}

func TestStoreChatMalformedJSON(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/store_chat", "application/json",
		bytes.NewBufferString("{not valid json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", resp.StatusCode)
	}
}

func TestRecentChatsHappyPath(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Store 3 chats.
	for i := 0; i < 3; i++ {
		post(t, srv, "/store_chat", map[string]any{
			"project_id": "proj-recent",
			"session_id": "sess",
			"messages":   []map[string]string{{"role": "user", "content": "msg"}},
		})
	}

	resp := post(t, srv, "/recent_chats", map[string]any{
		"project_id": "proj-recent",
		"limit":      10,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var out RecentChatsResponse
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Chats) != 3 {
		t.Errorf("expected 3 chats, got %d", len(out.Chats))
	}
}

func TestConversationSearchHappyPath(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	post(t, srv, "/store_chat", map[string]any{
		"project_id": "proj-search",
		"session_id": "sess",
		"messages":   []map[string]string{{"role": "user", "content": "golang is great"}},
	})

	resp := post(t, srv, "/conversation_search", map[string]any{
		"project_id": "proj-search",
		"query":      "golang",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	var out SearchResponse
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(out.Results))
	}
	if out.QueryHash == "" {
		t.Error("expected non-empty query_hash")
	}
}

func TestConversationSearchProjectIsolation(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	post(t, srv, "/store_chat", map[string]any{
		"project_id": "proj-A",
		"session_id": "sess",
		"messages":   []map[string]string{{"role": "user", "content": "secret data"}},
	})

	// Search under a different project should not return proj-A chats.
	resp := post(t, srv, "/conversation_search", map[string]any{
		"project_id": "proj-B",
		"query":      "secret",
	})
	defer resp.Body.Close()

	var out SearchResponse
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Results) != 0 {
		t.Errorf("expected 0 results for different project, got %d", len(out.Results))
	}
}

func TestConversationSearchTimeRange(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)

	post(t, srv, "/store_chat", map[string]any{
		"project_id": "proj-time",
		"session_id": "sess",
		"timestamp":  old,
		"messages":   []map[string]string{{"role": "user", "content": "old memory"}},
	})
	post(t, srv, "/store_chat", map[string]any{
		"project_id": "proj-time",
		"session_id": "sess",
		"timestamp":  recent,
		"messages":   []map[string]string{{"role": "user", "content": "recent memory"}},
	})

	// Only search within the last 2 hours.
	resp := post(t, srv, "/conversation_search", map[string]any{
		"project_id": "proj-time",
		"query":      "memory",
		"time_range": map[string]any{
			"start": time.Now().Add(-2 * time.Hour),
			"end":   time.Now(),
		},
	})
	defer resp.Body.Close()

	var out SearchResponse
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Results) != 1 {
		t.Errorf("expected 1 result within time range, got %d", len(out.Results))
	}
}

func TestDeleteChatHappyPath(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	storeResp := post(t, srv, "/store_chat", map[string]any{
		"project_id": "proj-del",
		"session_id": "sess",
		"messages":   []map[string]string{{"role": "user", "content": "delete me"}},
	})
	var stored StoreChatResponse
	json.NewDecoder(storeResp.Body).Decode(&stored)
	storeResp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete,
		srv.URL+"/chats/"+stored.ChatID+"?project_id=proj-del", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestDeleteProjectHappyPath(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	for i := 0; i < 3; i++ {
		post(t, srv, "/store_chat", map[string]any{
			"project_id": "proj-nuke",
			"session_id": "sess",
			"messages":   []map[string]string{{"role": "user", "content": "x"}},
		})
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/projects/proj-nuke", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if out["chats_deleted"].(float64) != 3 {
		t.Errorf("expected chats_deleted=3, got %v", out["chats_deleted"])
	}
}
