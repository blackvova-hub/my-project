BEGIN;

ALTER TABLE alerts
  ADD COLUMN IF NOT EXISTS scanner_slot TEXT NOT NULL DEFAULT 'SLOT_1';

UPDATE alerts
SET scanner_slot = 'SLOT_1'
WHERE scanner_slot IS NULL OR scanner_slot = '';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'alerts_scanner_slot_check'
  ) THEN
    ALTER TABLE alerts
      ADD CONSTRAINT alerts_scanner_slot_check
      CHECK (scanner_slot IN ('SLOT_1', 'SLOT_2'));
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_alerts_user_slot
  ON alerts (user_id, scanner_slot);

COMMIT;
