-- 026_add_session_ip.sql
-- add ip to sessions for login history

ALTER TABLE sessions
  ADD COLUMN IF NOT EXISTS ip TEXT NOT NULL DEFAULT '';
