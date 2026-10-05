BEGIN;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'trade_status') THEN
    CREATE TYPE trade_status AS ENUM ('OPEN', 'CLOSED');
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS signal_trades (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  signal_id UUID NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
  status trade_status NOT NULL DEFAULT 'OPEN',
  buy_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  sell_at TIMESTAMPTZ NULL,
  duration_minutes INT NULL CHECK (duration_minutes IS NULL OR duration_minutes >= 0),
  profit_percent NUMERIC(10,4) NULL,
  profit_usd NUMERIC(18,4) NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_signal_trades_open
  ON signal_trades (user_id, signal_id)
  WHERE status = 'OPEN';

CREATE INDEX IF NOT EXISTS idx_signal_trades_user_created
  ON signal_trades (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_signal_trades_signal
  ON signal_trades (signal_id);

CREATE INDEX IF NOT EXISTS idx_signal_trades_status
  ON signal_trades (user_id, status, created_at DESC);

CREATE OR REPLACE FUNCTION trg_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS signal_trades_set_updated_at ON signal_trades;
CREATE TRIGGER signal_trades_set_updated_at
BEFORE UPDATE ON signal_trades
FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

COMMIT;
