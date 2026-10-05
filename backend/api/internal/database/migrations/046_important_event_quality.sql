BEGIN;

ALTER TABLE important_events
  ADD COLUMN IF NOT EXISTS severity SMALLINT NOT NULL DEFAULT 55,
  ADD COLUMN IF NOT EXISTS peak_severity SMALLINT NOT NULL DEFAULT 55,
  ADD COLUMN IF NOT EXISTS last_observed_at TIMESTAMPTZ;

UPDATE important_events
SET severity = LEAST(100, GREATEST(0, 55 + (3 - priority) * 12 + (confidence - 1) * 4)),
    peak_severity = GREATEST(peak_severity, LEAST(100, GREATEST(0, 55 + (3 - priority) * 12 + (confidence - 1) * 4))),
    last_observed_at = COALESCE(last_observed_at, last_seen_at, event_at)
WHERE last_observed_at IS NULL OR severity = 55 OR peak_severity = 55;

ALTER TABLE important_events
  ALTER COLUMN last_observed_at SET NOT NULL;

ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_severity_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_severity_chk CHECK (severity BETWEEN 0 AND 100);
ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_peak_severity_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_peak_severity_chk CHECK (peak_severity BETWEEN 0 AND 100);

-- Older workers could create more than one active multi-factor row when the
-- detector composition changed. Preserve the newest row and its accumulated
-- lifecycle, then retire the redundant cards before normalizing the identity.
WITH ranked AS (
  SELECT id,
         row_number() OVER (
           PARTITION BY exchange, market_type, symbol, direction
           ORDER BY last_seen_at DESC, created_at DESC
         ) AS rn,
         min(first_seen_at) OVER (PARTITION BY exchange, market_type, symbol, direction) AS merged_first_seen,
         max(last_seen_at) OVER (PARTITION BY exchange, market_type, symbol, direction) AS merged_last_seen,
         max(last_observed_at) OVER (PARTITION BY exchange, market_type, symbol, direction) AS merged_last_observed,
         sum(occurrence_count) OVER (PARTITION BY exchange, market_type, symbol, direction) AS merged_occurrences,
         max(peak_severity) OVER (PARTITION BY exchange, market_type, symbol, direction) AS merged_peak
  FROM important_events
  WHERE family = 'multi_factor' AND status = 'active'
)
UPDATE important_events e
SET first_seen_at = ranked.merged_first_seen,
    last_seen_at = ranked.merged_last_seen,
    last_observed_at = ranked.merged_last_observed,
    occurrence_count = ranked.merged_occurrences,
    peak_severity = ranked.merged_peak
FROM ranked
WHERE e.id = ranked.id AND ranked.rn = 1;

WITH ranked AS (
  SELECT id,
         row_number() OVER (
           PARTITION BY exchange, market_type, symbol, direction
           ORDER BY last_seen_at DESC, created_at DESC
         ) AS rn
  FROM important_events
  WHERE family = 'multi_factor' AND status = 'active'
)
UPDATE important_events e
SET status = 'resolved', resolved_at = COALESCE(resolved_at, e.last_seen_at)
FROM ranked
WHERE e.id = ranked.id AND ranked.rn > 1;

UPDATE important_events
SET identity_key = concat_ws(':', 'multi_factor', exchange, market_type, symbol, direction)
WHERE family = 'multi_factor' AND status = 'active';

CREATE INDEX IF NOT EXISTS idx_important_events_feed
  ON important_events(status, family, peak_severity DESC, last_seen_at DESC);

COMMIT;
