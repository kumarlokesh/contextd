-- Migration 006: per-project retention overrides.
-- When no row exists for a project the daemon falls back to
-- policy.default_retention_days from the config.
CREATE TABLE IF NOT EXISTS project_retentions (
    project_id     TEXT    PRIMARY KEY,
    retention_days INTEGER NOT NULL CHECK (retention_days > 0),
    updated_at     INTEGER NOT NULL  -- Unix milliseconds
);
