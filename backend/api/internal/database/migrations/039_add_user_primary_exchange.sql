BEGIN;

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS primary_exchange TEXT;

ALTER TABLE users
  DROP CONSTRAINT IF EXISTS users_primary_exchange_check;

ALTER TABLE users
  ADD CONSTRAINT users_primary_exchange_check
  CHECK (primary_exchange IS NULL OR primary_exchange IN ('bybit', 'binance'));

COMMIT;
