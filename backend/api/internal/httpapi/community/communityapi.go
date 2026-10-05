package communityapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"backend/internal/auth"
)

type API struct {
	authStore *auth.Store
	db        *pgxpool.Pool
}

func New(authStore *auth.Store, db *pgxpool.Pool) *API {
	return &API{authStore: authStore, db: db}
}

func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireAuth)

		r.Get("/community/feed", a.handleListFeed)
		r.Post("/community/posts", a.handleCreatePost)
		r.Get("/community/posts/{id}/comments", a.handleListComments)
		r.Post("/community/posts/{id}/comments", a.handleCreateComment)
		r.Post("/community/reactions", a.handleToggleReaction)
	})
}

// --------------------
// Auth middleware
// --------------------

type ctxKey string

const ctxUserKey ctxKey = "auth_user"

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.authStore.Authenticate(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !auth.IsPro(user.Plan) {
			writeErr(w, http.StatusForbidden, "plan_required")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userFromCtx(r *http.Request) (auth.User, bool) {
	v := r.Context().Value(ctxUserKey)
	u, ok := v.(auth.User)
	return u, ok
}

// --------------------
// DTOs
// --------------------

type UserView struct {
	ID        string `json:"id"`
	PublicID  int64  `json:"publicId"`
	Name      string `json:"displayName"`
	AvatarURL string `json:"avatarUrl"`
	Plan      string `json:"plan"`
}

type ReactionSummary struct {
	Emoji   string `json:"emoji"`
	Count   int    `json:"count"`
	Reacted bool   `json:"reacted"`
}

type FeedPost struct {
	ID           string            `json:"id"`
	Content      string            `json:"content"`
	CreatedAt    string            `json:"createdAt"`
	CommentCount int               `json:"commentCount"`
	Reactions    []ReactionSummary `json:"reactions"`
	Author       UserView          `json:"author"`
}

type CommentItem struct {
	ID        string            `json:"id"`
	PostID    string            `json:"postId"`
	ParentID  *string           `json:"parentId,omitempty"`
	Content   string            `json:"content"`
	CreatedAt string            `json:"createdAt"`
	Reactions []ReactionSummary `json:"reactions"`
	Author    UserView          `json:"author"`
}

// --------------------
// Handlers
// --------------------

type listFeedResp struct {
	Items []FeedPost `json:"items"`
	Total int        `json:"total"`
}

func (a *API) handleListFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit := clampInt(parseInt(r.URL.Query().Get("limit"), 20), 1, 50)
	offset := clampInt(parseInt(r.URL.Query().Get("offset"), 0), 0, 5000)
	sortKey := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))

	orderBy := "p.created_at DESC"
	if sortKey == "top" {
		orderBy = "(coalesce(rc.cnt,0) + coalesce(cc.cnt,0)) DESC, p.created_at DESC"
	}

	rows, err := a.db.Query(ctx, `
		SELECT
			p.id::text,
			p.content,
			p.created_at,
			u.id::text,
			coalesce(u.public_id, 0),
			coalesce(u.display_name, 'User'),
			coalesce(u.avatar_url, ''),
			coalesce(u.plan, 'Free'),
			coalesce(cc.cnt, 0)
		FROM community_posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN (
			SELECT post_id, count(*) cnt
			FROM community_comments
			GROUP BY post_id
		) cc ON cc.post_id = p.id
		LEFT JOIN (
			SELECT post_id, count(*) cnt
			FROM community_reactions
			WHERE post_id IS NOT NULL
			GROUP BY post_id
		) rc ON rc.post_id = p.id
		ORDER BY `+orderBy+`
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "feed_fetch_failed")
		return
	}
	defer rows.Close()

	items := make([]FeedPost, 0, limit)
	postIDs := make([]string, 0, limit)

	for rows.Next() {
		var it FeedPost
		var createdAt time.Time
		if err := rows.Scan(
			&it.ID,
			&it.Content,
			&createdAt,
			&it.Author.ID,
			&it.Author.PublicID,
			&it.Author.Name,
			&it.Author.AvatarURL,
			&it.Author.Plan,
			&it.CommentCount,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "feed_scan_failed")
			return
		}
		it.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, it)
		postIDs = append(postIDs, it.ID)
	}

	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "feed_scan_failed")
		return
	}

	reactionMap, err := a.fetchPostReactions(ctx, user.ID, postIDs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction_fetch_failed")
		return
	}
	for i := range items {
		items[i].Reactions = reactionMap[items[i].ID]
	}

	var total int
	if err := a.db.QueryRow(ctx, `SELECT count(*) FROM community_posts`).Scan(&total); err != nil {
		total = len(items)
	}

	writeJSON(w, http.StatusOK, listFeedResp{Items: items, Total: total})
}

type createPostReq struct {
	Content string `json:"content"`
}

type createPostResp struct {
	Post FeedPost `json:"post"`
}

func (a *API) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createPostReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if len(content) == 0 || len(content) > 2000 {
		writeErr(w, http.StatusBadRequest, "invalid_content")
		return
	}

	var postID string
	var createdAt time.Time
	var author UserView
	if err := a.db.QueryRow(ctx, `
		INSERT INTO community_posts (user_id, content)
		VALUES ($1, $2)
		RETURNING id::text, created_at
	`, user.ID, content).Scan(&postID, &createdAt); err != nil {
		writeErr(w, http.StatusInternalServerError, "post_create_failed")
		return
	}

	if err := a.db.QueryRow(ctx, `
		SELECT id::text, coalesce(public_id, 0), coalesce(display_name, 'User'),
		       coalesce(avatar_url, ''), coalesce(plan, 'Free')
		FROM users WHERE id = $1
	`, user.ID).Scan(&author.ID, &author.PublicID, &author.Name, &author.AvatarURL, &author.Plan); err != nil {
		writeErr(w, http.StatusInternalServerError, "user_fetch_failed")
		return
	}

	post := FeedPost{
		ID:           postID,
		Content:      content,
		CreatedAt:    createdAt.Format(time.RFC3339),
		CommentCount: 0,
		Reactions:    []ReactionSummary{},
		Author:       author,
	}

	writeJSON(w, http.StatusOK, createPostResp{Post: post})
}

func (a *API) handleListComments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	postID := strings.TrimSpace(chi.URLParam(r, "id"))
	if postID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_post")
		return
	}

	rows, err := a.db.Query(ctx, `
		SELECT
			c.id::text,
			c.post_id::text,
			c.parent_id::text,
			c.content,
			c.created_at,
			u.id::text,
			coalesce(u.public_id, 0),
			coalesce(u.display_name, 'User'),
			coalesce(u.avatar_url, ''),
			coalesce(u.plan, 'Free')
		FROM community_comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.post_id = $1
		ORDER BY c.created_at ASC
	`, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "comment_fetch_failed")
		return
	}
	defer rows.Close()

	items := make([]CommentItem, 0, 32)
	commentIDs := make([]string, 0, 32)

	for rows.Next() {
		var it CommentItem
		var createdAt time.Time
		var parentID *string
		if err := rows.Scan(
			&it.ID,
			&it.PostID,
			&parentID,
			&it.Content,
			&createdAt,
			&it.Author.ID,
			&it.Author.PublicID,
			&it.Author.Name,
			&it.Author.AvatarURL,
			&it.Author.Plan,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "comment_scan_failed")
			return
		}
		it.ParentID = parentID
		it.CreatedAt = createdAt.Format(time.RFC3339)
		items = append(items, it)
		commentIDs = append(commentIDs, it.ID)
	}

	reactionMap, err := a.fetchCommentReactions(ctx, user.ID, commentIDs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction_fetch_failed")
		return
	}
	for i := range items {
		items[i].Reactions = reactionMap[items[i].ID]
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createCommentReq struct {
	Content  string  `json:"content"`
	ParentID *string `json:"parentId"`
}

func (a *API) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	postID := strings.TrimSpace(chi.URLParam(r, "id"))
	if postID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_post")
		return
	}

	var req createCommentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if len(content) == 0 || len(content) > 2000 {
		writeErr(w, http.StatusBadRequest, "invalid_content")
		return
	}

	if req.ParentID != nil {
		parent := strings.TrimSpace(*req.ParentID)
		if parent == "" {
			req.ParentID = nil
		} else {
			var exists bool
			if err := a.db.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM community_comments WHERE id = $1 AND post_id = $2
				)
			`, parent, postID).Scan(&exists); err != nil || !exists {
				writeErr(w, http.StatusBadRequest, "invalid_parent")
				return
			}
		}
	}

	var commentID string
	var createdAt time.Time
	if err := a.db.QueryRow(ctx, `
		INSERT INTO community_comments (post_id, parent_id, user_id, content)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, created_at
	`, postID, req.ParentID, user.ID, content).Scan(&commentID, &createdAt); err != nil {
		writeErr(w, http.StatusInternalServerError, "comment_create_failed")
		return
	}

	var author UserView
	if err := a.db.QueryRow(ctx, `
		SELECT id::text, coalesce(public_id, 0), coalesce(display_name, 'User'),
		       coalesce(avatar_url, ''), coalesce(plan, 'Free')
		FROM users WHERE id = $1
	`, user.ID).Scan(&author.ID, &author.PublicID, &author.Name, &author.AvatarURL, &author.Plan); err != nil {
		writeErr(w, http.StatusInternalServerError, "user_fetch_failed")
		return
	}

	item := CommentItem{
		ID:        commentID,
		PostID:    postID,
		ParentID:  req.ParentID,
		Content:   content,
		CreatedAt: createdAt.Format(time.RFC3339),
		Reactions: []ReactionSummary{},
		Author:    author,
	}

	writeJSON(w, http.StatusOK, map[string]any{"comment": item})
}

type toggleReactionReq struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Emoji      string `json:"emoji"`
}

type toggleReactionResp struct {
	TargetType string            `json:"targetType"`
	TargetID   string            `json:"targetId"`
	Reactions  []ReactionSummary `json:"reactions"`
}

func (a *API) handleToggleReaction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req toggleReactionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body")
		return
	}

	targetType := strings.ToLower(strings.TrimSpace(req.TargetType))
	targetID := strings.TrimSpace(req.TargetID)
	emoji := strings.TrimSpace(req.Emoji)
	if targetID == "" || emoji == "" {
		writeErr(w, http.StatusBadRequest, "invalid_target")
		return
	}

	if targetType == "post" {
		tag, err := a.db.Exec(ctx, `
			INSERT INTO community_reactions (post_id, user_id, emoji)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, targetID, user.ID, emoji)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "reaction_failed")
			return
		}
		if tag.RowsAffected() == 0 {
			_, _ = a.db.Exec(ctx, `
				DELETE FROM community_reactions
				WHERE post_id = $1 AND user_id = $2 AND emoji = $3
			`, targetID, user.ID, emoji)
		}
		res, err := a.fetchPostReactions(ctx, user.ID, []string{targetID})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "reaction_fetch_failed")
			return
		}
		writeJSON(w, http.StatusOK, toggleReactionResp{TargetType: targetType, TargetID: targetID, Reactions: res[targetID]})
		return
	}

	if targetType == "comment" {
		tag, err := a.db.Exec(ctx, `
			INSERT INTO community_reactions (comment_id, user_id, emoji)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, targetID, user.ID, emoji)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "reaction_failed")
			return
		}
		if tag.RowsAffected() == 0 {
			_, _ = a.db.Exec(ctx, `
				DELETE FROM community_reactions
				WHERE comment_id = $1 AND user_id = $2 AND emoji = $3
			`, targetID, user.ID, emoji)
		}
		res, err := a.fetchCommentReactions(ctx, user.ID, []string{targetID})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "reaction_fetch_failed")
			return
		}
		writeJSON(w, http.StatusOK, toggleReactionResp{TargetType: targetType, TargetID: targetID, Reactions: res[targetID]})
		return
	}

	writeErr(w, http.StatusBadRequest, "invalid_target")
}

// --------------------
// Helpers
// --------------------

func (a *API) fetchPostReactions(ctx context.Context, userID string, postIDs []string) (map[string][]ReactionSummary, error) {
	out := make(map[string][]ReactionSummary)
	if len(postIDs) == 0 {
		return out, nil
	}

	rows, err := a.db.Query(ctx, `
		SELECT post_id::text, emoji, count(*)::int AS cnt,
		       bool_or(user_id = $1) AS reacted
		FROM community_reactions
		WHERE post_id = ANY($2::uuid[])
		GROUP BY post_id, emoji
	`, userID, postIDs)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var postID string
		var sum ReactionSummary
		if err := rows.Scan(&postID, &sum.Emoji, &sum.Count, &sum.Reacted); err != nil {
			return out, err
		}
		out[postID] = append(out[postID], sum)
	}

	return out, nil
}

func (a *API) fetchCommentReactions(ctx context.Context, userID string, commentIDs []string) (map[string][]ReactionSummary, error) {
	out := make(map[string][]ReactionSummary)
	if len(commentIDs) == 0 {
		return out, nil
	}

	rows, err := a.db.Query(ctx, `
		SELECT comment_id::text, emoji, count(*)::int AS cnt,
		       bool_or(user_id = $1) AS reacted
		FROM community_reactions
		WHERE comment_id = ANY($2::uuid[])
		GROUP BY comment_id, emoji
	`, userID, commentIDs)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var commentID string
		var sum ReactionSummary
		if err := rows.Scan(&commentID, &sum.Emoji, &sum.Count, &sum.Reacted); err != nil {
			return out, err
		}
		out[commentID] = append(out[commentID], sum)
	}

	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code})
}

func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	v, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return v
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
