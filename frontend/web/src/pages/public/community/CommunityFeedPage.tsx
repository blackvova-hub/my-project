import { useCallback, useEffect, useMemo, useState } from "react";
import { useAuth } from "../../../shared/auth/AuthContext";
import {
  createCommunityComment,
  createCommunityPost,
  fetchCommunityComments,
  fetchCommunityFeed,
  toggleCommunityReaction,
} from "./communityApi";
import type { CommunityComment, CommunityPost, ReactionSummary } from "./types";

const EMOJIS = ["👍", "🔥", "💡", "😂", "💎"] as const;
const PAGE_SIZE = 10;

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString();
}

function initials(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const first = parts[0]?.[0] ?? "U";
  const second = parts[1]?.[0] ?? "";
  return (first + second).toUpperCase();
}

function ReactionBar({
  reactions,
  onToggle,
}: {
  reactions?: ReactionSummary[] | null;
  onToggle: (emoji: string) => void;
}) {
  const reactionMap = useMemo(() => {
    const map = new Map<string, ReactionSummary>();
    (reactions ?? []).forEach((r) => map.set(r.emoji, r));
    return map;
  }, [reactions]);

  return (
    <div className="flex flex-wrap items-center gap-2">
      {EMOJIS.map((emoji) => {
        const item = reactionMap.get(emoji);
        const count = item?.count ?? 0;
        const reacted = item?.reacted ?? false;
        return (
          <button
            key={emoji}
            type="button"
            onClick={() => onToggle(emoji)}
            className={
              "flex items-center gap-1 rounded-full border px-2 py-1 text-xs transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring " +
              (reacted
                ? "border-primary/40 bg-primary/15 text-primary"
                : "border-border/60 bg-secondary/40 text-muted-foreground hover:bg-accent")
            }
          >
            <span>{emoji}</span>
            <span>{count}</span>
          </button>
        );
      })}
    </div>
  );
}

export default function CommunityFeedPage() {
  const { user } = useAuth();
  const [posts, setPosts] = useState<CommunityPost[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [sort, setSort] = useState<"new" | "top">("new");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const [newPost, setNewPost] = useState("");
  const [isPosting, setIsPosting] = useState(false);
  const [commentsByPost, setCommentsByPost] = useState<Record<string, CommunityComment[]>>({});
  const [commentsOpen, setCommentsOpen] = useState<Record<string, boolean>>({});
  const [commentsLoading, setCommentsLoading] = useState<Record<string, boolean>>({});
  const [commentDrafts, setCommentDrafts] = useState<Record<string, string>>({});
  const [replyDrafts, setReplyDrafts] = useState<Record<string, string>>({});

  const canLoadMore = posts.length < total;

  const loadFeed = useCallback(async (nextOffset = 0, reset = false) => {
    setIsLoading(reset);
    setError("");
    try {
      const res = await fetchCommunityFeed({
        limit: PAGE_SIZE,
        offset: nextOffset,
        sort,
      });
      setTotal(res.total);
      setOffset(nextOffset);
      setPosts((prev) => (reset ? res.items : [...prev, ...res.items]));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось загрузить ленту");
    } finally {
      setIsLoading(false);
    }
  }, [sort]);

  useEffect(() => {
    setPosts([]);
    void loadFeed(0, true);
  }, [loadFeed]);

  async function handleCreatePost() {
    const content = newPost.trim();
    if (!content || isPosting) return;
    setIsPosting(true);
    try {
      const post = await createCommunityPost(content);
      setPosts((prev) => [post, ...prev]);
      setTotal((prev) => prev + 1);
      setNewPost("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось отправить пост");
    } finally {
      setIsPosting(false);
    }
  }

  async function handleToggleReaction(postId: string, emoji: string) {
    const res = await toggleCommunityReaction({ targetType: "post", targetId: postId, emoji });
    setPosts((prev) =>
      prev.map((post) => (post.id === postId ? { ...post, reactions: res.reactions } : post))
    );
  }

  async function handleToggleCommentReaction(commentId: string, emoji: string) {
    const res = await toggleCommunityReaction({ targetType: "comment", targetId: commentId, emoji });
    setCommentsByPost((prev) => {
      const next = { ...prev };
      for (const postId of Object.keys(next)) {
        next[postId] = next[postId].map((c) =>
          c.id === commentId ? { ...c, reactions: res.reactions } : c
        );
      }
      return next;
    });
  }

  async function handleToggleComments(postId: string) {
    setCommentsOpen((prev) => ({ ...prev, [postId]: !prev[postId] }));
    if (commentsByPost[postId]) return;
    setCommentsLoading((prev) => ({ ...prev, [postId]: true }));
    try {
      const items = await fetchCommunityComments(postId);
      setCommentsByPost((prev) => ({ ...prev, [postId]: items }));
    } finally {
      setCommentsLoading((prev) => ({ ...prev, [postId]: false }));
    }
  }

  async function handleCreateComment(postId: string, parentId?: string | null) {
    const draft = (parentId ? replyDrafts[parentId] : commentDrafts[postId]) ?? "";
    const content = draft.trim();
    if (!content) return;

    const comment = await createCommunityComment({ postId, content, parentId });
    setCommentsByPost((prev) => ({
      ...prev,
      [postId]: [...(prev[postId] ?? []), comment],
    }));
    setPosts((prev) =>
      prev.map((post) =>
        post.id === postId ? { ...post, commentCount: post.commentCount + 1 } : post
      )
    );

    if (parentId) {
      setReplyDrafts((prev) => ({ ...prev, [parentId]: "" }));
    } else {
      setCommentDrafts((prev) => ({ ...prev, [postId]: "" }));
    }
    setCommentsOpen((prev) => ({ ...prev, [postId]: true }));
  }

  return (
    <div className="mx-auto max-w-6xl px-4 py-10 md:py-14">
      <div className="rounded-3xl border border-border/60 bg-card p-6 text-card-foreground md:p-8">
        <div className="flex flex-col gap-6 md:flex-row md:items-center md:justify-between">
          <div>
            <div className="text-xs uppercase tracking-wide text-muted-foreground">Комьюнити</div>
            <h1 className="mt-3 text-3xl font-extrabold tracking-tight md:text-4xl">Лента</h1>
            <p className="mt-3 max-w-2xl text-sm text-muted-foreground">
              Обсуждения, идеи и реакции сообщества в одном месте.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setSort("new")}
              className={
                "rounded-full border px-4 py-2 text-sm transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring " +
                (sort === "new"
                  ? "border-primary/40 bg-primary/15 text-primary"
                  : "border-border/60 bg-secondary/40 text-muted-foreground hover:bg-accent")
              }
            >
              Новые
            </button>
            <button
              type="button"
              onClick={() => setSort("top")}
              className={
                "rounded-full border px-4 py-2 text-sm transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring " +
                (sort === "top"
                  ? "border-primary/40 bg-primary/15 text-primary"
                  : "border-border/60 bg-secondary/40 text-muted-foreground hover:bg-accent")
              }
            >
              Популярные
            </button>
          </div>
        </div>

        <div className="mt-6 rounded-2xl border border-border/60 bg-surface-raised p-4">
          <div className="flex items-start gap-3">
            <div className="h-10 w-10 rounded-full border border-border/60 bg-secondary flex items-center justify-center text-sm font-semibold">
              {user?.displayName ? initials(user.displayName) : "U"}
            </div>
            <div className="flex-1">
              <textarea
                value={newPost}
                onChange={(e) => setNewPost(e.target.value)}
                rows={3}
                className="w-full rounded-xl border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                placeholder="Поделись идеей или наблюдением..."
              />
              <div className="mt-3 flex items-center justify-between">
                <span className="text-xs text-muted-foreground">До 2000 символов</span>
                <button
                  type="button"
                  onClick={handleCreatePost}
                  disabled={isPosting || !newPost.trim()}
                  className="rounded-xl border border-primary bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground transition-colors hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-50"
                >
                  Опубликовать
                </button>
              </div>
            </div>
          </div>
        </div>

        {error ? (
          <div className="mt-6 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        ) : null}

        <div className="mt-6 space-y-4">
          {isLoading && posts.length === 0 ? (
            <div className="rounded-2xl border border-border/60 bg-surface-raised p-6 text-sm text-muted-foreground">
              Загружаем ленту...
            </div>
          ) : null}

          {posts.map((post) => {
            const comments = commentsByPost[post.id] ?? [];
            const open = commentsOpen[post.id];
            const rootComments = comments.filter((c) => !c.parentId);
            const replies = comments.filter((c) => c.parentId);
            const repliesByParent = replies.reduce<Record<string, CommunityComment[]>>((acc, item) => {
              const key = item.parentId ?? "";
              acc[key] = acc[key] ? [...acc[key], item] : [item];
              return acc;
            }, {});

            return (
              <div key={post.id} className="rounded-2xl border border-border/60 bg-surface-raised p-5">
                <div className="flex items-start gap-3">
                  <div className="h-10 w-10 overflow-hidden rounded-full border border-border/60 bg-secondary flex items-center justify-center text-sm font-semibold">
                    {post.author.avatarUrl ? (
                      <img
                        src={post.author.avatarUrl}
                        alt={post.author.displayName}
                        className="h-full w-full object-cover"
                      />
                    ) : (
                      initials(post.author.displayName || "User")
                    )}
                  </div>
                  <div className="flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <div className="text-sm font-semibold text-card-foreground">
                        {post.author.displayName || "User"}
                      </div>
                      <span className="text-xs text-muted-foreground">ID {post.author.publicId}</span>
                      <span className="text-xs text-muted-foreground">•</span>
                      <span className="text-xs text-muted-foreground">{formatDate(post.createdAt)}</span>
                    </div>
                    <p className="mt-3 text-sm text-card-foreground/80 whitespace-pre-wrap">
                      {post.content}
                    </p>
                    <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
                      <ReactionBar
                        reactions={post.reactions ?? []}
                        onToggle={(emoji) => handleToggleReaction(post.id, emoji)}
                      />
                      <button
                        type="button"
                        onClick={() => handleToggleComments(post.id)}
                        className="text-xs text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                      >
                        Комментарии ({post.commentCount})
                      </button>
                    </div>
                  </div>
                </div>

                {open ? (
                  <div className="mt-4 border-t border-border/60 pt-4">
                    {commentsLoading[post.id] ? (
                      <div className="text-xs text-muted-foreground">Загружаем комментарии...</div>
                    ) : null}

                    <div className="space-y-4">
                      {rootComments.map((comment) => (
                        <div key={comment.id} className="rounded-xl border border-border/40 bg-secondary/40 p-3">
                          <div className="flex items-start gap-3">
                            <div className="h-8 w-8 rounded-full border border-border/60 bg-secondary flex items-center justify-center text-[11px] font-semibold">
                              {comment.author.avatarUrl ? (
                                <img
                                  src={comment.author.avatarUrl}
                                  alt={comment.author.displayName}
                                  className="h-full w-full rounded-full object-cover"
                                />
                              ) : (
                                initials(comment.author.displayName || "User")
                              )}
                            </div>
                            <div className="flex-1">
                              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                <span className="font-semibold text-card-foreground/80">
                                  {comment.author.displayName || "User"}
                                </span>
                                <span>•</span>
                                <span>{formatDate(comment.createdAt)}</span>
                              </div>
                              <div className="mt-2 text-sm text-card-foreground/80 whitespace-pre-wrap">
                                {comment.content}
                              </div>
                              <div className="mt-3 flex flex-wrap items-center gap-3">
                                <ReactionBar
                                  reactions={comment.reactions ?? []}
                                  onToggle={(emoji) => handleToggleCommentReaction(comment.id, emoji)}
                                />
                                <button
                                  type="button"
                                  onClick={() =>
                                    setReplyDrafts((prev) => ({
                                      ...prev,
                                      [comment.id]: prev[comment.id] ?? "",
                                    }))
                                  }
                                  className="text-xs text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                                >
                                  Ответить
                                </button>
                              </div>

                              {replyDrafts[comment.id] !== undefined ? (
                                <div className="mt-3">
                                  <textarea
                                    rows={2}
                                    value={replyDrafts[comment.id]}
                                    onChange={(e) =>
                                      setReplyDrafts((prev) => ({
                                        ...prev,
                                        [comment.id]: e.target.value,
                                      }))
                                    }
                                    className="w-full rounded-lg border border-input bg-background px-3 py-2 text-xs text-foreground placeholder:text-muted-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                                    placeholder="Ответить..."
                                  />
                                  <div className="mt-2 flex justify-end">
                                    <button
                                      type="button"
                                      onClick={() => handleCreateComment(post.id, comment.id)}
                                      className="rounded-lg border border-border bg-secondary px-3 py-1 text-xs text-secondary-foreground transition-colors hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                                    >
                                      Отправить
                                    </button>
                                  </div>
                                </div>
                              ) : null}

                              {repliesByParent[comment.id]?.length ? (
                                <div className="mt-4 space-y-3 border-l border-border/60 pl-3">
                                  {repliesByParent[comment.id].map((reply) => (
                                    <div key={reply.id} className="rounded-lg border border-border/40 bg-secondary/40 p-2">
                                      <div className="flex items-start gap-2">
                                        <div className="h-7 w-7 rounded-full border border-border/60 bg-secondary flex items-center justify-center text-[10px] font-semibold">
                                          {reply.author.avatarUrl ? (
                                            <img
                                              src={reply.author.avatarUrl}
                                              alt={reply.author.displayName}
                                              className="h-full w-full rounded-full object-cover"
                                            />
                                          ) : (
                                            initials(reply.author.displayName || "User")
                                          )}
                                        </div>
                                        <div className="flex-1">
                                          <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                                            <span className="font-semibold text-card-foreground/80">
                                              {reply.author.displayName || "User"}
                                            </span>
                                            <span>•</span>
                                            <span>{formatDate(reply.createdAt)}</span>
                                          </div>
                                          <div className="mt-1 text-xs text-card-foreground/80 whitespace-pre-wrap">
                                            {reply.content}
                                          </div>
                                          <div className="mt-2">
                                            <ReactionBar
                                              reactions={reply.reactions ?? []}
                                              onToggle={(emoji) => handleToggleCommentReaction(reply.id, emoji)}
                                            />
                                          </div>
                                        </div>
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              ) : null}
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>

                    <div className="mt-4 rounded-xl border border-border/60 bg-surface-raised p-3">
                      <textarea
                        rows={2}
                        value={commentDrafts[post.id] ?? ""}
                        onChange={(e) =>
                          setCommentDrafts((prev) => ({ ...prev, [post.id]: e.target.value }))
                        }
                        className="w-full rounded-lg border border-input bg-background px-3 py-2 text-xs text-foreground placeholder:text-muted-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                        placeholder="Написать комментарий..."
                      />
                      <div className="mt-2 flex justify-end">
                        <button
                          type="button"
                          onClick={() => handleCreateComment(post.id)}
                          className="rounded-lg border border-border bg-secondary px-3 py-1 text-xs text-secondary-foreground transition-colors hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                        >
                          Отправить
                        </button>
                      </div>
                    </div>
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>

        {canLoadMore ? (
          <div className="mt-6 flex justify-center">
            <button
              type="button"
              onClick={() => loadFeed(offset + PAGE_SIZE)}
              className="rounded-xl border border-border bg-secondary px-4 py-2 text-sm text-secondary-foreground transition-colors hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              Показать ещё
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
