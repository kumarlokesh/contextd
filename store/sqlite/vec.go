package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/kumarlokesh/contextd/store"
)

// VecStore implements store.VectorStore using the sqlite-vec vec0 extension.
type VecStore struct {
	db              *sql.DB
	stmtInsert      *sql.Stmt
	stmtKNN         *sql.Stmt
	stmtPending     *sql.Stmt
	stmtSetStatus   *sql.Stmt
	stmtChatRowID   *sql.Stmt
}

// NewVecStore creates a VecStore that shares the given *sql.DB.
// Migration 003 must already have been applied (creates chats_vec and
// chats_embedding_status).
func NewVecStore(db *sql.DB) (*VecStore, error) {
	vs := &VecStore{db: db}
	if err := vs.prepare(); err != nil {
		return nil, fmt.Errorf("preparing vec statements: %w", err)
	}
	return vs, nil
}

func (vs *VecStore) prepare() error {
	var err error

	vs.stmtInsert, err = vs.db.Prepare(
		`INSERT OR REPLACE INTO chats_vec(rowid, embedding) VALUES (?, ?)`)
	if err != nil {
		return err
	}

	// KNN subquery gets ANN candidates; outer JOIN filters by project_id.
	vs.stmtKNN, err = vs.db.Prepare(`
		SELECT c.id, c.session_id, c.timestamp, knn.distance
		FROM (
			SELECT rowid, distance
			FROM chats_vec
			WHERE embedding MATCH ?
			  AND k = ?
			ORDER BY distance
		) knn
		JOIN chats c ON c.rowid = knn.rowid
		WHERE c.project_id = ?
		ORDER BY knn.distance
		LIMIT ?`)
	if err != nil {
		return err
	}

	vs.stmtPending, err = vs.db.Prepare(`
		SELECT ces.chat_id, ces.project_id, c.content_text
		FROM chats_embedding_status ces
		JOIN chats c ON c.id = ces.chat_id
		WHERE ces.status = 'pending'
		ORDER BY ces.created_at
		LIMIT ?`)
	if err != nil {
		return err
	}

	vs.stmtSetStatus, err = vs.db.Prepare(`
		UPDATE chats_embedding_status
		SET status = ?, updated_at = ?
		WHERE chat_id = ?`)
	if err != nil {
		return err
	}

	vs.stmtChatRowID, err = vs.db.Prepare(
		`SELECT rowid FROM chats WHERE id = ?`)
	return err
}

// InsertEmbedding stores a float32 embedding for the given chat rowid.
func (vs *VecStore) InsertEmbedding(ctx context.Context, chatRowID int64, embedding []float32) error {
	_, err := vs.stmtInsert.ExecContext(ctx, chatRowID, serializeFloat32(embedding))
	return err
}

// KNNSearch returns the nearest neighbours for the query embedding within
// the given project. fetchK is passed as the vec0 k= hint (pre-filter count);
// limit caps the final result set.
func (vs *VecStore) KNNSearch(ctx context.Context, projectID string, embedding []float32, fetchK, limit int) ([]store.VectorHit, error) {
	rows, err := vs.stmtKNN.QueryContext(ctx,
		serializeFloat32(embedding), fetchK, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("vec knn: %w", err)
	}
	defer rows.Close()

	var hits []store.VectorHit
	for rows.Next() {
		var (
			chatID    string
			sessionID string
			tsMillis  int64
			distance  float64
		)
		if err := rows.Scan(&chatID, &sessionID, &tsMillis, &distance); err != nil {
			return nil, err
		}
		hits = append(hits, store.VectorHit{
			ChatID:    chatID,
			SessionID: sessionID,
			Timestamp: time.UnixMilli(tsMillis).UTC(),
			Distance:  distance,
		})
	}
	return hits, rows.Err()
}

// PendingChats returns up to limit chats with status = 'pending'.
func (vs *VecStore) PendingChats(ctx context.Context, limit int) ([]store.PendingChat, error) {
	rows, err := vs.stmtPending.QueryContext(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("pending chats: %w", err)
	}
	defer rows.Close()

	var chats []store.PendingChat
	for rows.Next() {
		var pc store.PendingChat
		if err := rows.Scan(&pc.ChatID, &pc.ProjectID, &pc.ContentText); err != nil {
			return nil, err
		}
		chats = append(chats, pc)
	}
	return chats, rows.Err()
}

// SetEmbeddingStatus updates the embedding status for a chat.
func (vs *VecStore) SetEmbeddingStatus(ctx context.Context, chatID, status string) error {
	_, err := vs.stmtSetStatus.ExecContext(ctx, status, time.Now().UnixMilli(), chatID)
	return err
}

// ChatRowID returns the SQLite integer rowid for the given chat UUID.
func (vs *VecStore) ChatRowID(ctx context.Context, chatID string) (int64, error) {
	var rowid int64
	err := vs.stmtChatRowID.QueryRowContext(ctx, chatID).Scan(&rowid)
	return rowid, err
}

// Close releases prepared statements. The underlying *sql.DB is not closed
// here — it is owned by the store.
func (vs *VecStore) Close() error {
	for _, s := range []*sql.Stmt{
		vs.stmtInsert,
		vs.stmtKNN,
		vs.stmtPending,
		vs.stmtSetStatus,
		vs.stmtChatRowID,
	} {
		if s != nil {
			s.Close()
		}
	}
	return nil
}

// serializeFloat32 encodes a float32 slice as little-endian IEEE 754 bytes,
// the format expected by sqlite-vec for MATCH arguments and INSERT values.
func serializeFloat32(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
