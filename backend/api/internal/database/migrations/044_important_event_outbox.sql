BEGIN;

CREATE TABLE IF NOT EXISTS important_event_outbox (
  id BIGSERIAL PRIMARY KEY,
  event_id UUID NOT NULL REFERENCES important_events(id) ON DELETE CASCADE,
  operation TEXT NOT NULL,
  version INTEGER NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  CONSTRAINT important_event_outbox_operation_chk CHECK (operation IN ('created', 'updated', 'resolved')),
  CONSTRAINT important_event_outbox_version_chk CHECK (version > 0),
  CONSTRAINT uq_important_event_outbox_version UNIQUE (event_id, operation, version)
);

CREATE INDEX IF NOT EXISTS idx_important_event_outbox_pending
  ON important_event_outbox(id)
  WHERE published_at IS NULL;

COMMIT;
