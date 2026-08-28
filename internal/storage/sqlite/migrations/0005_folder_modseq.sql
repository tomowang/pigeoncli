-- Stores each folder's HIGHESTMODSEQ from the last sync (RFC 7162
-- CONDSTORE), so the next sync can ask the server for only what changed
-- since then instead of every message's UID+flags. 0 means "no CONDSTORE
-- baseline yet" — either the server doesn't support it or this folder
-- hasn't completed a CONDSTORE-eligible sync.
ALTER TABLE folders ADD COLUMN mod_seq INTEGER NOT NULL DEFAULT 0;
