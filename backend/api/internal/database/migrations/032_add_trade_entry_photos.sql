BEGIN;

ALTER TABLE signal_trades
	ADD COLUMN IF NOT EXISTS entry_basis TEXT NULL;

ALTER TABLE signal_trades
	ADD COLUMN IF NOT EXISTS entry_photos TEXT[] NULL;

DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'chk_signal_trades_entry_photos'
	) THEN
		ALTER TABLE signal_trades
			ADD CONSTRAINT chk_signal_trades_entry_photos
			CHECK (entry_photos IS NULL OR array_length(entry_photos, 1) <= 3);
	END IF;
END $$;

COMMIT;
