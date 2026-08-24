-- The original signatures.account_id FK tied a signature's existence to
-- the accounts mirror row, which only exists once an account has been
-- synced. Signatures are a config-level concern like account settings
-- themselves, so re-key them by the account's config slug instead — no
-- sync required before adding one. The table was never populated by any
-- shipped code path, so dropping and recreating it loses no data.
DROP TABLE signatures;

CREATE TABLE signatures (
    id INTEGER PRIMARY KEY,
    account_slug TEXT, -- NULL = global default signature
    name TEXT,
    body_plain TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
