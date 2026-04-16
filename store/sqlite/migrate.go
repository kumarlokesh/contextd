package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RunMigrations applies pending SQL migrations from an embed.FS.
// Files must be named NNN_*.sql (e.g., 001_initial.sql) and are applied in
// lexical order. Already-applied versions are skipped; each migration is
// wrapped in a transaction.
func RunMigrations(db *sql.DB, fsys embed.FS) error {
	// Ensure schema_migrations exists. This is always safe to run first
	// because the table is created in 001_initial.sql — but we need it before
	// we can check what's already been applied.
	const ensureTable = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		);`
	if _, err := db.Exec(ensureTable); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	// Collect migration file names in lexical order.
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	// Fetch already-applied versions.
	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	for _, name := range files {
		version, err := versionFromName(name)
		if err != nil {
			return fmt.Errorf("migration %q has invalid name format: %w", name, err)
		}
		if applied[version] {
			continue
		}

		data, err := fsys.ReadFile(name)
		if err != nil {
			return fmt.Errorf("reading migration %q: %w", name, err)
		}

		if err := applyMigration(db, version, string(data)); err != nil {
			return fmt.Errorf("applying migration %q: %w", name, err)
		}
	}
	return nil
}

func appliedVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("querying schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func applyMigration(db *sql.DB, version int, sql string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(sql); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
		version, time.Now().UnixMilli(),
	); err != nil {
		return fmt.Errorf("recording version: %w", err)
	}
	return tx.Commit()
}

// versionFromName extracts the numeric prefix from a migration file name.
// e.g., "001_initial.sql" → 1.
func versionFromName(name string) (int, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) < 2 {
		return 0, fmt.Errorf("name must match NNN_*.sql")
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("numeric prefix: %w", err)
	}
	return n, nil
}
