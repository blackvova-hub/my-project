-- 007_add_session_user_agent.sql
-- add user agent to sessions for device/browser visibility

ALTER TABLE sessions
  ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '';
