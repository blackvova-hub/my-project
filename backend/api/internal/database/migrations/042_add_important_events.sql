BEGIN;

CREATE TABLE IF NOT EXISTS important_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_type TEXT NOT NULL,
  priority SMALLINT NOT NULL DEFAULT 2,
  title TEXT NOT NULL,
  details TEXT NOT NULL DEFAULT '',
  exchange TEXT,
  market_type TEXT,
  symbol TEXT,
  direction TEXT,
  amount_usd DOUBLE PRECISION,
  change_percent DOUBLE PRECISION,
  window_minutes INTEGER NOT NULL DEFAULT 1,
  event_at TIMESTAMPTZ NOT NULL,
  dedup_key TEXT NOT NULL UNIQUE,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT important_events_priority_chk CHECK (priority BETWEEN 1 AND 3),
  CONSTRAINT important_events_window_chk CHECK (window_minutes BETWEEN 1 AND 1440)
);

CREATE INDEX IF NOT EXISTS idx_important_events_recent
  ON important_events(event_at DESC, priority ASC);

COMMIT;
