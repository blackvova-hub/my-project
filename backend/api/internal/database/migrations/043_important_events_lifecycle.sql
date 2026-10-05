BEGIN;

ALTER TABLE important_events
  ADD COLUMN IF NOT EXISTS family TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS identity_key TEXT,
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'resolved',
  ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS occurrence_count INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS confidence SMALLINT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS baseline_value DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS baseline_ratio DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS percentile DOUBLE PRECISION;

UPDATE important_events
SET family = CASE WHEN family = '' THEN event_type ELSE family END,
    identity_key = COALESCE(identity_key, dedup_key),
    first_seen_at = COALESCE(first_seen_at, event_at),
    last_seen_at = COALESCE(last_seen_at, event_at),
    resolved_at = COALESCE(resolved_at, event_at)
WHERE family = '' OR identity_key IS NULL OR first_seen_at IS NULL OR last_seen_at IS NULL;

ALTER TABLE important_events
  ALTER COLUMN identity_key SET NOT NULL,
  ALTER COLUMN first_seen_at SET NOT NULL,
  ALTER COLUMN last_seen_at SET NOT NULL;

ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_status_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_status_chk CHECK (status IN ('active', 'resolved'));
ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_confidence_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_confidence_chk CHECK (confidence BETWEEN 1 AND 3);

CREATE UNIQUE INDEX IF NOT EXISTS uq_important_events_active_identity
  ON important_events(identity_key) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_important_events_active_instrument
  ON important_events(exchange, market_type, symbol, family, last_seen_at DESC)
  WHERE status = 'active';

COMMIT;
