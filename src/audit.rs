use chrono::{DateTime, Utc};
use serde_json;
use std::fs::OpenOptions;
use std::io::{BufWriter, Write};
use std::path::Path;
use uuid::Uuid;

use crate::config::AuditConfig;
use crate::error::{ContextdError, Result};
use crate::models::AuditLogEntry;

pub struct AuditLogger {
    config: AuditConfig,
}

impl AuditLogger {
    pub fn new(config: AuditConfig) -> Result<Self> {
        // Create audit log directory if it doesn't exist
        if let Some(parent) = Path::new(&config.log_path).parent() {
            std::fs::create_dir_all(parent)?;
        }

        Ok(Self { config })
    }

    pub async fn log_search(
        &self,
        project_id: &str,
        query: &str,
        result_count: usize,
        result_hashes: Vec<String>,
        execution_time_ms: u64,
    ) -> Result<()> {
        if !self.config.enabled {
            return Ok(());
        }

        let entry = AuditLogEntry {
            id: Uuid::new_v4(),
            timestamp: Utc::now(),
            project_id: project_id.to_string(),
            operation: "search".to_string(),
            query: Some(query.to_string()),
            result_count,
            result_hashes,
            execution_time_ms,
        };

        self.write_entry(&entry).await
    }

    pub async fn log_recent_chats(
        &self,
        project_id: &str,
        result_count: usize,
        result_hashes: Vec<String>,
        execution_time_ms: u64,
    ) -> Result<()> {
        if !self.config.enabled {
            return Ok(());
        }

        let entry = AuditLogEntry {
            id: Uuid::new_v4(),
            timestamp: Utc::now(),
            project_id: project_id.to_string(),
            operation: "recent".to_string(),
            query: None,
            result_count,
            result_hashes,
            execution_time_ms,
        };

        self.write_entry(&entry).await
    }

    pub async fn log_store(
        &self,
        project_id: &str,
        transcript_hash: &str,
        execution_time_ms: u64,
    ) -> Result<()> {
        if !self.config.enabled {
            return Ok(());
        }

        let entry = AuditLogEntry {
            id: Uuid::new_v4(),
            timestamp: Utc::now(),
            project_id: project_id.to_string(),
            operation: "store".to_string(),
            query: None,
            result_count: 1,
            result_hashes: vec![transcript_hash.to_string()],
            execution_time_ms,
        };

        self.write_entry(&entry).await
    }

    async fn write_entry(&self, entry: &AuditLogEntry) -> Result<()> {
        let json_line = serde_json::to_string(entry)?;

        // Write to file in a thread-safe manner
        tokio::task::spawn_blocking({
            let log_path = self.config.log_path.clone();
            let json_line = json_line.clone();

            move || -> Result<()> {
                let file = OpenOptions::new()
                    .create(true)
                    .append(true)
                    .open(&log_path)?;

                let mut writer = BufWriter::new(file);
                writeln!(writer, "{}", json_line)?;
                writer.flush()?;

                Ok(())
            }
        })
        .await
        .map_err(|e| ContextdError::Internal(format!("Failed to write audit log: {}", e)))??;

        Ok(())
    }

    pub async fn get_logs(
        &self,
        project_id: &str,
        limit: usize,
        start_time: Option<DateTime<Utc>>,
        end_time: Option<DateTime<Utc>>,
    ) -> Result<Vec<AuditLogEntry>> {
        if !self.config.enabled {
            return Ok(vec![]);
        }

        let content = tokio::fs::read_to_string(&self.config.log_path).await?;

        let mut entries = Vec::new();
        for line in content.lines().rev() {
            // Read in reverse order (most recent first)
            if entries.len() >= limit {
                break;
            }

            if let Ok(entry) = serde_json::from_str::<AuditLogEntry>(line) {
                // Filter by project_id
                if entry.project_id != project_id {
                    continue;
                }

                // Filter by time range
                if let Some(start) = start_time {
                    if entry.timestamp < start {
                        continue;
                    }
                }

                if let Some(end) = end_time {
                    if entry.timestamp > end {
                        continue;
                    }
                }

                entries.push(entry);
            }
        }

        Ok(entries)
    }

    pub async fn cleanup_old_logs(&self) -> Result<()> {
        if !self.config.enabled {
            return Ok(());
        }

        // This is a simplified implementation
        // In production, we'd want to rotate logs properly
        let cutoff = Utc::now() - chrono::Duration::days(self.config.retention_days as i64);

        let content = tokio::fs::read_to_string(&self.config.log_path).await?;
        let mut valid_lines = Vec::new();

        for line in content.lines() {
            if let Ok(entry) = serde_json::from_str::<AuditLogEntry>(line) {
                if entry.timestamp >= cutoff {
                    valid_lines.push(line);
                }
            }
        }

        if valid_lines.len() * 2 < content.lines().count() {
            // If we're removing more than 50% of entries, rewrite the file
            let new_content = valid_lines.join("\n");
            if !new_content.is_empty() {
                tokio::fs::write(&self.config.log_path, new_content + "\n").await?;
            } else {
                // If no valid entries remain, truncate the file
                tokio::fs::write(&self.config.log_path, "").await?;
            }
        }

        Ok(())
    }

    pub async fn get_stats(&self, project_id: &str) -> Result<AuditStats> {
        if !self.config.enabled {
            return Ok(AuditStats::default());
        }

        let content = tokio::fs::read_to_string(&self.config.log_path).await?;

        let mut total_operations = 0;
        let mut searches = 0;
        let mut stores = 0;
        let mut recent_queries = 0;

        for line in content.lines() {
            if let Ok(entry) = serde_json::from_str::<AuditLogEntry>(line) {
                if entry.project_id == project_id {
                    total_operations += 1;
                    match entry.operation.as_str() {
                        "search" => searches += 1,
                        "store" => stores += 1,
                        "recent" => recent_queries += 1,
                        _ => {}
                    }
                }
            }
        }

        Ok(AuditStats {
            total_operations,
            searches,
            stores,
            recent_queries,
        })
    }
}

#[derive(Debug, Clone, Default)]
pub struct AuditStats {
    pub total_operations: usize,
    pub searches: usize,
    pub stores: usize,
    pub recent_queries: usize,
}
