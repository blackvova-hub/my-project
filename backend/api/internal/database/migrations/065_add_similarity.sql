CREATE TABLE IF NOT EXISTS similarity_assets (
 market text NOT NULL CHECK(market IN ('spot','linear')),
 symbol text NOT NULL,
 sectors text[] NOT NULL DEFAULT '{}',
 is_alt boolean NOT NULL DEFAULT true,
 is_stablecoin boolean NOT NULL DEFAULT false,
 enabled boolean NOT NULL DEFAULT true,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(market,symbol)
);
CREATE INDEX IF NOT EXISTS similarity_assets_sectors ON similarity_assets USING gin(sectors);

-- Small durable cursors, not one PostgreSQL row per vector.
CREATE TABLE IF NOT EXISTS similarity_streams (
 id bigserial PRIMARY KEY,
 version text NOT NULL,
 market text NOT NULL,
 symbol text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('features','outcomes')),
 span integer NOT NULL CHECK(span>=12),
 stride integer NOT NULL CHECK(stride>0),
 target_from bigint NOT NULL,
 target_to bigint NOT NULL,
 scanned_from bigint NOT NULL,
 scanned_to bigint NOT NULL,
 lease_token uuid,
 lease_until timestamptz NOT NULL DEFAULT '-infinity',
 retry_at timestamptz NOT NULL DEFAULT '-infinity',
 updated_at timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT '',
 UNIQUE(version,market,symbol,kind,span,stride)
);
CREATE INDEX IF NOT EXISTS similarity_streams_schedule ON similarity_streams(version,retry_at,updated_at);
CREATE TABLE IF NOT EXISTS similarity_repairs (
 stream_id bigint NOT NULL REFERENCES similarity_streams(id) ON DELETE CASCADE,
 range_from bigint NOT NULL,
 range_to bigint NOT NULL,
 retry_at timestamptz NOT NULL DEFAULT now()+interval '1 hour',
 PRIMARY KEY(stream_id,range_from,range_to)
);
CREATE INDEX IF NOT EXISTS similarity_repairs_due ON similarity_repairs(retry_at);
