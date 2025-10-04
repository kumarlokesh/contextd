use anyhow::Result;
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct Config {
    pub server: ServerConfig,
    pub storage: StorageConfig,
    pub search: SearchConfig,
    pub policy: PolicyConfig,
    pub audit: AuditConfig,
    pub ranking: RankingConfig,
    pub limits: LimitsConfig,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ServerConfig {
    pub host: String,
    pub port: u16,
    pub compression: bool,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct StorageConfig {
    #[serde(rename = "type")]
    pub storage_type: String,
    pub sqlite_path: Option<String>,
    pub postgres_url: Option<String>,
    pub compression: bool,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct SearchConfig {
    pub engine: String,
    pub index_path: String,
    pub vector_search: bool,
    pub vector_dimension: usize,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct PolicyConfig {
    pub default_retention_days: u32,
    pub max_results_per_query: usize,
    pub max_transcript_size: usize,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct AuditConfig {
    pub enabled: bool,
    pub retention_days: u32,
    pub log_path: String,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct RankingConfig {
    pub bm25_weight: f32,
    pub semantic_weight: f32,
    pub temporal_weight: f32,
    pub temporal_decay_factor: f32,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct LimitsConfig {
    pub rate_limit_per_minute: u32,
    pub max_connections: usize,
    pub request_timeout_seconds: u64,
}

impl Config {
    pub fn load<P: AsRef<Path>>(path: P) -> Result<Self> {
        let content = std::fs::read_to_string(path)?;
        let config: Config = toml::from_str(&content)?;

        // Validate configuration
        config.validate()?;

        Ok(config)
    }

    fn validate(&self) -> Result<()> {
        // Validate ranking weights sum to 1.0
        let weight_sum =
            self.ranking.bm25_weight + self.ranking.semantic_weight + self.ranking.temporal_weight;

        if (weight_sum - 1.0).abs() > 0.001 {
            return Err(anyhow::anyhow!(
                "Ranking weights must sum to 1.0, got {}",
                weight_sum
            ));
        }

        // Validate storage type
        match self.storage.storage_type.as_str() {
            "sqlite" => {
                if self.storage.sqlite_path.is_none() {
                    return Err(anyhow::anyhow!("sqlite_path required for SQLite storage"));
                }
            }
            "postgres" => {
                if self.storage.postgres_url.is_none() {
                    return Err(anyhow::anyhow!(
                        "postgres_url required for Postgres storage"
                    ));
                }
            }
            _ => {
                return Err(anyhow::anyhow!(
                    "Invalid storage type: {}. Must be 'sqlite' or 'postgres'",
                    self.storage.storage_type
                ));
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    #[test]
    fn test_default_config() {
        let config = Config::default();
        assert_eq!(config.server.host, "127.0.0.1");
        assert_eq!(config.server.port, 8080);
        assert_eq!(config.storage.storage_type, "sqlite");
        assert!(config.audit.enabled);
    }

    #[test]
    fn test_config_validation() {
        let mut config = Config::default();

        // Test invalid ranking weights
        config.ranking.bm25_weight = 0.5;
        config.ranking.semantic_weight = 0.5;
        config.ranking.temporal_weight = 0.5; // Sum > 1.0

        assert!(config.validate().is_err());

        // Fix weights
        config.ranking.temporal_weight = 0.0;
        assert!(config.validate().is_ok());
    }

    #[test]
    fn test_sqlite_config_validation() {
        let mut config = Config::default();
        config.storage.storage_type = "sqlite".to_string();
        config.storage.sqlite_path = None;

        assert!(config.validate().is_err());

        config.storage.sqlite_path = Some("/tmp/test.db".to_string());
        assert!(config.validate().is_ok());
    }

    #[test]
    fn test_postgres_config_validation() {
        let mut config = Config::default();
        config.storage.storage_type = "postgres".to_string();
        config.storage.postgres_url = None;

        assert!(config.validate().is_err());

        config.storage.postgres_url = Some("postgresql://user:pass@localhost/db".to_string());
        assert!(config.validate().is_ok());
    }

    #[test]
    fn test_load_config_from_toml() {
        let temp_dir = TempDir::new().unwrap();
        let config_path = temp_dir.path().join("test_config.toml");

        let toml_content = r#"
[server]
host = "0.0.0.0"
port = 9090

[storage]
type = "sqlite"
sqlite_path = "/custom/path.db"

[audit]
enabled = false
"#;

        std::fs::write(&config_path, toml_content).unwrap();

        let config = Config::load(&config_path).unwrap();
        assert_eq!(config.server.host, "0.0.0.0");
        assert_eq!(config.server.port, 9090);
        assert_eq!(config.storage.sqlite_path.unwrap(), "/custom/path.db");
        assert!(!config.audit.enabled);
    }
}

impl Default for Config {
    fn default() -> Self {
        Self {
            server: ServerConfig {
                host: "127.0.0.1".to_string(),
                port: 8080,
                compression: true,
            },
            storage: StorageConfig {
                storage_type: "sqlite".to_string(),
                sqlite_path: Some("./data/contextd.db".to_string()),
                postgres_url: None,
                compression: true,
            },
            search: SearchConfig {
                engine: "tantivy".to_string(),
                index_path: "./data/index".to_string(),
                vector_search: false,
                vector_dimension: 384,
            },
            policy: PolicyConfig {
                default_retention_days: 90,
                max_results_per_query: 100,
                max_transcript_size: 1048576, // 1MB
            },
            audit: AuditConfig {
                enabled: true,
                retention_days: 365,
                log_path: "./data/audit.log".to_string(),
            },
            ranking: RankingConfig {
                bm25_weight: 0.6,
                semantic_weight: 0.3,
                temporal_weight: 0.1,
                temporal_decay_factor: 0.1,
            },
            limits: LimitsConfig {
                rate_limit_per_minute: 100,
                max_connections: 1000,
                request_timeout_seconds: 30,
            },
        }
    }
}
