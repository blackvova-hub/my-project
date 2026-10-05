BEGIN;

CREATE TABLE IF NOT EXISTS signal_delivery_outbox (
  id BIGSERIAL PRIMARY KEY,
  signal_id UUID NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  stream_published_at TIMESTAMPTZ,
  telegram_processed_at TIMESTAMPTZ,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  CONSTRAINT uq_signal_delivery_outbox_signal UNIQUE (signal_id)
);

CREATE INDEX IF NOT EXISTS idx_signal_delivery_outbox_pending
  ON signal_delivery_outbox(id)
  WHERE stream_published_at IS NULL OR telegram_processed_at IS NULL;

COMMIT;
