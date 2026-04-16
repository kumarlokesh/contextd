package sqlite

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kumarlokesh/contextd/store"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreAndRetrieve(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	input := store.ChatInput{
		ProjectID: "proj-1",
		SessionID: "sess-1",
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		Messages: []store.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
		Metadata: map[string]any{"source": "test"},
	}

	id, err := s.StoreChat(ctx, input)
	if err != nil {
		t.Fatalf("StoreChat: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty chat ID")
	}

	got, err := s.GetChat(ctx, "proj-1", id)
	if err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	if got == nil {
		t.Fatal("expected chat, got nil")
	}
	if got.ProjectID != input.ProjectID {
		t.Errorf("ProjectID mismatch: got %q want %q", got.ProjectID, input.ProjectID)
	}
	if got.SessionID != input.SessionID {
		t.Errorf("SessionID mismatch: got %q want %q", got.SessionID, input.SessionID)
	}
	if len(got.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(got.Messages))
	}
	if got.Metadata["source"] != "test" {
		t.Errorf("metadata mismatch: got %v", got.Metadata)
	}
}

func TestGetChatNotFound(t *testing.T) {
	s := openTestStore(t)
	got, err := s.GetChat(context.Background(), "proj-x", "no-such-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing chat, got %+v", got)
	}
}

func TestRecentChatsOrdering(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 5; i++ {
		_, err := s.StoreChat(ctx, store.ChatInput{
			ProjectID: "proj-order",
			SessionID: "sess",
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Messages:  []store.Message{{Role: "user", Content: "msg"}},
		})
		if err != nil {
			t.Fatalf("StoreChat[%d]: %v", i, err)
		}
	}

	chats, err := s.RecentChats(ctx, "proj-order", nil, 10)
	if err != nil {
		t.Fatalf("RecentChats: %v", err)
	}
	if len(chats) != 5 {
		t.Fatalf("expected 5 chats, got %d", len(chats))
	}
	// First result should be newest.
	for i := 1; i < len(chats); i++ {
		if chats[i].Timestamp.After(chats[i-1].Timestamp) {
			t.Errorf("chats not in descending order at index %d", i)
		}
	}
}

func TestRecentChatsBySession(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for _, sess := range []string{"A", "B"} {
		for i := 0; i < 3; i++ {
			if _, err := s.StoreChat(ctx, store.ChatInput{
				ProjectID: "proj-sess",
				SessionID: sess,
				Messages:  []store.Message{{Role: "user", Content: sess}},
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	sessA := "A"
	chats, err := s.RecentChats(ctx, "proj-sess", &sessA, 10)
	if err != nil {
		t.Fatalf("RecentChats by session: %v", err)
	}
	if len(chats) != 3 {
		t.Errorf("expected 3 chats for session A, got %d", len(chats))
	}
	for _, c := range chats {
		if c.SessionID != "A" {
			t.Errorf("unexpected session %q in session-filtered results", c.SessionID)
		}
	}
}

func TestDeleteChat(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, _ := s.StoreChat(ctx, store.ChatInput{
		ProjectID: "proj-del",
		SessionID: "sess",
		Messages:  []store.Message{{Role: "user", Content: "bye"}},
	})

	if err := s.DeleteChat(ctx, "proj-del", id); err != nil {
		t.Fatalf("DeleteChat: %v", err)
	}
	got, _ := s.GetChat(ctx, "proj-del", id)
	if got != nil {
		t.Error("expected nil after deletion")
	}
}

func TestDeleteProject(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		s.StoreChat(ctx, store.ChatInput{
			ProjectID: "proj-nuke",
			SessionID: "sess",
			Messages:  []store.Message{{Role: "user", Content: "x"}},
		})
	}

	n, err := s.DeleteProject(ctx, "proj-nuke")
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if n != 4 {
		t.Errorf("expected 4 deleted, got %d", n)
	}

	chats, _ := s.RecentChats(ctx, "proj-nuke", nil, 10)
	if len(chats) != 0 {
		t.Errorf("expected 0 chats after project deletion, got %d", len(chats))
	}
}

func TestConcurrentWrites(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	const goroutines = 10
	const perGoroutine = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				_, err := s.StoreChat(ctx, store.ChatInput{
					ProjectID: "proj-concurrent",
					SessionID: "sess",
					Messages:  []store.Message{{Role: "user", Content: "concurrent"}},
				})
				if err != nil {
					t.Errorf("concurrent StoreChat: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	chats, err := s.RecentChats(ctx, "proj-concurrent", nil, goroutines*perGoroutine+1)
	if err != nil {
		t.Fatalf("RecentChats: %v", err)
	}
	if len(chats) != goroutines*perGoroutine {
		t.Errorf("expected %d chats, got %d", goroutines*perGoroutine, len(chats))
	}
}

func TestReopenPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	var chatID string
	{
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		chatID, _ = s.StoreChat(context.Background(), store.ChatInput{
			ProjectID: "proj-persist",
			SessionID: "sess",
			Messages:  []store.Message{{Role: "user", Content: "hello"}},
		})
		s.Close()
	}
	{
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		got, err := s.GetChat(context.Background(), "proj-persist", chatID)
		if err != nil {
			t.Fatalf("GetChat after reopen: %v", err)
		}
		if got == nil {
			t.Fatal("data not persisted after close/reopen")
		}
	}
}
