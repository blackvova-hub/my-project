BEGIN;

ALTER TABLE news_items
  ADD COLUMN IF NOT EXISTS url_hash TEXT,
  ADD COLUMN IF NOT EXISTS title_norm TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_news_items_url_hash
  ON news_items(url_hash)
  WHERE url_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_news_items_title_norm_recent
  ON news_items(published_at DESC, title_norm);

COMMIT;
