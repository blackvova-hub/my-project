import { http } from "../../../shared/api/http";
import type { CommunityComment, CommunityPost, FeedResponse, ReactionSummary } from "./types";

export async function fetchCommunityFeed(params: {
  limit?: number;
  offset?: number;
  sort?: "new" | "top";
}): Promise<FeedResponse> {
  const qs = new URLSearchParams();
  if (params.limit) qs.set("limit", String(params.limit));
  if (params.offset) qs.set("offset", String(params.offset));
  if (params.sort) qs.set("sort", params.sort);
  return http<FeedResponse>(`/api/community/feed?${qs.toString()}`);
}

export async function createCommunityPost(content: string): Promise<CommunityPost> {
  const res = await http<{ post: CommunityPost }>("/api/community/posts", {
    method: "POST",
    body: JSON.stringify({ content }),
  });
  return res.post;
}

export async function fetchCommunityComments(postId: string): Promise<CommunityComment[]> {
  const res = await http<{ items: CommunityComment[] }>(`/api/community/posts/${postId}/comments`);
  return res.items;
}

export async function createCommunityComment(input: {
  postId: string;
  content: string;
  parentId?: string | null;
}): Promise<CommunityComment> {
  const res = await http<{ comment: CommunityComment }>(`/api/community/posts/${input.postId}/comments`, {
    method: "POST",
    body: JSON.stringify({ content: input.content, parentId: input.parentId ?? null }),
  });
  return res.comment;
}

export async function toggleCommunityReaction(input: {
  targetType: "post" | "comment";
  targetId: string;
  emoji: string;
}): Promise<{ reactions: ReactionSummary[] }> {
  const res = await http<{ reactions: ReactionSummary[] }>("/api/community/reactions", {
    method: "POST",
    body: JSON.stringify(input),
  });
  return res;
}
