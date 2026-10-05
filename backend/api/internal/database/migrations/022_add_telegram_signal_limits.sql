BEGIN;

CREATE TABLE IF NOT EXISTS telegram_signal_limits (
  user_id BIGINT NOT NULL,
  symbol TEXT NOT NULL,
  daily_limit INT NOT NULL CHECK (daily_limit >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, symbol)
);

CREATE INDEX IF NOT EXISTS idx_telegram_signal_limits_user
  ON telegram_signal_limits(user_id);

CREATE TABLE IF NOT EXISTS telegram_limit_requests (
  telegram_id BIGINT PRIMARY KEY,
  step TEXT NOT NULL,
  symbol TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;

