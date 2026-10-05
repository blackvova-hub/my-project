BEGIN;

-- Retry scheduling prevents one failing row from being reclaimed in a tight
-- loop and blocking unrelated deliveries. Dead-lettered rows remain in
-- PostgreSQL with their payload for audited/manual replay.
ALTER TABLE signal_delivery_outbox
  ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS dead_lettered_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS error_class TEXT,
  ADD COLUMN IF NOT EXISTS stream_attempts INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS telegram_attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE important_event_outbox
  ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS dead_lettered_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS error_class TEXT,
  ADD COLUMN IF NOT EXISTS publish_attempts INTEGER NOT NULL DEFAULT 0;

-- Keep reruns safe after an interrupted/manual partial rollout where the
-- column may already exist but lack the final invariant.
UPDATE signal_delivery_outbox SET next_attempt_at=now() WHERE next_attempt_at IS NULL;
ALTER TABLE signal_delivery_outbox ALTER COLUMN next_attempt_at SET DEFAULT now();
ALTER TABLE signal_delivery_outbox ALTER COLUMN next_attempt_at SET NOT NULL;
UPDATE important_event_outbox SET next_attempt_at=now() WHERE next_attempt_at IS NULL;
ALTER TABLE important_event_outbox ALTER COLUMN next_attempt_at SET DEFAULT now();
ALTER TABLE important_event_outbox ALTER COLUMN next_attempt_at SET NOT NULL;

ALTER TABLE signal_delivery_outbox
  DROP CONSTRAINT IF EXISTS signal_delivery_outbox_step_attempts_nonnegative,
  ADD CONSTRAINT signal_delivery_outbox_step_attempts_nonnegative
    CHECK (stream_attempts >= 0 AND telegram_attempts >= 0) NOT VALID;
ALTER TABLE signal_delivery_outbox
  VALIDATE CONSTRAINT signal_delivery_outbox_step_attempts_nonnegative;

ALTER TABLE important_event_outbox
  DROP CONSTRAINT IF EXISTS important_event_outbox_publish_attempts_nonnegative,
  ADD CONSTRAINT important_event_outbox_publish_attempts_nonnegative
    CHECK (publish_attempts >= 0) NOT VALID;
ALTER TABLE important_event_outbox
  VALIDATE CONSTRAINT important_event_outbox_publish_attempts_nonnegative;

ALTER TABLE signal_delivery_outbox
  DROP CONSTRAINT IF EXISTS signal_delivery_outbox_error_class_check,
  ADD CONSTRAINT signal_delivery_outbox_error_class_check CHECK (
    error_class IS NULL OR error_class IN ('transient', 'permanent', 'transient_exhausted')
  ) NOT VALID,
  DROP CONSTRAINT IF EXISTS signal_delivery_outbox_dead_letter_check,
  ADD CONSTRAINT signal_delivery_outbox_dead_letter_check CHECK (
    dead_lettered_at IS NULL OR (error_class IS NOT NULL AND error_class IN ('permanent', 'transient_exhausted'))
  ) NOT VALID;
ALTER TABLE signal_delivery_outbox
  VALIDATE CONSTRAINT signal_delivery_outbox_error_class_check;
ALTER TABLE signal_delivery_outbox
  VALIDATE CONSTRAINT signal_delivery_outbox_dead_letter_check;

ALTER TABLE important_event_outbox
  DROP CONSTRAINT IF EXISTS important_event_outbox_error_class_check,
  ADD CONSTRAINT important_event_outbox_error_class_check CHECK (
    error_class IS NULL OR error_class IN ('transient', 'permanent', 'transient_exhausted')
  ) NOT VALID,
  DROP CONSTRAINT IF EXISTS important_event_outbox_dead_letter_check,
  ADD CONSTRAINT important_event_outbox_dead_letter_check CHECK (
    dead_lettered_at IS NULL OR (error_class IS NOT NULL AND error_class IN ('permanent', 'transient_exhausted'))
  ) NOT VALID;
ALTER TABLE important_event_outbox
  VALIDATE CONSTRAINT important_event_outbox_error_class_check;
ALTER TABLE important_event_outbox
  VALIDATE CONSTRAINT important_event_outbox_dead_letter_check;

CREATE INDEX IF NOT EXISTS idx_signal_delivery_outbox_ready
  ON signal_delivery_outbox(next_attempt_at, id)
  WHERE dead_lettered_at IS NULL
    AND (stream_published_at IS NULL OR telegram_processed_at IS NULL);

CREATE INDEX IF NOT EXISTS idx_important_event_outbox_ready
  ON important_event_outbox(next_attempt_at, id)
  WHERE dead_lettered_at IS NULL AND published_at IS NULL;

-- Supports the dispatcher invariant that versions of one event are emitted in
-- order even while an unrelated event is backing off.
CREATE INDEX IF NOT EXISTS idx_important_event_outbox_event_pending
  ON important_event_outbox(event_id, id)
  WHERE published_at IS NULL;

COMMIT;
