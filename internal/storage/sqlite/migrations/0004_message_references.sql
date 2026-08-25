-- Named references_ids, not references — REFERENCES is a reserved SQLite
-- keyword (foreign-key clause syntax) and using it unquoted as a column
-- name is a footgun worth avoiding outright.
ALTER TABLE messages ADD COLUMN references_ids TEXT NOT NULL DEFAULT '[]';

-- Supports resolving in_reply_to/references_ids message-IDs to cached rows
-- across folders within an account (e.g. a Sent copy vs. an Inbox reply).
CREATE INDEX idx_messages_account_message_id ON messages(account_id, message_id);
