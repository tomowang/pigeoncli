-- Records the oldest UID a folder has committed to keeping synced, once an
-- initial-sync window has been applied. 0 means "no horizon" — the folder
-- was fully synced (either windowing is disabled, or it predates this
-- column), so every message is expected to be cached. A nonzero horizon
-- tells syncFolder to leave remote UIDs below it uncached on every future
-- sync too, instead of re-treating them as "new" forever.
ALTER TABLE folders ADD COLUMN sync_horizon_uid INTEGER NOT NULL DEFAULT 0;
