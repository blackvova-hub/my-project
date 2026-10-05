CREATE TABLE analytics_connections (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id text NOT NULL,
 exchange text NOT NULL CHECK (exchange IN ('bybit','binance')),
 account_type text NOT NULL CHECK (account_type IN ('unified','futures','spot')),
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
 credentials bytea NOT NULL,
 fingerprint text NOT NULL,
 status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','syncing','ready','error')),
 error text NOT NULL DEFAULT '',
 warnings jsonb NOT NULL DEFAULT '[]',
 coverage_from timestamptz NOT NULL DEFAULT now() - interval '90 days',
 synced_through timestamptz,
 last_sync timestamptz,
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (user_id,exchange,account_type,fingerprint)
);
CREATE INDEX analytics_connections_owner ON analytics_connections(user_id,created_at);
CREATE INDEX analytics_connections_due ON analytics_connections(last_sync) WHERE status <> 'syncing';
CREATE TABLE analytics_events (
 connection_id uuid NOT NULL REFERENCES analytics_connections(id) ON DELETE CASCADE,
 source_id text NOT NULL,
 kind text NOT NULL,
 at timestamptz NOT NULL,
 symbol text NOT NULL DEFAULT '',
 currency text NOT NULL DEFAULT 'USDT',
 amount numeric(38,18) NOT NULL DEFAULT 0,
 normalized jsonb NOT NULL,
 raw jsonb NOT NULL,
 PRIMARY KEY (connection_id,source_id,kind)
);
CREATE INDEX analytics_events_timeline ON analytics_events(connection_id,at,kind);
CREATE TABLE analytics_snapshots (
 connection_id uuid NOT NULL REFERENCES analytics_connections(id) ON DELETE CASCADE,
 at timestamptz NOT NULL,
 equity numeric(38,18) NOT NULL,
 wallet numeric(38,18) NOT NULL,
 snapshot jsonb NOT NULL,
 PRIMARY KEY(connection_id,at)
);
CREATE TABLE analytics_trade_notes (
 user_id text NOT NULL,
 trade_id text NOT NULL,
 tag text NOT NULL DEFAULT '' CHECK(length(tag)<=80),
 strategy text NOT NULL DEFAULT '' CHECK(length(strategy)<=80),
 PRIMARY KEY(user_id,trade_id)
);
CREATE TABLE analytics_trade_metrics (
 connection_id uuid NOT NULL REFERENCES analytics_connections(id) ON DELETE CASCADE,
 trade_id text NOT NULL,
 mfe numeric,
 mae numeric,
 captured numeric,
 calculated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(connection_id,trade_id)
);
CREATE TABLE analytics_explanations (
 user_id text NOT NULL,
 evidence_hash text NOT NULL,
 explanation text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,evidence_hash)
);
CREATE INDEX analytics_explanations_daily ON analytics_explanations(user_id,created_at);
