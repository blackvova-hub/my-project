export type CommunityUser = {
  id: string;
  publicId: number;
  displayName: string;
  avatarUrl: string;
  plan: string;
};

export type ReactionSummary = {
  emoji: string;
  count: number;
  reacted: boolean;
};

export type CommunityPost = {
  id: string;
  content: string;
  createdAt: string;
  commentCount: number;
  reactions: ReactionSummary[];
  author: CommunityUser;
};

export type CommunityComment = {
  id: string;
  postId: string;
  parentId?: string | null;
  content: string;
  createdAt: string;
  reactions: ReactionSummary[];
  author: CommunityUser;
};

export type FeedResponse = {
  items: CommunityPost[];
  total: number;
};
