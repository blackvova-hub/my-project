BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'alert_direction') THEN
    CREATE TYPE alert_direction AS ENUM ('up','down','both');
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'trade_status') THEN
    CREATE TYPE trade_status AS ENUM ('OPEN','CLOSED');
  END IF;
END $$;

CREATE OR REPLACE FUNCTION trg_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION trg_ai_usage_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION effective_plan(
  raw_plan TEXT,
  subscription_expires_at TIMESTAMPTZ,
  subscription_frozen_at TIMESTAMPTZ
)
RETURNS TEXT
LANGUAGE plpgsql
AS $$
DECLARE
  p TEXT;
BEGIN
  p := lower(trim(COALESCE(raw_plan, 'free')));
  IF p = 'standart' THEN
    p := 'standard';
  END IF;

  IF p NOT IN ('free', 'standard', 'pro') THEN
    p := 'free';
  END IF;

  IF p IN ('standard', 'pro') THEN
    IF subscription_frozen_at IS NOT NULL THEN
      RETURN 'free';
    END IF;
    IF subscription_expires_at IS NULL OR subscription_expires_at <= now() THEN
      RETURN 'free';
    END IF;
    RETURN p;
  END IF;

  RETURN 'free';
END;
$$;

CREATE OR REPLACE FUNCTION trg_normalize_user_subscription()
RETURNS TRIGGER AS $$
DECLARE
  p TEXT;
BEGIN
  p := lower(trim(COALESCE(NEW.plan, 'free')));
  IF p = 'standart' THEN
    p := 'standard';
  END IF;
  IF p NOT IN ('free', 'standard', 'pro') THEN
    p := 'free';
  END IF;

  NEW.plan := p;
  NEW.subscription_frozen_days_remaining := GREATEST(COALESCE(NEW.subscription_frozen_days_remaining, 0), 0);

  IF NEW.plan = 'free' THEN
    NEW.subscription_expires_at := NULL;
    NEW.subscription_frozen_at := NULL;
    NEW.subscription_frozen_days_remaining := 0;
    RETURN NEW;
  END IF;

  IF NEW.subscription_frozen_at IS NOT NULL THEN
    NEW.subscription_expires_at := NULL;
    IF NEW.subscription_frozen_days_remaining <= 0 THEN
      NEW.subscription_frozen_at := NULL;
      NEW.subscription_frozen_days_remaining := 0;
      NEW.plan := 'free';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.subscription_expires_at IS NULL OR NEW.subscription_expires_at <= now() THEN
    NEW.plan := 'free';
    NEW.subscription_expires_at := NULL;
    NEW.subscription_frozen_at := NULL;
    NEW.subscription_frozen_days_remaining := 0;
    RETURN NEW;
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  num_id BIGSERIAL NOT NULL UNIQUE,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  display_name TEXT,
  plan TEXT NOT NULL DEFAULT 'free',
  subscription_expires_at TIMESTAMPTZ,
  subscription_frozen_at TIMESTAMPTZ,
  subscription_frozen_days_remaining INTEGER NOT NULL DEFAULT 0,
  two_fa_enabled BOOLEAN NOT NULL DEFAULT false,
  avatar_url TEXT,
	primary_exchange TEXT CHECK (primary_exchange IN ('bybit', 'binance')),
  last_seen_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  email_verified BOOLEAN NOT NULL DEFAULT false,
  email_verify_hash TEXT,
  email_verify_expires_at TIMESTAMPTZ,
  longkoin_balance BIGINT NOT NULL DEFAULT 0,
  is_admin BOOLEAN NOT NULL DEFAULT false,
  telegram_id BIGINT,
  telegram_username TEXT,
  telegram_enabled BOOLEAN NOT NULL DEFAULT false,
  telegram_linked_at TIMESTAMPTZ,
  public_id INTEGER UNIQUE,
  last_ip TEXT,
  watchlist_hot JSONB NOT NULL DEFAULT '[]'::jsonb,
  watchlist_cold JSONB NOT NULL DEFAULT '[]'::jsonb,
  CONSTRAINT users_plan_valid CHECK (lower(trim(plan)) IN ('free', 'standard', 'pro')),
  CONSTRAINT users_subscription_frozen_days_nonnegative CHECK (subscription_frozen_days_remaining >= 0)
);

DROP TRIGGER IF EXISTS users_normalize_subscription ON users;
CREATE TRIGGER users_normalize_subscription BEFORE INSERT OR UPDATE ON users FOR EACH ROW EXECUTE FUNCTION trg_normalize_user_subscription();

CREATE TABLE IF NOT EXISTS sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  user_agent TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(user_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS alerts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  exchange TEXT NOT NULL DEFAULT 'bybit' CHECK (exchange IN ('bybit', 'binance')),
  market_type TEXT NOT NULL DEFAULT 'perpetual' CHECK (market_type IN ('spot', 'perpetual')),
  symbol TEXT NOT NULL,
  window_minutes INT NOT NULL,
  threshold_percent DOUBLE PRECISION,
  direction alert_direction NOT NULL DEFAULT 'both',
  cooldown_seconds INT NOT NULL DEFAULT 60 CHECK (cooldown_seconds >= 0),
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  indicator TEXT NOT NULL DEFAULT 'price',
  threshold_amount DOUBLE PRECISION,
  conditions JSONB,
  scanner_slot TEXT NOT NULL DEFAULT 'SLOT_1',
  CONSTRAINT alerts_window_minutes_24h_check CHECK (window_minutes BETWEEN 1 AND 1440),
  CONSTRAINT alerts_threshold_percent_check CHECK (threshold_percent IS NULL OR threshold_percent > 0),
  CONSTRAINT alerts_threshold_amount_check CHECK (threshold_amount IS NULL OR threshold_amount > 0)
);
CREATE INDEX IF NOT EXISTS idx_alerts_enabled_instrument ON alerts(enabled, exchange, market_type, symbol);
CREATE INDEX IF NOT EXISTS idx_alerts_updated_at ON alerts(updated_at);
CREATE INDEX IF NOT EXISTS idx_alerts_user ON alerts(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_alerts_user_slot_exchange_market ON alerts(user_id, scanner_slot, exchange, market_type);
DROP TRIGGER IF EXISTS alerts_set_updated_at ON alerts;
CREATE TRIGGER alerts_set_updated_at BEFORE UPDATE ON alerts FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS signals (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  exchange TEXT NOT NULL DEFAULT 'bybit' CHECK (exchange IN ('bybit', 'binance')),
  market_type TEXT NOT NULL DEFAULT 'perpetual' CHECK (market_type IN ('spot', 'perpetual')),
  symbol TEXT NOT NULL,
  tf TEXT NOT NULL DEFAULT '1m',
  ts BIGINT NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  scanner_slot TEXT NOT NULL DEFAULT 'SLOT_1'
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_signals_rule_instrument_tf_ts ON signals(rule_id, exchange, market_type, symbol, tf, ts);
CREATE INDEX IF NOT EXISTS idx_signals_user_created ON signals(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_signals_user_instrument_created ON signals(user_id, exchange, market_type, created_at DESC);

CREATE TABLE IF NOT EXISTS signal_delivery_outbox (
  id BIGSERIAL PRIMARY KEY,
  signal_id UUID NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  stream_published_at TIMESTAMPTZ,
  telegram_processed_at TIMESTAMPTZ,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  claim_token TEXT,
  claim_expires_at TIMESTAMPTZ,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  dead_lettered_at TIMESTAMPTZ,
  error_class TEXT,
  stream_attempts INTEGER NOT NULL DEFAULT 0,
  telegram_attempts INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT signal_delivery_outbox_step_attempts_nonnegative CHECK (stream_attempts >= 0 AND telegram_attempts >= 0),
  CONSTRAINT signal_delivery_outbox_error_class_check CHECK (error_class IS NULL OR error_class IN ('transient', 'permanent', 'transient_exhausted')),
  CONSTRAINT signal_delivery_outbox_dead_letter_check CHECK (dead_lettered_at IS NULL OR (error_class IS NOT NULL AND error_class IN ('permanent', 'transient_exhausted'))),
  CONSTRAINT uq_signal_delivery_outbox_signal UNIQUE (signal_id)
);
CREATE INDEX IF NOT EXISTS idx_signal_delivery_outbox_pending
  ON signal_delivery_outbox(id)
  WHERE stream_published_at IS NULL OR telegram_processed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_signal_delivery_outbox_ready
  ON signal_delivery_outbox(next_attempt_at, id)
  WHERE dead_lettered_at IS NULL AND (stream_published_at IS NULL OR telegram_processed_at IS NULL);

CREATE TABLE IF NOT EXISTS two_fa_codes (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose TEXT NOT NULL,
  code_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, purpose)
);

CREATE TABLE IF NOT EXISTS signal_trades (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  signal_id UUID NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
  status trade_status NOT NULL DEFAULT 'OPEN',
  buy_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  sell_at TIMESTAMPTZ,
  duration_minutes INTEGER,
  profit_percent NUMERIC(10,4),
  profit_usd NUMERIC(18,4),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  exchange TEXT,
  timeframe TEXT,
  comment TEXT,
  strategy_id UUID,
  entry_basis TEXT,
  entry_photos TEXT[]
);
DROP TRIGGER IF EXISTS signal_trades_set_updated_at ON signal_trades;
CREATE TRIGGER signal_trades_set_updated_at BEFORE UPDATE ON signal_trades FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

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
DROP TRIGGER IF EXISTS trade_strategies_set_updated_at ON trade_strategies;
CREATE TRIGGER trade_strategies_set_updated_at BEFORE UPDATE ON trade_strategies FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS trade_meta (
  user_id BIGINT PRIMARY KEY REFERENCES users(num_id) ON DELETE CASCADE,
  exchange TEXT,
  timeframe TEXT,
  strategy_name TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DROP TRIGGER IF EXISTS trade_meta_set_updated_at ON trade_meta;
CREATE TRIGGER trade_meta_set_updated_at BEFORE UPDATE ON trade_meta FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS news_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title TEXT NOT NULL,
  summary TEXT,
  url TEXT NOT NULL UNIQUE,
  image TEXT,
  source TEXT NOT NULL,
  category TEXT NOT NULL,
  published_at TIMESTAMPTZ NOT NULL,
  url_hash TEXT,
  title_norm TEXT,
  calendar_only BOOLEAN NOT NULL DEFAULT FALSE,
  original_title TEXT NOT NULL DEFAULT '',
  original_text TEXT NOT NULL DEFAULT '',
  original_language TEXT NOT NULL DEFAULT '',
  translated_title TEXT,
  translated_summary TEXT,
  source_id TEXT NOT NULL DEFAULT '',
  source_type TEXT NOT NULL DEFAULT '',
  event_at TIMESTAMPTZ,
  event_type TEXT NOT NULL DEFAULT '',
  importance SMALLINT NOT NULL DEFAULT 0,
  entities JSONB NOT NULL DEFAULT '[]'::jsonb,
  assets JSONB NOT NULL DEFAULT '[]'::jsonb,
  event_fingerprint TEXT,
  event_group_id UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_news_items_url_hash
  ON news_items(url_hash)
  WHERE url_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_news_items_title_norm_recent
  ON news_items(published_at DESC, title_norm);
CREATE INDEX IF NOT EXISTS idx_news_items_category_published
  ON news_items(category, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_news_items_regular_category_published
  ON news_items(category, published_at DESC)
  WHERE calendar_only = FALSE;
CREATE INDEX IF NOT EXISTS idx_news_items_published
  ON news_items(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_news_items_source_published ON news_items(source_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_news_items_event_fingerprint ON news_items(event_fingerprint) WHERE event_fingerprint IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_news_items_assets_gin ON news_items USING GIN (assets jsonb_path_ops);

CREATE TABLE IF NOT EXISTS news_event_groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_fingerprint TEXT NOT NULL UNIQUE,
  event_type TEXT NOT NULL DEFAULT '',
  event_at TIMESTAMPTZ,
  importance SMALLINT NOT NULL DEFAULT 0,
  entities JSONB NOT NULL DEFAULT '[]'::jsonb,
  assets JSONB NOT NULL DEFAULT '[]'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_news_event_groups_last_seen ON news_event_groups(last_seen_at DESC);

CREATE TABLE IF NOT EXISTS important_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_type TEXT NOT NULL,
  title TEXT NOT NULL,
  details TEXT NOT NULL DEFAULT '',
  exchange TEXT,
  market_type TEXT,
  symbol TEXT,
  direction TEXT,
  amount_usd DOUBLE PRECISION,
  change_percent DOUBLE PRECISION,
  window_minutes INTEGER NOT NULL DEFAULT 1,
  event_at TIMESTAMPTZ NOT NULL,
  dedup_key TEXT NOT NULL UNIQUE,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  family TEXT NOT NULL DEFAULT '',
  identity_key TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'resolved',
  first_seen_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL,
  resolved_at TIMESTAMPTZ,
  occurrence_count INTEGER NOT NULL DEFAULT 1,
  confidence SMALLINT NOT NULL DEFAULT 1,
  baseline_value DOUBLE PRECISION,
  baseline_ratio DOUBLE PRECISION,
  percentile DOUBLE PRECISION,
  last_observed_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_ingested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT important_events_window_chk CHECK (window_minutes BETWEEN 1 AND 1440),
  CONSTRAINT important_events_status_chk CHECK (status IN ('candidate','confirmed','resolved')),
  CONSTRAINT important_events_confidence_chk CHECK (confidence BETWEEN 1 AND 3)
);
CREATE INDEX IF NOT EXISTS idx_important_events_recent
  ON important_events(event_at DESC);
ALTER TABLE important_events
  ADD COLUMN IF NOT EXISTS observation_count INTEGER NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS confirmation_started_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS confirmation_deadline TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS factor_count SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS confirmed_venues JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS venue_count SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS cluster_start_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS cluster_end_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS source_kind TEXT NOT NULL DEFAULT 'market',
  ADD COLUMN IF NOT EXISTS source_ref TEXT,
  ADD COLUMN IF NOT EXISTS catalyst_event_id UUID REFERENCES news_items(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS entity_id UUID;
CREATE INDEX IF NOT EXISTS idx_important_events_resolution_clock
  ON important_events(status, source_kind, last_ingested_at, id)
  WHERE status IN ('candidate','confirmed') AND source_kind='market';
ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_public_type_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_public_type_chk CHECK (
  status='resolved' OR event_type IN (
    'price_shock','open_interest_shock','large_aggressive_trade',
    'exchange_announcement','onchain_transfer','major_statement'
  )
);

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
CREATE INDEX IF NOT EXISTS idx_important_event_factors_event_time ON important_event_factors(event_id, observed_at DESC);

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

CREATE TABLE IF NOT EXISTS onchain_scan_cursors (
  provider TEXT NOT NULL,
  chain TEXT NOT NULL,
  address TEXT NOT NULL,
  last_scanned_block BIGINT NOT NULL DEFAULT 0,
  cursor_token TEXT NOT NULL DEFAULT '',
  last_scanned_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, chain, address),
  CONSTRAINT onchain_scan_cursors_block_chk CHECK (last_scanned_block >= 0),
  CONSTRAINT onchain_scan_cursors_wallet_fk
    FOREIGN KEY (chain, address) REFERENCES wallet_registry(chain, address) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_onchain_scan_cursors_updated
  ON onchain_scan_cursors(provider, chain, updated_at);
CREATE INDEX IF NOT EXISTS idx_important_events_confirmed_identity
  ON important_events(identity_key) WHERE status = 'confirmed';
CREATE INDEX IF NOT EXISTS idx_important_events_feed
  ON important_events(status, family, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS important_event_outbox (
  id BIGSERIAL PRIMARY KEY,
  event_id UUID NOT NULL REFERENCES important_events(id) ON DELETE CASCADE,
  operation TEXT NOT NULL,
  version INTEGER NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  claim_token TEXT,
  claim_expires_at TIMESTAMPTZ,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  dead_lettered_at TIMESTAMPTZ,
  error_class TEXT,
  publish_attempts INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT important_event_outbox_operation_chk CHECK (operation IN ('created', 'updated', 'resolved')),
  CONSTRAINT important_event_outbox_version_chk CHECK (version > 0),
  CONSTRAINT important_event_outbox_publish_attempts_nonnegative CHECK (publish_attempts >= 0),
  CONSTRAINT important_event_outbox_error_class_check CHECK (error_class IS NULL OR error_class IN ('transient', 'permanent', 'transient_exhausted')),
  CONSTRAINT important_event_outbox_dead_letter_check CHECK (dead_lettered_at IS NULL OR (error_class IS NOT NULL AND error_class IN ('permanent', 'transient_exhausted'))),
  CONSTRAINT uq_important_event_outbox_version UNIQUE (event_id, operation, version)
);
CREATE INDEX IF NOT EXISTS idx_important_event_outbox_pending
  ON important_event_outbox(id)
  WHERE published_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_important_event_outbox_ready
  ON important_event_outbox(next_attempt_at, id)
  WHERE dead_lettered_at IS NULL AND published_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_important_event_outbox_event_pending
  ON important_event_outbox(event_id, id)
  WHERE published_at IS NULL;

CREATE TABLE IF NOT EXISTS news_likes (
  news_id UUID NOT NULL REFERENCES news_items(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (news_id, user_id)
);

CREATE TABLE IF NOT EXISTS community_posts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DROP TRIGGER IF EXISTS community_posts_set_updated_at ON community_posts;
CREATE TRIGGER community_posts_set_updated_at BEFORE UPDATE ON community_posts FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS community_comments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id UUID NOT NULL REFERENCES community_posts(id) ON DELETE CASCADE,
  parent_id UUID REFERENCES community_comments(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DROP TRIGGER IF EXISTS community_comments_set_updated_at ON community_comments;
CREATE TRIGGER community_comments_set_updated_at BEFORE UPDATE ON community_comments FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS community_reactions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id UUID REFERENCES community_posts(id) ON DELETE CASCADE,
  comment_id UUID REFERENCES community_comments(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  emoji TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT community_reactions_target_chk CHECK ((post_id IS NOT NULL AND comment_id IS NULL) OR (post_id IS NULL AND comment_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_community_reactions_post ON community_reactions(user_id, post_id, emoji) WHERE post_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_community_reactions_comment ON community_reactions(user_id, comment_id, emoji) WHERE comment_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS community_channels (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  is_private BOOLEAN NOT NULL DEFAULT false,
  min_plan TEXT NOT NULL DEFAULT 'Free',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DROP TRIGGER IF EXISTS community_channels_set_updated_at ON community_channels;
CREATE TRIGGER community_channels_set_updated_at BEFORE UPDATE ON community_channels FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS banned_ips (
  ip TEXT PRIMARY KEY,
  reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS telegram_link_tokens (
  token TEXT PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS telegram_signal_limits (
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  symbol TEXT NOT NULL,
  daily_limit INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, symbol)
);
DROP TRIGGER IF EXISTS telegram_signal_limits_set_updated_at ON telegram_signal_limits;
CREATE TRIGGER telegram_signal_limits_set_updated_at BEFORE UPDATE ON telegram_signal_limits FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS telegram_limit_requests (
  telegram_id BIGINT PRIMARY KEY,
  step TEXT NOT NULL,
  symbol TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DROP TRIGGER IF EXISTS telegram_limit_requests_set_updated_at ON telegram_limit_requests;
CREATE TRIGGER telegram_limit_requests_set_updated_at BEFORE UPDATE ON telegram_limit_requests FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS ai_usage (
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  day DATE NOT NULL,
  tokens_used INTEGER NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, day)
);
DROP TRIGGER IF EXISTS ai_usage_set_updated_at ON ai_usage;
CREATE TRIGGER ai_usage_set_updated_at BEFORE UPDATE ON ai_usage FOR EACH ROW EXECUTE FUNCTION trg_ai_usage_updated_at();

CREATE TABLE IF NOT EXISTS password_reset_tokens (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user_id ON password_reset_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_expires_at ON password_reset_tokens(expires_at);

COMMIT;
