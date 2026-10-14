BEGIN;

-- Terminal collection is deliberately separate from the moderated Telegram
-- publication flag. A terminal fetch must never enqueue a broadcast.
ALTER TABLE news_items
  ADD COLUMN IF NOT EXISTS terminal_collected BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_news_items_terminal_published
  ON news_items(published_at DESC)
  WHERE terminal_collected = TRUE;

CREATE TABLE IF NOT EXISTS news_terminal_source_health (
  source_id TEXT PRIMARY KEY,
  last_attempt TIMESTAMPTZ,
  last_success TIMESTAMPTZ,
  last_error TEXT,
  item_count INTEGER NOT NULL DEFAULT 0,
  etag TEXT NOT NULL DEFAULT '',
  last_modified TEXT NOT NULL DEFAULT '',
  next_attempt TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;
