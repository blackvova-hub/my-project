BEGIN;

CREATE TABLE IF NOT EXISTS two_fa_codes (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose TEXT NOT NULL,
  code_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, purpose)
);

CREATE INDEX IF NOT EXISTS idx_two_fa_codes_expires
  ON two_fa_codes(expires_at);

COMMIT;
