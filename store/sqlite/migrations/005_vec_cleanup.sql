-- Migration 005: add delete trigger for chats_vec so embeddings are removed
-- whenever a chat is deleted. Without this, orphaned vec0 rows accumulate when
-- chats are deleted individually or via retention sweeps.
CREATE TRIGGER IF NOT EXISTS chats_vec_ad
AFTER DELETE ON chats BEGIN
    DELETE FROM chats_vec WHERE rowid = old.rowid;
END;
