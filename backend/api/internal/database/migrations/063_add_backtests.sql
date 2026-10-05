BEGIN;

CREATE TABLE IF NOT EXISTS backtest_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 request_key UUID NOT NULL,
 request JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','completed','failed','cancelled')),
 phase TEXT NOT NULL DEFAULT 'queued',
 progress INTEGER NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
 error TEXT NOT NULL DEFAULT '',
 result JSONB,
 attempts INTEGER NOT NULL DEFAULT 0,
 lease_token UUID,
 heartbeat_at TIMESTAMPTZ,
 enqueued_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 finished_at TIMESTAMPTZ,
 UNIQUE (user_id, request_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS backtest_one_active_user ON backtest_jobs(user_id) WHERE status IN ('queued','running');
CREATE INDEX IF NOT EXISTS backtest_user_history ON backtest_jobs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS backtest_queue_pending ON backtest_jobs(created_at) WHERE status='queued';
CREATE INDEX IF NOT EXISTS backtest_expired_lease ON backtest_jobs(heartbeat_at) WHERE status='running';

CREATE TABLE IF NOT EXISTS backtest_trades (
 job_id UUID NOT NULL REFERENCES backtest_jobs(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL,
 trade JSONB NOT NULL,
 PRIMARY KEY (job_id,ordinal)
);

-- Small metadata catalog; candle rows themselves live in shared ClickHouse.
CREATE TABLE IF NOT EXISTS backtest_candle_series (
 market TEXT NOT NULL CHECK (market IN ('spot','linear')),
 symbol TEXT NOT NULL,
 timeframe TEXT NOT NULL CHECK (timeframe IN ('5m','15m','1h','4h')),
 last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 refreshed_at TIMESTAMPTZ,
 PRIMARY KEY (market,symbol,timeframe)
);
CREATE INDEX IF NOT EXISTS backtest_series_refresh ON backtest_candle_series(last_used_at DESC,refreshed_at);
COMMIT;
