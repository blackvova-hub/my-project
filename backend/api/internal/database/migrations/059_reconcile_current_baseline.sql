BEGIN;

-- Compatibility layer for databases created from baseline/001_base_current.sql.
ALTER TABLE news_items
  ADD COLUMN IF NOT EXISTS publication_eligible BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_news_items_publication_feed
  ON news_items(category, published_at DESC)
  WHERE publication_eligible = TRUE;

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS token_hash TEXT;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS public_id UUID NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash) WHERE token_hash IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_public_id ON sessions(public_id);

-- Legacy plaintext sessions cannot satisfy the hardened token contract.
UPDATE sessions SET revoked_at = now() WHERE token_hash IS NULL AND revoked_at IS NULL;
DELETE FROM sessions WHERE token_hash IS NULL;
ALTER TABLE sessions ALTER COLUMN token_hash SET NOT NULL;

CREATE TABLE IF NOT EXISTS login_challenges (
  token_hash TEXT PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_login_challenges_expires ON login_challenges(expires_at);

COMMIT;
