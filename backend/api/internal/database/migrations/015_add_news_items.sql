CREATE TABLE IF NOT EXISTS news_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  category TEXT NOT NULL,
  title TEXT NOT NULL,
  url TEXT NOT NULL UNIQUE,
  source TEXT NOT NULL,
  published_at TIMESTAMPTZ NOT NULL,
  summary TEXT NULL,
  image TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_news_items_category_published
  ON news_items(category, published_at DESC);

CREATE INDEX IF NOT EXISTS idx_news_items_published
  ON news_items(published_at DESC);
