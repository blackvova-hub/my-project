-- allow SLOT_3 in signals scanner slot check (used by worker_slot3)
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'signals_scanner_slot_check'
  ) THEN
    ALTER TABLE signals DROP CONSTRAINT signals_scanner_slot_check;
  END IF;

  ALTER TABLE signals
    ADD CONSTRAINT signals_scanner_slot_check
    CHECK (scanner_slot IN ('SLOT_1', 'SLOT_2', 'SLOT_3'));
END $$;
