// Package sqlite provides the SQLite-backed implementation of store.Store.
package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver

	"github.com/kumarlokesh/contextd/store"
	"github.com/kumarlokesh/contextd/store/sqlite/migrations"
)

// Store is a SQLite-backed implementation of store.Store.
type Store struct {
	db *sql.DB

	// Prepared statements cached for the lifetime of the store.
	stmtInsertProject *sql.Stmt
	stmtInsertSession *sql.Stmt
	stmtInsertChat    *sql.Stmt
	stmtGetChat       *sql.Stmt
	stmtRecentChats   *sql.Stmt
	stmtRecentBySession *sql.Stmt
	stmtDeleteChat    *sql.Stmt
	stmtDeleteProject *sql.Stmt
	stmtCountProject  *sql.Stmt
}

// Open opens (or creates) the SQLite database at path, runs migrations, and
// returns a ready Store.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}

	// SQLite does best with a single writer; allow many readers.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Apply pragmas explicitly — more portable than DSN params.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	if err := RunMigrations(db, migrations.FS); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}

	s := &Store{db: db}
	if err := s.prepare(); err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing statements: %w", err)
	}
	return s, nil
}

// prepare creates and caches all prepared statements.
func (s *Store) prepare() error {
	var err error

	s.stmtInsertProject, err = s.db.Prepare(
		`INSERT OR IGNORE INTO projects (id, created_at) VALUES (?, ?)`)
	if err != nil {
		return err
	}

	s.stmtInsertSession, err = s.db.Prepare(
		`INSERT OR IGNORE INTO sessions (id, project_id, created_at) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}

	s.stmtInsertChat, err = s.db.Prepare(
		`INSERT INTO chats (id, project_id, session_id, timestamp, messages, metadata, content_text)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}

	s.stmtGetChat, err = s.db.Prepare(
		`SELECT id, project_id, session_id, timestamp, messages, metadata
		 FROM chats WHERE project_id = ? AND id = ?`)
	if err != nil {
		return err
	}

	s.stmtRecentChats, err = s.db.Prepare(
		`SELECT id, project_id, session_id, timestamp, messages, metadata
		 FROM chats WHERE project_id = ?
		 ORDER BY timestamp DESC LIMIT ?`)
	if err != nil {
		return err
	}

	s.stmtRecentBySession, err = s.db.Prepare(
		`SELECT id, project_id, session_id, timestamp, messages, metadata
		 FROM chats WHERE project_id = ? AND session_id = ?
		 ORDER BY timestamp DESC LIMIT ?`)
	if err != nil {
		return err
	}

	s.stmtDeleteChat, err = s.db.Prepare(
		`DELETE FROM chats WHERE project_id = ? AND id = ?`)
	if err != nil {
		return err
	}

	s.stmtCountProject, err = s.db.Prepare(
		`SELECT COUNT(*) FROM chats WHERE project_id = ?`)
	if err != nil {
		return err
	}

	s.stmtDeleteProject, err = s.db.Prepare(
		`DELETE FROM projects WHERE id = ?`)
	if err != nil {
		return err
	}

	return nil
}

// StoreChat persists a chat. It auto-creates the project and session if they
// do not already exist.
func (s *Store) StoreChat(ctx context.Context, input store.ChatInput) (string, error) {
	chatID, err := newUUID()
	if err != nil {
		return "", fmt.Errorf("generating chat id: %w", err)
	}

	messagesJSON, err := json.Marshal(input.Messages)
	if err != nil {
		return "", fmt.Errorf("marshalling messages: %w", err)
	}

	var metadataJSON []byte
	if len(input.Metadata) > 0 {
		metadataJSON, err = json.Marshal(input.Metadata)
		if err != nil {
			return "", fmt.Errorf("marshalling metadata: %w", err)
		}
	}

	contentText := flattenMessages(input.Messages)
	ts := input.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	now := time.Now().UnixMilli()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.StmtContext(ctx, s.stmtInsertProject).ExecContext(
		ctx, input.ProjectID, now,
	); err != nil {
		return "", fmt.Errorf("insert project: %w", err)
	}

	if _, err := tx.StmtContext(ctx, s.stmtInsertSession).ExecContext(
		ctx, input.SessionID, input.ProjectID, now,
	); err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}

	if _, err := tx.StmtContext(ctx, s.stmtInsertChat).ExecContext(
		ctx,
		chatID,
		input.ProjectID,
		input.SessionID,
		ts.UnixMilli(),
		string(messagesJSON),
		nullableJSON(metadataJSON),
		contentText,
	); err != nil {
		return "", fmt.Errorf("insert chat: %w", err)
	}

	return chatID, tx.Commit()
}

// GetChat retrieves a single chat by project and chat ID.
func (s *Store) GetChat(ctx context.Context, projectID, chatID string) (*store.Chat, error) {
	row := s.stmtGetChat.QueryRowContext(ctx, projectID, chatID)
	chat, err := scanChat(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return chat, nil
}

// RecentChats returns up to limit chats for the project (newest first).
func (s *Store) RecentChats(ctx context.Context, projectID string, sessionID *string, limit int) ([]store.Chat, error) {
	var rows *sql.Rows
	var err error

	if sessionID != nil {
		rows, err = s.stmtRecentBySession.QueryContext(ctx, projectID, *sessionID, limit)
	} else {
		rows, err = s.stmtRecentChats.QueryContext(ctx, projectID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []store.Chat
	for rows.Next() {
		chat, err := scanChatRow(rows)
		if err != nil {
			return nil, err
		}
		chats = append(chats, *chat)
	}
	return chats, rows.Err()
}

// DeleteChat removes a single chat.
func (s *Store) DeleteChat(ctx context.Context, projectID, chatID string) error {
	_, err := s.stmtDeleteChat.ExecContext(ctx, projectID, chatID)
	return err
}

// DeleteProject removes all data for a project and returns the chat count deleted.
func (s *Store) DeleteProject(ctx context.Context, projectID string) (int, error) {
	var count int
	if err := s.stmtCountProject.QueryRowContext(ctx, projectID).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting chats: %w", err)
	}
	if _, err := s.stmtDeleteProject.ExecContext(ctx, projectID); err != nil {
		return 0, fmt.Errorf("delete project: %w", err)
	}
	return count, nil
}

// Close closes the database and releases all resources.
func (s *Store) Close() error {
	stmts := []*sql.Stmt{
		s.stmtInsertProject,
		s.stmtInsertSession,
		s.stmtInsertChat,
		s.stmtGetChat,
		s.stmtRecentChats,
		s.stmtRecentBySession,
		s.stmtDeleteChat,
		s.stmtCountProject,
		s.stmtDeleteProject,
	}
	for _, stmt := range stmts {
		if stmt != nil {
			stmt.Close()
		}
	}
	return s.db.Close()
}

// --- helpers -----------------------------------------------------------------

// scanChat scans a single *sql.Row into a Chat.
func scanChat(row *sql.Row) (*store.Chat, error) {
	var (
		c            store.Chat
		tsMillis     int64
		messagesJSON string
		metadataJSON sql.NullString
	)
	if err := row.Scan(&c.ID, &c.ProjectID, &c.SessionID, &tsMillis, &messagesJSON, &metadataJSON); err != nil {
		return nil, err
	}
	return hydrateChat(&c, tsMillis, messagesJSON, metadataJSON)
}

// scanChatRow scans a *sql.Rows cursor into a Chat.
func scanChatRow(rows *sql.Rows) (*store.Chat, error) {
	var (
		c            store.Chat
		tsMillis     int64
		messagesJSON string
		metadataJSON sql.NullString
	)
	if err := rows.Scan(&c.ID, &c.ProjectID, &c.SessionID, &tsMillis, &messagesJSON, &metadataJSON); err != nil {
		return nil, err
	}
	return hydrateChat(&c, tsMillis, messagesJSON, metadataJSON)
}

func hydrateChat(c *store.Chat, tsMillis int64, messagesJSON string, metadataJSON sql.NullString) (*store.Chat, error) {
	c.Timestamp = time.UnixMilli(tsMillis).UTC()

	if err := json.Unmarshal([]byte(messagesJSON), &c.Messages); err != nil {
		return nil, fmt.Errorf("unmarshalling messages: %w", err)
	}
	if metadataJSON.Valid && metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &c.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshalling metadata: %w", err)
		}
	}
	return c, nil
}

// flattenMessages concatenates all message content with " | " as separator.
func flattenMessages(msgs []store.Message) string {
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, " | ")
}

// nullableJSON returns nil if data is empty (for SQL nullable columns).
func nullableJSON(data []byte) any {
	if len(data) == 0 {
		return nil
	}
	return string(data)
}

// newUUID returns a random UUID v4 string using crypto/rand.
func newUUID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(buf[0:4]),
		hex.EncodeToString(buf[4:6]),
		hex.EncodeToString(buf[6:8]),
		hex.EncodeToString(buf[8:10]),
		hex.EncodeToString(buf[10:]),
	), nil
}
