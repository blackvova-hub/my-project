BEGIN;

ALTER TABLE news_items
  ADD COLUMN IF NOT EXISTS publication_eligible BOOLEAN NOT NULL DEFAULT FALSE;

-- Existing rows predate the explicit moderation contract. Keeping them false
-- prevents stale/unreviewed items from being sent after a publisher restart.
CREATE INDEX IF NOT EXISTS idx_news_items_publication_feed
  ON news_items(category, published_at DESC)
  WHERE publication_eligible = TRUE;

UPDATE important_events
SET status = CASE WHEN event_type IN (
    'price_shock','open_interest_shock','large_aggressive_trade',
    'exchange_announcement','onchain_transfer','major_statement'
  ) THEN 'confirmed' ELSE 'resolved' END,
    resolved_at = CASE WHEN event_type IN (
      'price_shock','open_interest_shock','large_aggressive_trade',
      'exchange_announcement','onchain_transfer','major_statement'
    ) THEN resolved_at ELSE COALESCE(resolved_at, now()) END
WHERE status = 'active';

-- Candidates created by the previous lifecycle do not carry the new explicit
-- official-confirmation contract and must not be promoted accidentally.
UPDATE important_events
SET status = 'resolved',
    resolved_at = COALESCE(resolved_at, now())
WHERE status = 'candidate';

DROP INDEX IF EXISTS idx_important_events_recent;
DROP INDEX IF EXISTS idx_important_events_feed;
DROP INDEX IF EXISTS uq_important_events_active_identity;
DROP INDEX IF EXISTS idx_important_events_active_instrument;

ALTER TABLE important_events
  DROP CONSTRAINT IF EXISTS important_events_priority_chk,
  DROP CONSTRAINT IF EXISTS important_events_severity_chk,
  DROP CONSTRAINT IF EXISTS important_events_peak_severity_chk,
  DROP CONSTRAINT IF EXISTS important_events_status_chk,
  DROP COLUMN IF EXISTS priority,
  DROP COLUMN IF EXISTS severity,
  DROP COLUMN IF EXISTS peak_severity,
  DROP COLUMN IF EXISTS candidate_score;

ALTER TABLE important_events
  ADD CONSTRAINT important_events_status_chk
  CHECK (status IN ('candidate','confirmed','resolved'));

CREATE INDEX idx_important_events_recent
  ON important_events(event_at DESC);
CREATE INDEX idx_important_events_feed
  ON important_events(status, family, last_seen_at DESC);
CREATE INDEX idx_important_events_confirmed_identity
  ON important_events(identity_key)
  WHERE status = 'confirmed';

COMMIT;
