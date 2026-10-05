BEGIN;

UPDATE users
SET plan = lower(trim(COALESCE(plan, 'free')));

UPDATE users
SET plan = 'standard'
WHERE plan = 'standart';

UPDATE users
SET plan = 'free'
WHERE plan NOT IN ('free', 'standard', 'pro');

UPDATE users
SET subscription_frozen_days_remaining = GREATEST(COALESCE(subscription_frozen_days_remaining, 0), 0);

UPDATE users
SET plan = 'free',
    subscription_expires_at = NULL,
    subscription_frozen_at = NULL,
    subscription_frozen_days_remaining = 0,
    updated_at = now()
WHERE plan IN ('standard', 'pro')
  AND subscription_frozen_at IS NULL
  AND (subscription_expires_at IS NULL OR subscription_expires_at <= now());

UPDATE users
SET subscription_expires_at = NULL
WHERE subscription_frozen_at IS NOT NULL;

UPDATE users
SET subscription_frozen_at = NULL,
    subscription_frozen_days_remaining = 0
WHERE subscription_frozen_at IS NOT NULL
  AND COALESCE(subscription_frozen_days_remaining, 0) <= 0;

ALTER TABLE users
  DROP CONSTRAINT IF EXISTS users_plan_valid,
  DROP CONSTRAINT IF EXISTS users_subscription_frozen_days_nonnegative;

ALTER TABLE users
  ADD CONSTRAINT users_plan_valid CHECK (lower(trim(plan)) IN ('free', 'standard', 'pro')),
  ADD CONSTRAINT users_subscription_frozen_days_nonnegative CHECK (subscription_frozen_days_remaining >= 0);

CREATE OR REPLACE FUNCTION trg_normalize_user_subscription()
RETURNS TRIGGER AS $$
DECLARE
  p TEXT;
BEGIN
  p := lower(trim(COALESCE(NEW.plan, 'free')));
  IF p = 'standart' THEN
    p := 'standard';
  END IF;
  IF p NOT IN ('free', 'standard', 'pro') THEN
    p := 'free';
  END IF;

  NEW.plan := p;
  NEW.subscription_frozen_days_remaining := GREATEST(COALESCE(NEW.subscription_frozen_days_remaining, 0), 0);

  IF NEW.plan = 'free' THEN
    NEW.subscription_expires_at := NULL;
    NEW.subscription_frozen_at := NULL;
    NEW.subscription_frozen_days_remaining := 0;
    RETURN NEW;
  END IF;

  IF NEW.subscription_frozen_at IS NOT NULL THEN
    NEW.subscription_expires_at := NULL;
    IF NEW.subscription_frozen_days_remaining <= 0 THEN
      NEW.subscription_frozen_at := NULL;
      NEW.subscription_frozen_days_remaining := 0;
      NEW.plan := 'free';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.subscription_expires_at IS NULL OR NEW.subscription_expires_at <= now() THEN
    NEW.plan := 'free';
    NEW.subscription_expires_at := NULL;
    NEW.subscription_frozen_at := NULL;
    NEW.subscription_frozen_days_remaining := 0;
    RETURN NEW;
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS users_normalize_subscription ON users;
CREATE TRIGGER users_normalize_subscription
BEFORE INSERT OR UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION trg_normalize_user_subscription();

COMMIT;
