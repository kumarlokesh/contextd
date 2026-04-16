# contextd Architecture

## Overview

contextd is a privacy-first memory daemon for LLMs built around explicit retrieval. It runs as a standalone Go binary and exposes a REST API (and, in a later phase, an MCP server). Every memory read goes through that API, is validated, and is recorded in an append-only audit log.

The key design choice: **one SQLite database for everything** - transcripts, FTS5 full-text index, sqlite-vec vectors, and the audit log. No separate index process, no network database for the default deployment.

## Component diagram

```text
┌─────────────────────────────────────────┐
│         LLM / Agent / MCP client        │
└─────────────────────┬───────────────────┘
                      │ HTTP/JSON  (MCP in future)
                      ▼
┌─────────────────────────────────────────┐
│              contextd daemon            │
│                                         │
│  cmd/contextd   ←  config/config.go     │
│  server/        ←  chi router           │
│  api/           ←  handlers + types     │
└────────────┬──────────────┬─────────────┘
             │              │
             ▼              ▼
   ┌──────────────┐  ┌─────────────────┐
   │  store.Store │  │ search.Searcher │
   │  (interface) │  │  (interface)    │
   └──────┬───────┘  └───────┬─────────┘
          │                  │
          ▼                  ▼
   ┌──────────────────────────────────┐
   │          SQLite database         │
   │                                  │
   │  chats / sessions / projects     │  ← store/sqlite/
   │  chats_fts  (FTS5)               │  ← search/sqlite_fts.go
   │  chats_vec  (sqlite-vec)         │  ← search/hybrid.go
   │  audit_log  (hash-chained)       │  ← audit/sqlite.go
   └──────────────────────────────────┘
```

## Package layout

| Package | Responsibility |
| --- | --- |
| `cmd/contextd` | Binary entry point; `serve`, `version`, `init-config` subcommands |
| `config` | YAML config loading with typed defaults |
| `server` | chi HTTP server lifecycle, middleware stack, `/health`, `/version` |
| `api` | HTTP handlers, request/response types, error envelope |
| `store` | `Store` interface + shared types (`Chat`, `Message`, `ChatInput`) |
| `store/sqlite` | SQLite implementation, migrations runner, prepared statements |
| `search` | `Searcher` interface |
| `search/sqlite_fts` | FTS5 BM25 searcher |
| `search/hybrid` | α·BM25 + β·vector + γ·temporal_decay ranker |
| `embed` | `Embedder` interface, Ollama + OpenAI implementations |
| `audit` | `Logger` interface, hash-chained SQLite implementation |
| `privacy` | Streaming export, retention enforcer |
| `mcp` | MCP JSON-RPC server, stdio + SSE transports |
| `integrations/agentflow` | agentflow `MemoryProvider` adapter (separate module) |

## SQLite schema

All schema changes are forward-only numbered migrations in `store/sqlite/migrations/`.

```sql
-- 001_initial.sql (current)
projects   (id, created_at, metadata)
sessions   (id, project_id, created_at)
chats      (id, project_id, session_id, timestamp, messages JSON,
            metadata JSON, content_text)   -- content_text feeds FTS5

-- 002_fts.sql
chats_fts  USING fts5(content_text, content='chats', tokenize='porter unicode61')
-- INSERT/UPDATE/DELETE triggers keep FTS in sync automatically

-- 003_vec.sql
chats_vec  USING vec0(chat_rowid INTEGER PRIMARY KEY, embedding FLOAT[384])
chats_embedding_status  (chat_id, status, embedded_at, error)

-- 004_audit.sql
audit_log  (id, timestamp, project_id, action, actor,
            query_hash, result_hashes JSON, metadata JSON,
            prev_hash, entry_hash)          -- hash chain
```

Timestamps are stored as Unix epoch milliseconds (`INTEGER`). JSON blobs as `TEXT`. Cascade deletes on project removal.

## SQLite driver strategy

| Phase | Driver | Reason |
| --- | --- | --- |
| Upcoming | `modernc.org/sqlite` | Pure Go, zero CGo, easy cross-compilation |
| Upcoming | `mattn/go-sqlite3` | CGo required to load the sqlite-vec extension |

The `store.Store` and `search.Searcher` interfaces are unchanged across the switch; only the driver import and the `Open()` call change.

## Data flows

### Write path - `POST /v1/store_chat`

```text
client → handler validates fields
       → store.StoreChat()
           → BEGIN transaction
           → INSERT OR IGNORE INTO projects
           → INSERT OR IGNORE INTO sessions
           → INSERT INTO chats  (content_text = joined message content)
           → COMMIT
           → FTS5 trigger fires automatically
       → async embed worker picks up 'pending' row
       → return chat_id
```

### Read path - `POST /v1/conversation_search`

```text
client → handler validates fields
       → search.Searcher.Search()
           → parallel: FTS query + vector KNN
           → merge + hybrid rank
       → audit.Logger.Log(action='search')
       → return results + query_hash
```

### Audit chain - each entry

```text
entry_hash = SHA-256(
    timestamp_ms ‖ project_id ‖ action ‖ actor
    ‖ query_hash ‖ result_hashes ‖ metadata_json
    ‖ prev_entry_hash
)
```

The first entry uses `prev_hash = "000...0"` (64 zeros). `audit/verify.go` walks the chain and recomputes every hash to detect tampering.

## HTTP API

All endpoints live under `/v1/`. Errors use a consistent envelope:

```json
{"error": {"code": "BAD_REQUEST", "message": "project_id is required"}}
```

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/v1/store_chat` | Store a conversation |
| `POST` | `/v1/conversation_search` | Search by query (FTS + hybrid in future) |
| `POST` | `/v1/recent_chats` | List recent chats, optionally filtered by session |
| `DELETE` | `/v1/chats/{id}` | Delete a single chat |
| `DELETE` | `/v1/projects/{id}` | Delete a project and all its data |
| `POST` | `/v1/audit/logs` | Query audit log |
| `POST` | `/v1/audit/verify` | Verify hash chain integrity |
| `GET` | `/v1/projects/{id}/retention` | Get per-project retention |
| `PUT` | `/v1/projects/{id}/retention` | Set per-project retention |
| `GET` | `/health` | Liveness probe |
| `GET` | `/version` | Build metadata |

Request body limit is 1 MB. All handlers accept `context.Context` from the request and thread it through to the store.

## Search strategy

**Current:** substring match fallback - candidates fetched via `RecentChats`, filtered in Go. Returns results but no BM25 ranking.

**Upcoming - FTS5:** `chats_fts` virtual table with porter stemmer. `bm25()` ranking. `snippet()` for highlights. All triggered automatically on INSERT/UPDATE/DELETE.

**Upcoming - Hybrid:**

```text
final_score = α·normalize(bm25)
            + β·normalize(vector_similarity)
            + γ·temporal_decay(timestamp)

temporal_decay(t) = exp(−0.05 · (now − t) / 86_400_000)   // ~14-day half-life
```

FTS and vector queries run in parallel via `errgroup`. Results merged on `chat_id`.

## Config

YAML (`contextd.yaml`). Missing file → defaults + `slog.Warn`. Missing fields → defaults. Invalid YAML → error.

```yaml
server:
  host: "127.0.0.1"
  port: 8080

storage:
  type: sqlite
  path: ./data/contextd.db
  compression: false

search:
  full_text: true
  vector: false
  hybrid_alpha: 0.5   # must sum to ~1.0
  hybrid_beta: 0.4
  hybrid_gamma: 0.1

policy:
  default_retention_days: 90
  max_results_per_query: 100

audit:
  enabled: true
  retention_days: 365
```

`CONTEXTD_LOG_LEVEL` env var overrides log level (`debug`, `info`, `warn`, `error`). JSON log output via `log/slog`.

## Deployment

```bash
# Local
./bin/contextd serve --config contextd.yaml

# Docker (with SQLite persistence in a volume)
docker run -p 8080:8080 -v contextd_data:/var/lib/contextd ghcr.io/kumarlokesh/contextd

# systemd
systemctl start contextd
```

Cross-platform binaries (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64) via GoReleaser in future releases.
