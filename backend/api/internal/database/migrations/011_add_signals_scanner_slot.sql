BEGIN;

ALTER TABLE signals
  ADD COLUMN IF NOT EXISTS scanner_slot TEXT NOT NULL DEFAULT 'SLOT_1';

UPDATE signals s
SET scanner_slot = a.scanner_slot
FROM alerts a
WHERE s.rule_id = a.id
  AND (s.scanner_slot IS NULL OR s.scanner_slot = '' OR s.scanner_slot = 'SLOT_1');

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'signals_scanner_slot_check'
  ) THEN
    ALTER TABLE signals
      ADD CONSTRAINT signals_scanner_slot_check
      CHECK (scanner_slot IN ('SLOT_1', 'SLOT_2'));
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_signals_user_slot_created
  ON signals (user_id, scanner_slot, created_at DESC);

COMMIT;