BEGIN;

CREATE TABLE IF NOT EXISTS community_posts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_community_posts_created_at
  ON community_posts(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_community_posts_user
  ON community_posts(user_id);

CREATE TABLE IF NOT EXISTS community_comments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id UUID NOT NULL REFERENCES community_posts(id) ON DELETE CASCADE,
  parent_id UUID NULL REFERENCES community_comments(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_community_comments_post
  ON community_comments(post_id);

CREATE INDEX IF NOT EXISTS idx_community_comments_parent
  ON community_comments(parent_id);

CREATE INDEX IF NOT EXISTS idx_community_comments_user
  ON community_comments(user_id);

CREATE TABLE IF NOT EXISTS community_reactions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id UUID NULL REFERENCES community_posts(id) ON DELETE CASCADE,
  comment_id UUID NULL REFERENCES community_comments(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  emoji TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT community_reactions_target_chk CHECK (
    (post_id IS NOT NULL AND comment_id IS NULL) OR
    (post_id IS NULL AND comment_id IS NOT NULL)
  )
);

CREATE INDEX IF NOT EXISTS idx_community_reactions_post
  ON community_reactions(post_id);

CREATE INDEX IF NOT EXISTS idx_community_reactions_comment
  ON community_reactions(comment_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_community_reactions_post
  ON community_reactions(user_id, post_id, emoji)
  WHERE post_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_community_reactions_comment
  ON community_reactions(user_id, comment_id, emoji)
  WHERE comment_id IS NOT NULL;

COMMIT;
