BEGIN;

-- Event timestamps may be delayed by the allowed scanner freshness window.
-- Resolution must wait after the worker actually observed the sample, rather
-- than resolving a just-ingested candidate immediately because event_at is old.
ALTER TABLE important_events
  ADD COLUMN IF NOT EXISTS last_ingested_at TIMESTAMPTZ DEFAULT now();

UPDATE important_events
SET last_ingested_at = COALESCE(last_ingested_at, created_at, last_observed_at, event_at)
WHERE last_ingested_at IS NULL;

ALTER TABLE important_events
  ALTER COLUMN last_ingested_at SET DEFAULT now(),
  ALTER COLUMN last_ingested_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_important_events_resolution_clock
  ON important_events(status, source_kind, last_ingested_at, id)
  WHERE status IN ('candidate','confirmed') AND source_kind='market';

COMMIT;
