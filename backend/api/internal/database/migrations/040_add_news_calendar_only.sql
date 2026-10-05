ALTER TABLE news_items
  ADD COLUMN IF NOT EXISTS calendar_only BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE news_items
SET calendar_only = TRUE
WHERE source IN (
  'bybit_listing',
  'bybit_delisting',
  'Bybit Announcements',
  'binance_listing',
  'binance_delisting'
);

CREATE INDEX IF NOT EXISTS idx_news_items_regular_category_published
  ON news_items(category, published_at DESC)
  WHERE calendar_only = FALSE;
