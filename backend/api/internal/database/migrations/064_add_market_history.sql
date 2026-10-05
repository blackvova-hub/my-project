BEGIN;

-- Progress only. The canonical OHLCV rows remain in shared ClickHouse.
CREATE TABLE IF NOT EXISTS market_history_series (
 market TEXT NOT NULL CHECK (market IN ('spot','linear')),
 symbol TEXT NOT NULL,
 timeframe TEXT NOT NULL DEFAULT '5m' CHECK (timeframe='5m'),
 active BOOLEAN NOT NULL DEFAULT true,
 history_from BIGINT NOT NULL,
 history_to BIGINT NOT NULL,
 backfill_to BIGINT NOT NULL,
 live_to BIGINT NOT NULL,
 launch_time BIGINT NOT NULL DEFAULT 0,
 first_available BIGINT,
 last_available BIGINT,
 lease_token UUID,
 lease_until TIMESTAMPTZ NOT NULL DEFAULT '-infinity',
 retry_at TIMESTAMPTZ NOT NULL DEFAULT '-infinity',
 failures INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 catalog_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (market,symbol,timeframe),
 CHECK (history_from <= backfill_to AND backfill_to <= history_to),
 CHECK (history_from % 300000=0 AND history_to % 300000=0 AND live_to % 300000=0 AND backfill_to % 300000=0)
);
CREATE INDEX IF NOT EXISTS market_history_schedule ON market_history_series(retry_at,lease_until,updated_at) WHERE active;

-- A missing source row is explicit, never replaced by a fabricated flat bar.
CREATE TABLE IF NOT EXISTS market_history_gaps (
 market TEXT NOT NULL,
 symbol TEXT NOT NULL,
 timeframe TEXT NOT NULL DEFAULT '5m',
 range_from BIGINT NOT NULL,
 range_to BIGINT NOT NULL,
 missing_candles INTEGER NOT NULL CHECK (missing_candles > 0),
 retry_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '1 day',
 checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (market,symbol,timeframe,range_from,range_to),
 FOREIGN KEY (market,symbol,timeframe) REFERENCES market_history_series(market,symbol,timeframe) ON DELETE CASCADE,
 CHECK (range_to>range_from)
);
CREATE INDEX IF NOT EXISTS market_history_gap_retry ON market_history_gaps(market,symbol,timeframe,retry_at);
COMMIT;
