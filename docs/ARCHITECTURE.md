# contextd Architecture

## Overview

contextd is a privacy-first memory daemon for LLMs, designed around explicit retrieval rather than silent context injection.

## Core Components

```
┌──────────────────────┐
│  LLM / Agent Client  │
└───────┬──────────────┘
        │ (REST API)
        ▼
┌─────────────────────┐
│   contextd API      │
│  (Axum HTTP svc)    │
└─────┬───────────────┘
      │
┌─────┼─────────────────┐
▼                       ▼
┌─────────────┐  ┌──────────────┐
│ MemoryStore │  │ IndexEngine  │
│  (SQLite)   │  │ (Tantivy +   │
│ transcripts │  │ optional vec)│
└─────────────┘  └──────────────┘
      │
      ▼
┌─────────────────────┐
│    Audit Logs       │
└─────────────────────┘
```

### MemoryStore
- **Purpose**: Append-only transcript storage
- **Technology**: SQLite (local) / Postgres (remote)
- **Schema**: User sessions, project isolation, metadata
- **Features**: Compression, retention policies, GDPR compliance

### IndexEngine
- **Full-text**: Tantivy for fast keyword search
- **Vector**: Optional Qdrant/HNSW for semantic search
- **Hybrid ranking**: `score = α * BM25 + β * semantic + γ * temporal_decay`
- **Real-time**: Incremental indexing on new transcripts

### Broker API
- **Endpoints**: `/store_chat`, `/conversation_search`, `/recent_chats`
- **Protocol**: REST (HTTP/JSON) with optional gRPC
- **Auth**: API keys with project namespacing
- **Rate limiting**: Per-key limits to prevent abuse

### Policy Layer
- **Access control**: Per-project user isolation
- **Retention**: Configurable TTL and size limits
- **Privacy**: Delete, export, audit APIs
- **Encryption**: Optional per-user keyring

### Audit Log
- **Immutable log**: All retrieval calls + returned snippet hashes
- **Queryable**: `/audit/logs` API for transparency
- **Compliance**: GDPR-ready logging with retention controls

## Data Flow

1. **Storage**: Client → `POST /store_chat` → MemoryStore + IndexEngine
2. **Retrieval**: Client → `POST /conversation_search` → IndexEngine → MemoryStore → Client
3. **Audit**: All retrievals logged with timestamp, query, and result hashes

## Security Model

- **Explicit retrieval**: No automatic context injection
- **Project isolation**: API keys map to project namespaces
- **Audit trail**: Every memory access logged and queryable
- **User control**: Delete/export APIs for data sovereignty
- **Local-first**: Full offline operation in SQLite mode

## Configuration

```toml
# contextd.toml
[server]
host = "127.0.0.1"
port = 8080

[storage]
type = "sqlite"  # or "postgres"
path = "./data/contextd.db"
compression = true

[search]
engine = "tantivy"
vector_search = false  # optional

[policy]
default_retention_days = 90
max_results_per_query = 100

[audit]
enabled = true
retention_days = 365
```

## API Specification

### Store Chat
```http
POST /store_chat
Content-Type: application/json

{
  "project_id": "my-project",
  "session_id": "session-123",
  "timestamp": "2024-01-01T12:00:00Z",
  "messages": [
    {"role": "user", "content": "Hello"},
    {"role": "assistant", "content": "Hi there!"}
  ],
  "metadata": {"source": "cli", "version": "1.0"}
}
```

### Search Conversations
```http
POST /conversation_search
Content-Type: application/json

{
  "project_id": "my-project",
  "query": "machine learning model",
  "max_results": 10,
  "time_range": {
    "start": "2024-01-01T00:00:00Z",
    "end": "2024-01-31T23:59:59Z"
  }
}
```

### Recent Chats
```http
POST /recent_chats
Content-Type: application/json

{
  "project_id": "my-project",
  "limit": 20,
  "session_id": "session-123"  // optional
}
```

## Deployment

- **Local daemon**: `contextd serve --config contextd.toml`
- **Container**: Docker image with health checks
- **Service**: systemd unit for production deployment
- **Scaling**: Read replicas via Postgres, write-through cache
