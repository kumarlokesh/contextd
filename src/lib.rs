/*!
# contextd Library

This crate provides the core functionality for contextd, a transparent, privacy-first memory daemon for LLMs.

## Modules

- [`config`] - Configuration management
- [`storage`] - Memory storage layer
- [`search`] - Full-text and vector search
- [`audit`] - Audit logging and transparency
- [`api`] - HTTP API endpoints
- [`models`] - Data models and types
- [`error`] - Error types and handling

## Features

- **Privacy-First**: Local SQLite storage with optional encryption
- **Explicit Memory**: No silent context injection - all retrieval is via API calls
- **Hybrid Search**: Full-text (Tantivy) + optional vector search with temporal ranking
- **Audit Trail**: Every memory access logged with timestamps and result hashes
- **Local-First**: Full offline operation, optional remote deployment

## Example

```rust
use contextd::{Config, ApiServer};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let config = Config::load("contextd.toml")?;
    let server = ApiServer::new(config).await?;
    server.serve().await?;
    Ok(())
}
```
*/

pub mod api;
pub mod audit;
pub mod config;
pub mod error;
pub mod models;
pub mod search;
pub mod storage;

pub use api::ApiServer;
pub use config::Config;
pub use error::{ContextdError, Result};
pub use models::*;

/// Version information
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// Get the version string
pub fn version() -> &'static str {
    VERSION
}
