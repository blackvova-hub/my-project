BEGIN;

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS last_ip TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS banned_ips (
  ip TEXT PRIMARY KEY,
  banned_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_banned_ips_created_at
  ON banned_ips(created_at);

COMMIT;
