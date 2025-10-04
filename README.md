# contextd

> **A transparent, privacy-first memory daemon for LLMs - explicit retrieval, hybrid search, and auditable context management.**

[![Rust](https://img.shields.io/badge/rust-stable-brightgreen.svg)](https://www.rust-lang.org/)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

## Table of Contents

- [contextd](#contextd)
  - [Table of Contents](#table-of-contents)
  - [Key Features](#key-features)
  - [Quick Start](#quick-start)
    - [1. Start the daemon](#1-start-the-daemon)
    - [2. Store your first conversation](#2-store-your-first-conversation)
    - [3. Search your memory](#3-search-your-memory)
    - [4. View audit logs](#4-view-audit-logs)
  - [Architecture](#architecture)
  - [Examples](#examples)
  - [Security \& Privacy](#security--privacy)
  - [Development](#development)
    - [Building from Source](#building-from-source)
    - [Running Examples](#running-examples)
  - [License](#license)

## Key Features

- **Privacy-First**: Local SQLite storage with optional encryption
- **Explicit Memory**: No silent context injection - all retrieval is via API calls
- **Hybrid Search**: Full-text (Tantivy) + optional vector search with temporal ranking
- **Audit Trail**: Every memory access logged with timestamps and result hashes
- **Lightweight**: Single binary daemon with REST API
- **Local-First**: Full offline operation, optional remote deployment

## Quick Start

### 1. Start the daemon

```bash
# Clone and build
git clone https://github.com/kumarlokesh/contextd
cd contextd
cargo run -- serve

# Server starts on http://127.0.0.1:8080
```

### 2. Store your first conversation

```bash
# Using the CLI client
cargo run --bin contextctl -- store \
  --project "my-project" \
  --session "session-123" \
  --user "How do I learn Rust?" \
  --assistant "Start with the Rust Book and practice with small projects!"
```

### 3. Search your memory

```bash
# Search across all conversations
cargo run --bin contextctl -- search \
  --project "my-project" \
  --query "Rust programming" \
  --limit 5
```

### 4. View audit logs

```bash
# See what memory was accessed
cargo run --bin contextctl -- audit \
  --project "my-project" \
  --limit 10
```

## Architecture

**[Architecture Documentation](docs/ARCHITECTURE.md)**

## Examples

contextd includes comprehensive Rust examples:

- **Basic Integration** - Core API usage, health checks, audit logging
- **LLM Memory Integration** - Memory-aware response generation
- **Batch Operations** - Concurrent processing, error handling, performance testing

**[See Examples Directory](examples/)** for complete working code and API patterns.

## Security & Privacy

- **Local-first**: All data stays on your machine by default
- **Explicit retrieval**: No automatic context injection
- **User control**: Delete/export APIs for data sovereignty
- **Optional encryption**: Per-user keyring support

## Development

### Building from Source

```bash
# Clone repository
git clone https://github.com/kumarlokesh/contextd
cd contextd

# Build and run tests
cargo build --release
cargo test

# Start development server
cargo run -- serve --config contextd.toml

# Run CLI client
cargo run --bin contextctl -- health
```

### Running Examples

```bash
# Start the daemon
make serve
# or: cargo run -- serve

# In another terminal, run the examples
make run-basic-example      # Basic integration demo
make run-llm-example        # LLM memory integration demo  
make run-batch-example      # Batch operations demo
make demo                   # Run all examples in sequence

# Or run examples manually:
cd examples
cargo run --bin basic_integration
cargo run --bin llm_memory_demo
cargo run --bin batch_operations
```

## License

Apache 2.0 License - see [LICENSE](LICENSE) for details.
