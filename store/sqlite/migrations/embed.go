// Package migrations embeds the SQL migration files for the SQLite store.
package migrations

import "embed"

// FS holds the embedded migration SQL files.
//
//go:embed *.sql
var FS embed.FS
