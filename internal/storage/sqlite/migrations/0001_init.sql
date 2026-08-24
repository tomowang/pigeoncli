CREATE TABLE accounts (
    id INTEGER PRIMARY KEY,
    slug TEXT UNIQUE NOT NULL,
    email TEXT NOT NULL,
    display_name TEXT,
    last_synced_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE folders (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    name TEXT NOT NULL,
    delimiter TEXT,
    special_use TEXT,
    uidvalidity INTEGER NOT NULL DEFAULT 0,
    uidnext INTEGER NOT NULL DEFAULT 0,
    unread_count INTEGER NOT NULL DEFAULT 0,
    total_count INTEGER NOT NULL DEFAULT 0,
    last_synced_at TIMESTAMP,
    UNIQUE(account_id, path)
);

CREATE TABLE messages (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    uid INTEGER NOT NULL,
    message_id TEXT,
    in_reply_to TEXT,
    subject TEXT,
    from_name TEXT,
    from_addr TEXT,
    to_addrs TEXT NOT NULL DEFAULT '[]',
    cc_addrs TEXT NOT NULL DEFAULT '[]',
    date TIMESTAMP,
    flags TEXT NOT NULL DEFAULT '[]',
    seen INTEGER NOT NULL DEFAULT 0,
    size INTEGER,
    preview_text TEXT,
    body_synced INTEGER NOT NULL DEFAULT 0,
    raw_ref TEXT,
    synced_at TIMESTAMP,
    UNIQUE(folder_id, uid)
);
CREATE INDEX idx_messages_folder_date ON messages(folder_id, date DESC);

CREATE TABLE signatures (
    id INTEGER PRIMARY KEY,
    account_id INTEGER REFERENCES accounts(id) ON DELETE CASCADE,
    name TEXT,
    body_plain TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
