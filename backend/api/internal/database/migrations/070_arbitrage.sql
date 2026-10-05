-- Additive: existing scanner settings and rules are unaffected.
CREATE TABLE arbitrage_preferences (
 user_id text PRIMARY KEY,
 settings jsonb NOT NULL DEFAULT '{}',
 favorites jsonb NOT NULL DEFAULT '[]',
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (jsonb_typeof(settings) = 'object'),
 CHECK (jsonb_typeof(favorites) = 'array' AND jsonb_array_length(favorites) <= 200)
);
