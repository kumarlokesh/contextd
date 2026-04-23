-- Migration 004: hash-chained audit log.
-- Every search, retrieval, delete, and export is recorded here.
-- Each entry includes the hash of the previous entry (prev_hash) so any
-- tampering invalidates the chain from that point forward.

CREATE TABLE IF NOT EXISTS audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp     INTEGER NOT NULL,
    project_id    TEXT    NOT NULL,
    action        TEXT    NOT NULL,            -- 'search', 'retrieve', 'delete', 'export'
    actor         TEXT    NOT NULL DEFAULT 'anonymous',
    query_hash    TEXT    NOT NULL DEFAULT '', -- SHA-256(query) for search actions
    result_hashes TEXT    NOT NULL DEFAULT '[]', -- JSON array of SHA-256(chat_id)
    metadata      TEXT    NOT NULL DEFAULT '{}', -- JSON, action-specific details
    prev_hash     TEXT    NOT NULL,            -- entry_hash of the immediately preceding row
    entry_hash    TEXT    NOT NULL             -- SHA-256(all fields || prev_hash)
);

CREATE INDEX IF NOT EXISTS idx_audit_project_timestamp ON audit_log(project_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
