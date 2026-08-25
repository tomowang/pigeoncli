-- FTS5 index over message headers (subject/from/to/cc), not bodies —
-- bodies are fetched lazily and often aren't cached locally at all. An
-- external-content table avoids duplicating the header text in messages;
-- the triggers below keep it in sync with every future insert/delete/update
-- on messages, so no sync/core code needs to touch this index explicitly.
CREATE VIRTUAL TABLE messages_fts USING fts5(
    subject, from_name, from_addr, to_addrs, cc_addrs,
    content='messages', content_rowid='id'
);

INSERT INTO messages_fts(rowid, subject, from_name, from_addr, to_addrs, cc_addrs)
SELECT id, subject, from_name, from_addr, to_addrs, cc_addrs FROM messages;

CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, subject, from_name, from_addr, to_addrs, cc_addrs)
    VALUES (new.id, new.subject, new.from_name, new.from_addr, new.to_addrs, new.cc_addrs);
END;

CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, from_name, from_addr, to_addrs, cc_addrs)
    VALUES ('delete', old.id, old.subject, old.from_name, old.from_addr, old.to_addrs, old.cc_addrs);
END;

CREATE TRIGGER messages_fts_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, from_name, from_addr, to_addrs, cc_addrs)
    VALUES ('delete', old.id, old.subject, old.from_name, old.from_addr, old.to_addrs, old.cc_addrs);
    INSERT INTO messages_fts(rowid, subject, from_name, from_addr, to_addrs, cc_addrs)
    VALUES (new.id, new.subject, new.from_name, new.from_addr, new.to_addrs, new.cc_addrs);
END;
