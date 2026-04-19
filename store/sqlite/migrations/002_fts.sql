-- 002_fts.sql: SQLite FTS5 full-text index over chats.content_text
--
-- External-content table (content='chats') means the FTS index doesn't
-- duplicate the text — it stores only the search index and references
-- the chats table's rowid via content_rowid='rowid'.
-- Triggers keep the index in sync on INSERT / UPDATE / DELETE.

CREATE VIRTUAL TABLE chats_fts USING fts5(
    content_text,
    content     = 'chats',
    content_rowid = 'rowid',
    tokenize    = 'porter unicode61'
);

-- Backfill: index all chats that exist before this migration ran.
INSERT INTO chats_fts(rowid, content_text)
    SELECT rowid, content_text FROM chats;

-- After INSERT: add the new row to the FTS index.
CREATE TRIGGER chats_fts_ai AFTER INSERT ON chats BEGIN
    INSERT INTO chats_fts(rowid, content_text)
        VALUES (new.rowid, new.content_text);
END;

-- After DELETE: remove the row from the FTS index.
CREATE TRIGGER chats_fts_ad AFTER DELETE ON chats BEGIN
    INSERT INTO chats_fts(chats_fts, rowid, content_text)
        VALUES ('delete', old.rowid, old.content_text);
END;

-- After UPDATE: delete the old index entry, insert the new one.
CREATE TRIGGER chats_fts_au AFTER UPDATE ON chats BEGIN
    INSERT INTO chats_fts(chats_fts, rowid, content_text)
        VALUES ('delete', old.rowid, old.content_text);
    INSERT INTO chats_fts(rowid, content_text)
        VALUES (new.rowid, new.content_text);
END;
