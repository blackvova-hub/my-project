BEGIN;

-- Legacy migration superseded by `018_add_community_feed.sql` / current backend schema.
-- Intentionally kept as no-op to avoid creating incompatible community schema
-- (`author_id/body/target_type`) on clean databases.

COMMIT;
