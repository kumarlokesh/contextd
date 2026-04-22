-- Migration 003: vector embeddings via sqlite-vec (vec0 virtual table).
-- float[384] matches all-minilm and other 384-dim embedding models.
-- Change the dimension here (and reset the database) when using larger models
-- (e.g. nomic-embed-text=768, text-embedding-3-small=1536).

CREATE VIRTUAL TABLE IF NOT EXISTS chats_vec USING vec0(
    embedding float[384]
);

-- Tracks embedding progress for each chat.
CREATE TABLE IF NOT EXISTS chats_embedding_status (
    chat_id    TEXT    NOT NULL PRIMARY KEY REFERENCES chats(id) ON DELETE CASCADE,
    project_id TEXT    NOT NULL,
    status     TEXT    NOT NULL CHECK(status IN ('pending', 'done', 'failed')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ces_status ON chats_embedding_status(status, created_at);

-- Auto-queue newly stored chats for embedding.
CREATE TRIGGER IF NOT EXISTS chats_vec_ai
AFTER INSERT ON chats BEGIN
    INSERT OR IGNORE INTO chats_embedding_status(chat_id, project_id, status, created_at, updated_at)
    VALUES (new.id, new.project_id, 'pending',
            (strftime('%s', 'now') * 1000),
            (strftime('%s', 'now') * 1000));
END;
