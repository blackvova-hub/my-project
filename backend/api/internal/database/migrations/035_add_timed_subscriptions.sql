BEGIN;

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS subscription_expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS subscription_frozen_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS subscription_frozen_days_remaining INTEGER NOT NULL DEFAULT 0;

UPDATE users
SET plan = lower(trim(plan))
WHERE plan IS NOT NULL;

UPDATE users
SET plan = 'standard'
WHERE plan = 'standart';

UPDATE users
SET plan = 'free'
WHERE plan NOT IN ('free', 'standard', 'pro');

UPDATE users
SET subscription_expires_at = now() + interval '3650 days'
WHERE plan IN ('standard', 'pro')
  AND subscription_expires_at IS NULL
  AND COALESCE(subscription_frozen_days_remaining, 0) <= 0;

CREATE OR REPLACE FUNCTION effective_plan(
  raw_plan TEXT,
  subscription_expires_at TIMESTAMPTZ,
  subscription_frozen_at TIMESTAMPTZ
)
RETURNS TEXT
LANGUAGE plpgsql
AS $$
DECLARE
  p TEXT;
BEGIN
  p := lower(trim(COALESCE(raw_plan, 'free')));
  IF p = 'standart' THEN
    p := 'standard';
  END IF;

  IF p NOT IN ('free', 'standard', 'pro') THEN
    p := 'free';
  END IF;

  IF p IN ('standard', 'pro') THEN
    IF subscription_frozen_at IS NOT NULL THEN
      RETURN 'free';
    END IF;
    IF subscription_expires_at IS NULL OR subscription_expires_at <= now() THEN
      RETURN 'free';
    END IF;
    RETURN p;
  END IF;

  RETURN 'free';
END;
$$;

COMMIT;
