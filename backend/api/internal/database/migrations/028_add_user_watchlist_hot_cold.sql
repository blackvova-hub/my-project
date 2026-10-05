ALTER TABLE users
ADD COLUMN IF NOT EXISTS watchlist_hot jsonb NOT NULL DEFAULT '["BTCUSDT","ETHUSDT"]'::jsonb;

ALTER TABLE users
ADD COLUMN IF NOT EXISTS watchlist_cold jsonb NOT NULL DEFAULT '[]'::jsonb;

UPDATE users
SET watchlist_hot = watchlist
WHERE (watchlist_hot IS NULL OR watchlist_hot = '[]'::jsonb)
  AND watchlist IS NOT NULL;
