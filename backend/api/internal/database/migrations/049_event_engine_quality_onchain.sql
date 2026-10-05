BEGIN;

ALTER TABLE important_events
  ADD COLUMN IF NOT EXISTS observation_count INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS confirmation_started_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS confirmation_deadline TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS candidate_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS factor_count SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS confirmed_venues JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS venue_count SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS cluster_start_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS cluster_end_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS source_kind TEXT NOT NULL DEFAULT 'market',
  ADD COLUMN IF NOT EXISTS source_ref TEXT,
  ADD COLUMN IF NOT EXISTS catalyst_event_id UUID REFERENCES news_items(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS entity_id UUID;

UPDATE important_events
SET cluster_start_at = COALESCE(cluster_start_at, first_seen_at),
    cluster_end_at = COALESCE(cluster_end_at, last_seen_at),
    confirmation_started_at = CASE WHEN status IN ('candidate','confirmed') THEN COALESCE(confirmation_started_at, first_seen_at) ELSE confirmation_started_at END,
    confirmation_deadline = CASE WHEN status IN ('candidate','confirmed') THEN COALESCE(confirmation_deadline, first_seen_at + interval '5 minutes') ELSE confirmation_deadline END,
    source_kind = CASE WHEN source_kind = '' THEN 'market' ELSE source_kind END
WHERE cluster_start_at IS NULL OR cluster_end_at IS NULL OR source_kind = '';

ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_status_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_status_chk CHECK (status IN ('candidate','confirmed','active','resolved'));
ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_observation_count_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_observation_count_chk CHECK (observation_count > 0);
ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_venue_count_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_venue_count_chk CHECK (venue_count >= 0);

CREATE INDEX IF NOT EXISTS idx_important_events_lifecycle
  ON important_events(status, source_kind, last_observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_important_events_candidate_deadline
  ON important_events(confirmation_deadline)
  WHERE status IN ('candidate','confirmed');
CREATE INDEX IF NOT EXISTS idx_important_events_catalyst
  ON important_events(catalyst_event_id)
  WHERE catalyst_event_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS important_event_factors (
  id BIGSERIAL PRIMARY KEY,
  event_id UUID NOT NULL REFERENCES important_events(id) ON DELETE CASCADE,
  factor_type TEXT NOT NULL,
  value DOUBLE PRECISION,
  baseline_ratio DOUBLE PRECISION,
  percentile DOUBLE PRECISION,
  observed_at TIMESTAMPTZ NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_important_event_factors_event_time
  ON important_event_factors(event_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS wallet_entities (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  entity_name TEXT NOT NULL,
  entity_type TEXT NOT NULL DEFAULT 'unknown',
  status TEXT NOT NULL DEFAULT 'unknown',
  aliases JSONB NOT NULL DEFAULT '[]'::jsonb,
  notes TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT wallet_entities_status_chk CHECK (status IN ('verified','probable','unknown','ignore'))
);
CREATE INDEX IF NOT EXISTS idx_wallet_entities_name ON wallet_entities(lower(entity_name));

CREATE TABLE IF NOT EXISTS wallet_registry (
  chain TEXT NOT NULL,
  address TEXT NOT NULL,
  entity_id UUID REFERENCES wallet_entities(id) ON DELETE SET NULL,
  entity_name TEXT NOT NULL DEFAULT '',
  entity_type TEXT NOT NULL DEFAULT 'unknown',
  status TEXT NOT NULL DEFAULT 'unknown',
  label_source TEXT NOT NULL DEFAULT '',
  verification_source_url TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  aliases JSONB NOT NULL DEFAULT '[]'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  significant_tx_count INTEGER NOT NULL DEFAULT 0,
  largest_tx_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
  sample_tx_hashes JSONB NOT NULL DEFAULT '[]'::jsonb,
  counterparties JSONB NOT NULL DEFAULT '[]'::jsonb,
  suggested_entity_type TEXT NOT NULL DEFAULT '',
  known_balance_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
  daily_volume_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
  discovery_source TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (chain, address),
  CONSTRAINT wallet_registry_status_chk CHECK (status IN ('verified','probable','unknown','ignore'))
);
CREATE INDEX IF NOT EXISTS idx_wallet_registry_status_seen ON wallet_registry(status, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_wallet_registry_entity ON wallet_registry(entity_id);
CREATE INDEX IF NOT EXISTS idx_wallet_registry_largest ON wallet_registry(largest_tx_usd DESC);

CREATE TABLE IF NOT EXISTS wallet_label_history (
  id BIGSERIAL PRIMARY KEY,
  chain TEXT NOT NULL,
  address TEXT NOT NULL,
  entity_id UUID,
  old_entity_name TEXT NOT NULL DEFAULT '',
  new_entity_name TEXT NOT NULL DEFAULT '',
  old_status TEXT NOT NULL DEFAULT '',
  new_status TEXT NOT NULL DEFAULT '',
  changed_by TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL DEFAULT '',
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_wallet_label_history_address ON wallet_label_history(chain, address, changed_at DESC);

CREATE TABLE IF NOT EXISTS onchain_transfers (
  id BIGSERIAL PRIMARY KEY,
  chain TEXT NOT NULL,
  tx_hash TEXT NOT NULL,
  block_number BIGINT,
  block_time TIMESTAMPTZ,
  token_address TEXT NOT NULL DEFAULT '',
  symbol TEXT NOT NULL DEFAULT '',
  from_address TEXT NOT NULL,
  to_address TEXT NOT NULL,
  from_entity_id UUID,
  to_entity_id UUID,
  usd_value DOUBLE PRECISION NOT NULL DEFAULT 0,
  token_amount NUMERIC,
  liquidity_share DOUBLE PRECISION,
  balance_share DOUBLE PRECISION,
  transfer_kind TEXT NOT NULL DEFAULT 'unknown',
  is_internal BOOLEAN NOT NULL DEFAULT FALSE,
  significance DOUBLE PRECISION NOT NULL DEFAULT 0,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(chain, tx_hash, from_address, to_address, token_address)
);
CREATE INDEX IF NOT EXISTS idx_onchain_transfers_time ON onchain_transfers(block_time DESC);
CREATE INDEX IF NOT EXISTS idx_onchain_transfers_entities ON onchain_transfers(from_entity_id, to_entity_id, block_time DESC);
CREATE INDEX IF NOT EXISTS idx_onchain_transfers_symbol ON onchain_transfers(symbol, block_time DESC);

ALTER TABLE wallet_registry ADD COLUMN IF NOT EXISTS known_balance_usd DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE wallet_registry ADD COLUMN IF NOT EXISTS daily_volume_usd DOUBLE PRECISION NOT NULL DEFAULT 0;

COMMIT;