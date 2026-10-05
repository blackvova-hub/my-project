BEGIN;

CREATE TABLE IF NOT EXISTS trade_meta (
  user_id BIGINT PRIMARY KEY REFERENCES users(num_id) ON DELETE CASCADE,
  timeframe TEXT NULL,
  exchange TEXT NULL,
  strategy_name TEXT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TRIGGER IF EXISTS trade_meta_set_updated_at ON trade_meta;
CREATE TRIGGER trade_meta_set_updated_at
BEFORE UPDATE ON trade_meta
FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

COMMIT;
