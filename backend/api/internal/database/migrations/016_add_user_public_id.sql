BEGIN;

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS public_id INT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_public_id
  ON users(public_id);

DO $$
DECLARE
  v_id INT;
  r RECORD;
BEGIN
  FOR r IN SELECT id FROM users WHERE public_id IS NULL LOOP
    LOOP
      v_id := floor(random() * 9000000 + 1000000);
      EXIT WHEN NOT EXISTS (SELECT 1 FROM users WHERE public_id = v_id);
    END LOOP;
    UPDATE users SET public_id = v_id WHERE id = r.id;
  END LOOP;
END $$;

ALTER TABLE users
  ALTER COLUMN public_id SET NOT NULL;

COMMIT;
