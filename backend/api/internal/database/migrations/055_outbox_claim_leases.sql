BEGIN;

-- Claims keep PostgreSQL transactions short: workers reserve one row atomically,
-- commit, and only then call Redis or Telegram. NULL means the row is unclaimed.
ALTER TABLE signal_delivery_outbox
  ADD COLUMN IF NOT EXISTS claim_token TEXT,
  ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;

ALTER TABLE important_event_outbox
  ADD COLUMN IF NOT EXISTS claim_token TEXT,
  ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;

COMMIT;
