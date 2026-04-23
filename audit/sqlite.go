package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// SQLiteLogger is a Logger backed by the audit_log table.
type SQLiteLogger struct {
	db         *sql.DB
	mu         sync.Mutex // serialises Log() to maintain chain ordering
	stmtInsert *sql.Stmt
	stmtLast   *sql.Stmt
}

// NewSQLiteLogger creates a SQLiteLogger that shares the given *sql.DB.
func NewSQLiteLogger(db *sql.DB) (*SQLiteLogger, error) {
	l := &SQLiteLogger{db: db}
	if err := l.prepare(); err != nil {
		return nil, fmt.Errorf("preparing audit statements: %w", err)
	}
	return l, nil
}

func (l *SQLiteLogger) prepare() error {
	var err error

	l.stmtInsert, err = l.db.Prepare(`
		INSERT INTO audit_log
			(timestamp, project_id, action, actor, query_hash, result_hashes,
			 metadata, prev_hash, entry_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}

	l.stmtLast, err = l.db.Prepare(
		`SELECT entry_hash FROM audit_log ORDER BY id DESC LIMIT 1`)
	return err
}

// Log appends an audit entry to the chain.
// Timestamp defaults to now if zero; Actor defaults to "anonymous" if empty.
// The method is serialised via an internal mutex to guarantee hash-chain order.
func (l *SQLiteLogger) Log(ctx context.Context, entry Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.Actor == "" {
		entry.Actor = "anonymous"
	}
	if entry.ResultHashes == nil {
		entry.ResultHashes = []string{}
	}

	metaJSON, err := json.Marshal(entry.Metadata)
	if err != nil || len(metaJSON) == 0 {
		metaJSON = []byte("{}")
	}
	rhJSON, err := json.Marshal(entry.ResultHashes)
	if err != nil {
		rhJSON = []byte("[]")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("audit log: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Read the previous entry's hash inside the same transaction.
	var prevHash string
	err = tx.StmtContext(ctx, l.stmtLast).QueryRowContext(ctx).Scan(&prevHash)
	if err == sql.ErrNoRows {
		prevHash = GenesisHash
	} else if err != nil {
		return fmt.Errorf("audit log: reading prev hash: %w", err)
	}

	entry.PrevHash = prevHash
	entry.EntryHash = ComputeEntryHash(entry, metaJSON)

	_, err = tx.StmtContext(ctx, l.stmtInsert).ExecContext(ctx,
		entry.Timestamp.UnixMilli(),
		entry.ProjectID,
		entry.Action,
		entry.Actor,
		entry.QueryHash,
		string(rhJSON),
		string(metaJSON),
		entry.PrevHash,
		entry.EntryHash,
	)
	if err != nil {
		return fmt.Errorf("audit log: insert: %w", err)
	}

	return tx.Commit()
}

// Query returns audit entries matching filter. Results are ordered by id DESC
// by default (newest first); set filter.Ascending for oldest-first order.
func (l *SQLiteLogger) Query(ctx context.Context, filter Filter) ([]Entry, error) {
	var conds []string
	var args []any

	if filter.ProjectID != "" {
		conds = append(conds, "project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.Action != "" {
		conds = append(conds, "action = ?")
		args = append(args, filter.Action)
	}
	if filter.TimeRange != nil {
		conds = append(conds, "timestamp BETWEEN ? AND ?")
		args = append(args, filter.TimeRange.Start.UnixMilli(), filter.TimeRange.End.UnixMilli())
	}

	dir := "DESC"
	if filter.Ascending {
		dir = "ASC"
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	q := `SELECT id, timestamp, project_id, action, actor, query_hash,
	             result_hashes, metadata, prev_hash, entry_hash
	      FROM audit_log`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY id " + dir
	q += " LIMIT ? OFFSET ?"
	args = append(args, limit, filter.Offset)

	rows, err := l.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("audit query: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var (
			e         Entry
			tsMillis  int64
			rhJSON    string
			metaJSON  string
		)
		if err := rows.Scan(
			&e.ID, &tsMillis, &e.ProjectID, &e.Action, &e.Actor,
			&e.QueryHash, &rhJSON, &metaJSON, &e.PrevHash, &e.EntryHash,
		); err != nil {
			return nil, err
		}
		e.Timestamp = time.UnixMilli(tsMillis).UTC()

		if err := json.Unmarshal([]byte(rhJSON), &e.ResultHashes); err != nil {
			slog.Warn("audit: parsing result_hashes", "id", e.ID, "err", err)
		}
		if err := json.Unmarshal([]byte(metaJSON), &e.Metadata); err != nil {
			slog.Warn("audit: parsing metadata", "id", e.ID, "err", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Close releases prepared statements. The *sql.DB is not closed here - it is
// owned by the store.
func (l *SQLiteLogger) Close() error {
	for _, s := range []*sql.Stmt{l.stmtInsert, l.stmtLast} {
		if s != nil {
			s.Close()
		}
	}
	return nil
}
