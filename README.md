# contextd

> **A transparent, privacy-first memory daemon for LLMs - explicit retrieval, hybrid search, auditable context management.**

[![Go](https://img.shields.io/badge/go-1.24-00ADD8.svg)](https://golang.org/)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

## What is contextd

contextd is a local daemon that stores and retrieves LLM conversation history. It is deliberately not a library: running as a separate process means every memory access goes through an explicit API call, is logged, and can be audited. Think `ssh-agent` for LLM memory.

Single binary. Single SQLite database for transcripts, full-text search, vectors, and audit log. No cloud, no telemetry.

## Key features

- **Explicit retrieval** - no silent context injection; your agent decides when to query memory
- **Local-first** - SQLite on disk, full offline operation; Postgres backend _(coming in a later phase)_
- **Hybrid search** - SQLite FTS5 (BM25) + sqlite-vec (semantic) + temporal decay, all in one database
- **Hash-chained audit log** - every search and retrieval is recorded; the chain detects tampering _(coming in a later phase)_
- **MCP server** - consumable by Claude Desktop, Cursor, and any MCP client _(coming in a later phase)_
- **Privacy APIs** - export and delete by project, per-project retention _(coming in a later phase)_

## Quick start

```bash
# Build
git clone https://github.com/kumarlokesh/contextd
cd contextd
make build

# Start the daemon (config is optional; falls back to defaults)
./bin/contextd serve

# Or with a custom port
./bin/contextd serve --port 9090

# Write a default config file to disk
./bin/contextd init-config
```

The daemon starts on `http://127.0.0.1:8080` by default.

## REST API

### Store a conversation

```bash
curl -s -X POST http://localhost:8080/v1/store_chat \
  -H 'Content-Type: application/json' \
  -d '{
    "project_id": "my-project",
    "session_id": "session-123",
    "messages": [
      {"role": "user",      "content": "How do I learn Go?"},
      {"role": "assistant", "content": "Start with the Tour of Go, then write small CLI tools."}
    ]
  }'
# {"chat_id":"...","stored_at":"..."}
```

### Search conversations

```bash
curl -s -X POST http://localhost:8080/v1/conversation_search \
  -H 'Content-Type: application/json' \
  -d '{"project_id": "my-project", "query": "Go programming", "max_results": 5}'
# {"results":[...],"query_hash":"...","took_ms":1}
```

### Get recent chats

```bash
curl -s -X POST http://localhost:8080/v1/recent_chats \
  -H 'Content-Type: application/json' \
  -d '{"project_id": "my-project", "limit": 20}'
```

### Delete a chat

```bash
curl -s -X DELETE 'http://localhost:8080/v1/chats/<chat_id>?project_id=my-project'
```

### Delete a project (cascading)

```bash
curl -s -X DELETE http://localhost:8080/v1/projects/my-project
# {"project_id":"my-project","chats_deleted":42}
```

### Health and version

```bash
curl http://localhost:8080/health   # {"status":"ok","uptime_seconds":N}
curl http://localhost:8080/version  # {"version":"...","commit":"...","build_date":"..."}
```

## Configuration

```bash
# Write defaults to contextd.yaml, then edit as needed
./bin/contextd init-config
```

```yaml
server:
  host: "127.0.0.1"
  port: 8080

storage:
  type: sqlite
  path: ./data/contextd.db

search:
  full_text: true
  vector: false       # sqlite-vec hybrid search coming in M5
  hybrid_alpha: 0.5   # BM25 weight
  hybrid_beta: 0.4    # vector weight
  hybrid_gamma: 0.1   # temporal decay weight

policy:
  default_retention_days: 90
  max_results_per_query: 100

audit:
  enabled: true
  retention_days: 365
```

## Architecture

**[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)** has a detailed overview of the internal design, data flow, and component interactions. Here's a high-level summary:

```text
┌─────────────────────────────┐
│   LLM / Agent / MCP client  │
└──────────────┬──────────────┘
               │ REST (v1) or MCP (future)
               ▼
┌─────────────────────────────┐
│        contextd daemon      │
│   (chi router, slog)        │
└──────┬───────────┬──────────┘
       │           │
       ▼           ▼
┌───────────┐  ┌────────────┐
│  Store    │  │  Searcher  │
│  SQLite   │  │  FTS5      │
│  WAL mode │  │  +vec      │
└───────────┘  └────────────┘
       │
       ▼
┌─────────────────────────────┐
│  Audit log                  │
│  hash-chained, tamper-detect│
└─────────────────────────────┘
```

One SQLite database holds transcripts, FTS5 index, vector embeddings, and the audit log.

## License

Apache 2.0 — see [LICENSE](LICENSE).
