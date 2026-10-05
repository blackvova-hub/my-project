BEGIN;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS token_hash TEXT;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS public_id UUID NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash) WHERE token_hash IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_public_id ON sessions(public_id);
UPDATE sessions SET revoked_at = now() WHERE revoked_at IS NULL;
DELETE FROM sessions WHERE token_hash IS NULL;
ALTER TABLE sessions ALTER COLUMN token_hash SET NOT NULL;
COMMIT;
