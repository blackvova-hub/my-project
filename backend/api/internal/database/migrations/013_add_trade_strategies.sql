BEGIN;

CREATE TABLE IF NOT EXISTS trade_strategies (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  name_norm TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_trade_strategies_user_name
  ON trade_strategies (user_id, name_norm);

CREATE INDEX IF NOT EXISTS idx_trade_strategies_user_created
  ON trade_strategies (user_id, created_at DESC);

ALTER TABLE signal_trades
  ADD COLUMN IF NOT EXISTS strategy_id UUID NULL REFERENCES trade_strategies(id) ON DELETE SET NULL;

ALTER TABLE signal_trades
  ADD COLUMN IF NOT EXISTS exchange TEXT NULL;

ALTER TABLE signal_trades
  ADD COLUMN IF NOT EXISTS timeframe TEXT NULL;

ALTER TABLE signal_trades
  ADD COLUMN IF NOT EXISTS comment TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_signal_trades_strategy
  ON signal_trades (user_id, strategy_id);

DROP TRIGGER IF EXISTS trade_strategies_set_updated_at ON trade_strategies;
CREATE TRIGGER trade_strategies_set_updated_at
BEFORE UPDATE ON trade_strategies
FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

COMMIT;
