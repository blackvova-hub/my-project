BEGIN;

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

COMMIT;
