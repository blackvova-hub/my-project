BEGIN;
CREATE TABLE IF NOT EXISTS candle_archive_series (
 exchange text NOT NULL CHECK(exchange IN ('bybit','binance')),
 market text NOT NULL CHECK(market IN ('spot','linear')),
 symbol text NOT NULL,
 active boolean NOT NULL DEFAULT true,
 history_from bigint NOT NULL, history_to bigint NOT NULL,
 backfill_to bigint NOT NULL, live_to bigint NOT NULL,
 first_available bigint, last_available bigint,
 lease_token uuid, lease_until timestamptz NOT NULL DEFAULT '-infinity',
 retry_at timestamptz NOT NULL DEFAULT '-infinity',
 failures integer NOT NULL DEFAULT 0, last_error text NOT NULL DEFAULT '',
 written_rows bigint NOT NULL DEFAULT 0,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(exchange,market,symbol),
 CHECK(history_from<=backfill_to AND backfill_to<=history_to),
 CHECK(history_from%60000=0 AND history_to%60000=0 AND backfill_to%60000=0 AND live_to%60000=0)
);
CREATE INDEX IF NOT EXISTS candle_archive_schedule ON candle_archive_series(exchange,market,retry_at,lease_until,updated_at) WHERE active;
CREATE TABLE IF NOT EXISTS candle_archive_gaps (
 exchange text NOT NULL, market text NOT NULL, symbol text NOT NULL,
 range_from bigint NOT NULL, range_to bigint NOT NULL,
 missing_candles integer NOT NULL CHECK(missing_candles>0),
 retry_at timestamptz NOT NULL DEFAULT now()+interval '1 day',
 PRIMARY KEY(exchange,market,symbol,range_from,range_to),
 FOREIGN KEY(exchange,market,symbol) REFERENCES candle_archive_series ON DELETE CASCADE,
 CHECK(range_to>range_from)
);
CREATE INDEX IF NOT EXISTS candle_archive_gap_retry ON candle_archive_gaps(exchange,market,symbol,retry_at);
COMMIT;
