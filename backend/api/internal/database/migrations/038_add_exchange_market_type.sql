BEGIN;

ALTER TABLE alerts
  ADD COLUMN IF NOT EXISTS exchange TEXT NOT NULL DEFAULT 'bybit',
  ADD COLUMN IF NOT EXISTS market_type TEXT NOT NULL DEFAULT 'perpetual';

ALTER TABLE signals
  ADD COLUMN IF NOT EXISTS exchange TEXT NOT NULL DEFAULT 'bybit',
  ADD COLUMN IF NOT EXISTS market_type TEXT NOT NULL DEFAULT 'perpetual';

UPDATE signals s
SET exchange = a.exchange,
    market_type = a.market_type
FROM alerts a
WHERE s.rule_id = a.id
  AND (s.exchange IS NULL OR s.exchange = '' OR s.market_type IS NULL OR s.market_type = '');

ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_exchange_check;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_market_type_check;
ALTER TABLE signals DROP CONSTRAINT IF EXISTS signals_exchange_check;
ALTER TABLE signals DROP CONSTRAINT IF EXISTS signals_market_type_check;

ALTER TABLE alerts
  ADD CONSTRAINT alerts_exchange_check CHECK (exchange IN ('bybit', 'binance')),
  ADD CONSTRAINT alerts_market_type_check CHECK (market_type IN ('spot', 'perpetual'));

ALTER TABLE signals
  ADD CONSTRAINT signals_exchange_check CHECK (exchange IN ('bybit', 'binance')),
  ADD CONSTRAINT signals_market_type_check CHECK (market_type IN ('spot', 'perpetual'));

DROP INDEX IF EXISTS uq_alerts_user_slot;
CREATE UNIQUE INDEX IF NOT EXISTS uq_alerts_user_slot_exchange_market
  ON alerts (user_id, scanner_slot, exchange, market_type);

DROP INDEX IF EXISTS uq_signals_rule_symbol_tf_ts;
CREATE UNIQUE INDEX IF NOT EXISTS uq_signals_rule_instrument_tf_ts
  ON signals (rule_id, exchange, market_type, symbol, tf, ts);

DROP INDEX IF EXISTS idx_alerts_enabled_symbol;
CREATE INDEX IF NOT EXISTS idx_alerts_enabled_instrument
  ON alerts (enabled, exchange, market_type, symbol);

CREATE INDEX IF NOT EXISTS idx_signals_user_instrument_created
  ON signals (user_id, exchange, market_type, created_at DESC);

COMMIT;
