-- 001_initial.sql: core schema for contextd
-- Timestamps are stored as Unix epoch milliseconds (INTEGER).
-- JSON blobs are stored as TEXT.

CREATE TABLE projects (
    id         TEXT    PRIMARY KEY,
    created_at INTEGER NOT NULL,
    metadata   TEXT                -- JSON object, nullable
);

CREATE TABLE sessions (
    id         TEXT    NOT NULL,
    project_id TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, id)
);

-- chats is the central append-only transcript table.
-- content_text is the concatenation of all message content, separated by " | ".
-- FTS5 (migration 002) will index this column via an external-content table.
CREATE TABLE chats (
    id           TEXT    PRIMARY KEY,
    project_id   TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id   TEXT    NOT NULL,
    timestamp    INTEGER NOT NULL,
    messages     TEXT    NOT NULL,  -- JSON array of {role, content} objects
    metadata     TEXT,              -- JSON object, nullable
    content_text TEXT    NOT NULL,  -- flattened message text for FTS
    FOREIGN KEY (project_id, session_id) REFERENCES sessions(project_id, id)
);

CREATE INDEX idx_chats_project_timestamp ON chats(project_id, timestamp DESC);
CREATE INDEX idx_chats_project_session ON chats(project_id, session_id, timestamp DESC);
