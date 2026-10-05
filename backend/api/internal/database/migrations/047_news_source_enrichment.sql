BEGIN;
ALTER TABLE news_items
  ADD COLUMN IF NOT EXISTS original_title TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS original_text TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS original_language TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS translated_title TEXT,
  ADD COLUMN IF NOT EXISTS translated_summary TEXT,
  ADD COLUMN IF NOT EXISTS source_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS source_type TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS event_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS event_type TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS importance SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS entities JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS assets JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS event_fingerprint TEXT,
  ADD COLUMN IF NOT EXISTS event_group_id UUID;

CREATE TABLE IF NOT EXISTS news_event_groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_fingerprint TEXT NOT NULL UNIQUE,
  event_type TEXT NOT NULL DEFAULT '',
  event_at TIMESTAMPTZ,
  importance SMALLINT NOT NULL DEFAULT 0,
  entities JSONB NOT NULL DEFAULT '[]'::jsonb,
  assets JSONB NOT NULL DEFAULT '[]'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
UPDATE news_items SET original_title=title WHERE original_title='';
UPDATE news_items SET original_text=COALESCE(summary,'') WHERE original_text='';

CREATE INDEX IF NOT EXISTS idx_news_items_source_published ON news_items(source_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_news_items_event_fingerprint ON news_items(event_fingerprint) WHERE event_fingerprint IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_news_event_groups_last_seen ON news_event_groups(last_seen_at DESC);
COMMIT;
