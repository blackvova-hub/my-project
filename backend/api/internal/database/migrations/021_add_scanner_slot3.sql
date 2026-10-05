-- add SLOT_3 to scanner_slot check constraint (third scanner)
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'alerts_scanner_slot_check'
  ) THEN
    ALTER TABLE alerts DROP CONSTRAINT alerts_scanner_slot_check;
  END IF;

  ALTER TABLE alerts
    ADD CONSTRAINT alerts_scanner_slot_check
    CHECK (scanner_slot IN ('SLOT_1', 'SLOT_2', 'SLOT_3'));
END $$;
