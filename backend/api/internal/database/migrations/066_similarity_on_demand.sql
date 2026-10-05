-- Only an explicit user search creates work. Vector coverage stays shared.
CREATE TABLE IF NOT EXISTS similarity_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id text NOT NULL,
 request_hash text NOT NULL,
 request jsonb NOT NULL,
 allowed text[] NOT NULL,
 status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','running','completed','failed','cancelled')),
 phase text NOT NULL DEFAULT 'В очереди',
 progress integer NOT NULL DEFAULT 0 CHECK(progress BETWEEN 0 AND 100),
 result jsonb,
 error text NOT NULL DEFAULT '',
 lease_token uuid,
 lease_until timestamptz NOT NULL DEFAULT '-infinity',
 attempts integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS similarity_jobs_queue ON similarity_jobs(created_at) WHERE status IN ('queued','running');
CREATE INDEX IF NOT EXISTS similarity_jobs_owner ON similarity_jobs(user_id,request_hash,created_at DESC);
CREATE INDEX IF NOT EXISTS similarity_streams_requested ON similarity_streams(version,market,span,symbol) WHERE kind='features';
