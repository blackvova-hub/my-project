BEGIN;

ALTER TABLE alerts
  ADD COLUMN IF NOT EXISTS conditions JSONB;

UPDATE alerts
SET conditions = jsonb_build_array(
  jsonb_build_object(
    'indicator', indicator,
    'direction', direction,
    'threshold_percent', threshold_percent,
    'threshold_amount', threshold_amount
  )
)
WHERE conditions IS NULL;

COMMIT;
