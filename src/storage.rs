use chrono::{DateTime, Utc};
use sqlx::{sqlite::SqlitePool, Pool, Row, Sqlite};
use uuid::Uuid;

use crate::config::StorageConfig;
use crate::error::{ContextdError, Result};
use crate::models::Transcript;

pub struct MemoryStore {
    pool: Pool<Sqlite>,
    compression_enabled: bool,
}

impl MemoryStore {
    pub async fn new(config: &StorageConfig) -> Result<Self> {
        let database_url = match config.storage_type.as_str() {
            "sqlite" => {
                let path = config.sqlite_path.as_ref().ok_or_else(|| {
                    ContextdError::Config(anyhow::anyhow!("SQLite path not configured"))
                })?;

                // Create data directory if it doesn't exist
                if let Some(parent) = std::path::Path::new(path).parent() {
                    std::fs::create_dir_all(parent)?;
                }

                format!("sqlite:{}", path)
            }
            "postgres" => config
                .postgres_url
                .as_ref()
                .ok_or_else(|| {
                    ContextdError::Config(anyhow::anyhow!("Postgres URL not configured"))
                })?
                .clone(),
            _ => {
                return Err(ContextdError::Config(anyhow::anyhow!(
                    "Invalid storage type"
                )))
            }
        };

        let pool = SqlitePool::connect(&database_url).await?;

        let store = Self {
            pool,
            compression_enabled: config.compression,
        };

        store.migrate().await?;
        Ok(store)
    }

    async fn migrate(&self) -> Result<()> {
        sqlx::query(
            r#"
            CREATE TABLE IF NOT EXISTS transcripts (
                id TEXT PRIMARY KEY,
                project_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                timestamp TEXT NOT NULL,
                messages TEXT NOT NULL,
                metadata TEXT NOT NULL,
                created_at TEXT NOT NULL,
                content_hash TEXT NOT NULL,
                compressed BOOLEAN NOT NULL DEFAULT FALSE
            );
            
            CREATE INDEX IF NOT EXISTS idx_transcripts_project_id ON transcripts(project_id);
            CREATE INDEX IF NOT EXISTS idx_transcripts_session_id ON transcripts(session_id);
            CREATE INDEX IF NOT EXISTS idx_transcripts_timestamp ON transcripts(timestamp);
            CREATE INDEX IF NOT EXISTS idx_transcripts_created_at ON transcripts(created_at);
            CREATE INDEX IF NOT EXISTS idx_transcripts_content_hash ON transcripts(content_hash);
            "#,
        )
        .execute(&self.pool)
        .await?;

        Ok(())
    }

    pub async fn store_transcript(&self, transcript: &Transcript) -> Result<Uuid> {
        let messages_json = serde_json::to_string(&transcript.messages)?;
        let metadata_json = serde_json::to_string(&transcript.metadata)?;

        let (messages_data, metadata_data, compressed) = if self.compression_enabled {
            (
                self.compress(&messages_json)?,
                self.compress(&metadata_json)?,
                true,
            )
        } else {
            (
                messages_json.into_bytes(),
                metadata_json.into_bytes(),
                false,
            )
        };

        sqlx::query(
            r#"
            INSERT INTO transcripts (
                id, project_id, session_id, timestamp, messages, metadata, 
                created_at, content_hash, compressed
            ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
            "#,
        )
        .bind(transcript.id.to_string())
        .bind(&transcript.project_id)
        .bind(&transcript.session_id)
        .bind(transcript.timestamp.to_rfc3339())
        .bind(messages_data)
        .bind(metadata_data)
        .bind(transcript.created_at.to_rfc3339())
        .bind(transcript.content_hash())
        .bind(compressed)
        .execute(&self.pool)
        .await?;

        Ok(transcript.id)
    }

    pub async fn get_transcript(&self, id: Uuid, project_id: &str) -> Result<Option<Transcript>> {
        let row = sqlx::query(
            r#"
            SELECT id, project_id, session_id, timestamp, messages, metadata, 
                   created_at, compressed
            FROM transcripts 
            WHERE id = ?1 AND project_id = ?2
            "#,
        )
        .bind(id.to_string())
        .bind(project_id)
        .fetch_optional(&self.pool)
        .await?;

        if let Some(row) = row {
            let messages_data: Vec<u8> = row.get("messages");
            let metadata_data: Vec<u8> = row.get("metadata");
            let compressed: bool = row.get("compressed");

            let messages_json = if compressed {
                String::from_utf8(self.decompress(&messages_data)?)?
            } else {
                String::from_utf8(messages_data)?
            };

            let metadata_json = if compressed {
                String::from_utf8(self.decompress(&metadata_data)?)?
            } else {
                String::from_utf8(metadata_data)?
            };

            let transcript = Transcript {
                id: Uuid::parse_str(&row.get::<String, _>("id"))?,
                project_id: row.get("project_id"),
                session_id: row.get("session_id"),
                timestamp: DateTime::parse_from_rfc3339(&row.get::<String, _>("timestamp"))?
                    .with_timezone(&Utc),
                messages: serde_json::from_str(&messages_json)?,
                metadata: serde_json::from_str(&metadata_json)?,
                created_at: DateTime::parse_from_rfc3339(&row.get::<String, _>("created_at"))?
                    .with_timezone(&Utc),
            };

            Ok(Some(transcript))
        } else {
            Ok(None)
        }
    }

    pub async fn get_transcripts_by_ids(
        &self,
        ids: &[Uuid],
        project_id: &str,
    ) -> Result<Vec<Transcript>> {
        if ids.is_empty() {
            return Ok(vec![]);
        }

        let id_strings: Vec<String> = ids.iter().map(|id| id.to_string()).collect();
        let placeholders = format!(
            "?{}",
            (1..=ids.len())
                .map(|i| format!(",?{}", i + 1))
                .collect::<String>()
        );

        let query = format!(
            r#"
            SELECT id, project_id, session_id, timestamp, messages, metadata, 
                   created_at, compressed
            FROM transcripts 
            WHERE project_id = ?1 AND id IN ({})
            ORDER BY timestamp DESC
            "#,
            placeholders
        );

        let mut query_builder = sqlx::query(&query).bind(project_id);
        for id_string in &id_strings {
            query_builder = query_builder.bind(id_string);
        }

        let rows = query_builder.fetch_all(&self.pool).await?;
        let mut transcripts = Vec::new();

        for row in rows {
            let messages_data: Vec<u8> = row.get("messages");
            let metadata_data: Vec<u8> = row.get("metadata");
            let compressed: bool = row.get("compressed");

            let messages_json = if compressed {
                String::from_utf8(self.decompress(&messages_data)?)?
            } else {
                String::from_utf8(messages_data)?
            };

            let metadata_json = if compressed {
                String::from_utf8(self.decompress(&metadata_data)?)?
            } else {
                String::from_utf8(metadata_data)?
            };

            let transcript = Transcript {
                id: Uuid::parse_str(&row.get::<String, _>("id"))?,
                project_id: row.get("project_id"),
                session_id: row.get("session_id"),
                timestamp: DateTime::parse_from_rfc3339(&row.get::<String, _>("timestamp"))?
                    .with_timezone(&Utc),
                messages: serde_json::from_str(&messages_json)?,
                metadata: serde_json::from_str(&metadata_json)?,
                created_at: DateTime::parse_from_rfc3339(&row.get::<String, _>("created_at"))?
                    .with_timezone(&Utc),
            };

            transcripts.push(transcript);
        }

        Ok(transcripts)
    }

    pub async fn get_recent_transcripts(
        &self,
        project_id: &str,
        session_id: Option<&str>,
        limit: usize,
    ) -> Result<Vec<Transcript>> {
        let query = if session_id.is_some() {
            r#"
            SELECT id, project_id, session_id, timestamp, messages, metadata, 
                   created_at, compressed
            FROM transcripts 
            WHERE project_id = ?1 AND session_id = ?2
            ORDER BY timestamp DESC 
            LIMIT ?3
            "#
        } else {
            r#"
            SELECT id, project_id, session_id, timestamp, messages, metadata, 
                   created_at, compressed
            FROM transcripts 
            WHERE project_id = ?1
            ORDER BY timestamp DESC 
            LIMIT ?2
            "#
        };

        let rows = if let Some(sess_id) = session_id {
            sqlx::query(query)
                .bind(project_id)
                .bind(sess_id)
                .bind(limit as i64)
                .fetch_all(&self.pool)
                .await?
        } else {
            sqlx::query(query)
                .bind(project_id)
                .bind(limit as i64)
                .fetch_all(&self.pool)
                .await?
        };

        let mut transcripts = Vec::new();
        for row in rows {
            let messages_data: Vec<u8> = row.get("messages");
            let metadata_data: Vec<u8> = row.get("metadata");
            let compressed: bool = row.get("compressed");

            let messages_json = if compressed {
                String::from_utf8(self.decompress(&messages_data)?)?
            } else {
                String::from_utf8(messages_data)?
            };

            let metadata_json = if compressed {
                String::from_utf8(self.decompress(&metadata_data)?)?
            } else {
                String::from_utf8(metadata_data)?
            };

            let transcript = Transcript {
                id: Uuid::parse_str(&row.get::<String, _>("id"))?,
                project_id: row.get("project_id"),
                session_id: row.get("session_id"),
                timestamp: DateTime::parse_from_rfc3339(&row.get::<String, _>("timestamp"))?
                    .with_timezone(&Utc),
                messages: serde_json::from_str(&messages_json)?,
                metadata: serde_json::from_str(&metadata_json)?,
                created_at: DateTime::parse_from_rfc3339(&row.get::<String, _>("created_at"))?
                    .with_timezone(&Utc),
            };

            transcripts.push(transcript);
        }

        Ok(transcripts)
    }

    fn compress(&self, data: &str) -> Result<Vec<u8>> {
        Ok(lz4_flex::compress_prepend_size(data.as_bytes()))
    }

    fn decompress(&self, data: &[u8]) -> Result<Vec<u8>> {
        lz4_flex::decompress_size_prepended(data)
            .map_err(|e| ContextdError::Internal(format!("Decompression failed: {}", e)))
    }
}
