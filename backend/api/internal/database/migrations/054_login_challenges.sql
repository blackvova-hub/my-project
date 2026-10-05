BEGIN;
CREATE TABLE IF NOT EXISTS login_challenges (
 token_hash TEXT PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_login_challenges_expires ON login_challenges(expires_at);
COMMIT;
