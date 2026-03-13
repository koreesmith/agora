package feed

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/media"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

type Service struct {
	db    *store.DB
	notif *notifications.Service
	media *media.Service
}

func NewService(db *store.DB, notif *notifications.Service, media *media.Service) *Service {
	return &Service{db: db, notif: notif, media: media}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/feed",                               s.GetFeed)
	r.Post("/posts",                             s.CreatePost)
	r.Get("/posts/{id}",                         s.GetPost)
	r.Delete("/posts/{id}",                      s.DeletePost)
	r.Patch("/posts/{id}",                       s.EditPost)
	r.Post("/posts/{id}/like",                   s.LikePost)
	r.Delete("/posts/{id}/like",                 s.UnlikePost)
	r.Post("/posts/{id}/repost",                 s.Repost)
	r.Get("/posts/{id}/comments",                s.GetComments)
	r.Post("/posts/{id}/comments",               s.CreateComment)
	r.Delete("/posts/{id}/comments/{commentID}", s.DeleteComment)
	r.Patch("/posts/{id}/comments/{commentID}",  s.EditComment)
	r.Get("/users/{username}/posts",             s.GetUserPosts)
}

// ── Feed ──────────────────────────────────────────────────────────────────────

func (s *Service) GetFeed(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	limit, offset := pageParams(r)
	listID := r.URL.Query().Get("list_id")

	var rows *sql.Rows
	var err error

	if listID != "" {
		// List feed: posts from members of a specific friend list owned by this user
		rows, err = s.db.Query(`
			SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url,
			       p.content, p.image_url, p.visibility, p.community_group_id,
			       cg.name, cg.slug,
			       p.repost_of_id, p.is_remote, p.remote_instance,
			       p.created_at, p.updated_at, p.edited_at,
			       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
			       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
			       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
			       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
			       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
			       rp_u.username, rp_u.display_name, rp_u.avatar_url,
			       rp.content, rp.image_url, rp.created_at
			FROM posts p
			JOIN users u ON u.id = p.author_id
			LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
			LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
			LEFT JOIN community_groups cg ON cg.id = p.community_group_id
			WHERE p.parent_id IS NULL
			  AND p.deleted_at IS NULL
			  AND p.visibility != 'private'
			  AND p.author_id IN (
			    SELECT friend_id FROM friend_group_members
			    WHERE group_id = $4
			    AND group_id IN (SELECT id FROM friend_groups WHERE user_id = $1)
			  )
			  AND (
			    p.visibility = 'public'
			    OR p.visibility = 'friends'
			    OR (p.visibility = 'group' AND p.group_id = $4)
			  )
			  AND (
			    p.community_group_id IS NULL
			    OR EXISTS (
			      SELECT 1 FROM community_group_members cgm
			      WHERE cgm.group_id = p.community_group_id AND cgm.user_id = $1
			    )
			  )
			ORDER BY p.created_at DESC
			LIMIT $2 OFFSET $3
		`, userID, limit, offset, listID)
	} else {
		rows, err = s.db.Query(`
			SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url,
			       p.content, p.image_url, p.visibility, p.community_group_id,
			       cg.name, cg.slug,
			       p.repost_of_id, p.is_remote, p.remote_instance,
			       p.created_at, p.updated_at, p.edited_at,
			       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
			       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
			       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
			       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
			       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
			       rp_u.username, rp_u.display_name, rp_u.avatar_url,
			       rp.content, rp.image_url, rp.created_at
			FROM posts p
			JOIN users u ON u.id = p.author_id
			LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
			LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
			LEFT JOIN community_groups cg ON cg.id = p.community_group_id
			WHERE p.parent_id IS NULL
			  AND p.deleted_at IS NULL
			  AND p.visibility != 'private'
			  AND (
			    p.author_id = $1
			    OR EXISTS(
			      SELECT 1 FROM friendships f
			      WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
			          OR (f.addressee_id = $1 AND f.requester_id = p.author_id))
			      AND f.status = 'accepted'
			    )
			  )
			  AND (
			    p.community_group_id IS NULL
			    OR EXISTS (
			      SELECT 1 FROM community_group_members cgm
			      WHERE cgm.group_id = p.community_group_id AND cgm.user_id = $1
			    )
			  )
			ORDER BY p.created_at DESC
			LIMIT $2 OFFSET $3
		`, userID, limit, offset)
	}
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	writeJSON(w, 200, map[string]any{"posts": posts})
}

func (s *Service) GetUserPosts(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	username := chi.URLParam(r, "username")
	limit, offset := pageParams(r)

	var authorID string
	var profilePrivate bool
	s.db.QueryRow(`SELECT id, profile_private FROM users WHERE username = $1`, username).Scan(&authorID, &profilePrivate)
	if authorID == "" {
		writeError(w, 404, "user not found")
		return
	}

	// Check access
	isFriend := false
	if viewerID != authorID {
		s.db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM friendships
				WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
				AND status = 'accepted'
			)
		`, viewerID, authorID).Scan(&isFriend)
	}

	visFilter := `p.visibility = 'public'`
	if viewerID == authorID {
		visFilter = `true`
	} else if isFriend {
		visFilter = `p.visibility IN ('public', 'friends')`
	}

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id,
			       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance,
		       p.created_at, p.updated_at, p.edited_at,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::timestamptz
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		WHERE p.author_id = $2 AND p.parent_id IS NULL AND p.deleted_at IS NULL
		  AND `+visFilter+`
		ORDER BY p.created_at DESC LIMIT $3 OFFSET $4
	`, viewerID, authorID, limit, offset)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	writeJSON(w, 200, map[string]any{"posts": posts})
}

// ── Post CRUD ─────────────────────────────────────────────────────────────────

func (s *Service) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Content    string `json:"content"`
		ImageURL   string `json:"image_url"`
		Visibility string `json:"visibility"`
		GroupID    string `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if req.Content == "" && req.ImageURL == "" {
		writeError(w, 400, "post must have content or image")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "friends"
	}

	var groupID *string
	if req.GroupID != "" && req.Visibility == "group" {
		groupID = &req.GroupID
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO posts (author_id, content, image_url, visibility, community_group_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, userID, req.Content, req.ImageURL, req.Visibility, groupID).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not create post")
		return
	}

	go s.notifyMentions(req.Content, userID, id)
	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Service) GetPost(w http.ResponseWriter, r *http.Request) {
	id       := chi.URLParam(r, "id")
	viewerID := auth.UserIDFromCtx(r.Context())

	// First fetch the post's core fields to check access before running the full query
	var authorID, visibility string
	var communityGroupID *string
	var parentID *string
	err := s.db.QueryRow(`
		SELECT author_id, visibility, community_group_id, parent_id
		FROM posts WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&authorID, &visibility, &communityGroupID, &parentID)
	if err != nil {
		writeError(w, 404, "post not found")
		return
	}

	// If this is a comment (has parent_id), redirect caller to the parent post
	if parentID != nil {
		writeJSON(w, 200, map[string]any{"redirect_to_post": *parentID})
		return
	}

	// Access control
	if authorID != viewerID {
		switch visibility {
		case "private":
			writeJSON(w, 403, map[string]string{"error": "access_denied", "reason": "private"})
			return
		case "friends", "group":
			// Check friendship
			var isFriend bool
			s.db.QueryRow(`
				SELECT EXISTS(
					SELECT 1 FROM friendships
					WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
					AND status = 'accepted'
				)
			`, viewerID, authorID).Scan(&isFriend)
			if !isFriend {
				writeJSON(w, 403, map[string]string{"error": "access_denied", "reason": "not_friends"})
				return
			}
		}

		// Group post — additionally check group membership
		if communityGroupID != nil {
			var isMember bool
			s.db.QueryRow(`
				SELECT EXISTS(
					SELECT 1 FROM community_group_members
					WHERE group_id = $1 AND user_id = $2
				)
			`, *communityGroupID, viewerID).Scan(&isMember)
			if !isMember {
				// Get group name and slug for a helpful message
				var groupName, groupSlug string
				s.db.QueryRow(`SELECT name, slug FROM community_groups WHERE id = $1`, *communityGroupID).
					Scan(&groupName, &groupSlug)
				writeJSON(w, 403, map[string]any{
					"error":      "access_denied",
					"reason":     "not_group_member",
					"group_name": groupName,
					"group_slug": groupSlug,
				})
				return
			}
		}
	}

	// Access granted — run the full query
	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id,
			   cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance,
		       p.created_at, p.updated_at, p.edited_at,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $2) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $2) AS reposted,
		       rp_u.username, rp_u.display_name, rp_u.avatar_url,
		       rp.content, rp.image_url, rp.created_at
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN posts rp   ON rp.id = p.repost_of_id
		LEFT JOIN users rp_u ON rp_u.id = rp.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		WHERE p.id = $1 AND p.deleted_at IS NULL
	`, id, viewerID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	if len(posts) == 0 {
		writeError(w, 404, "post not found")
		return
	}
	writeJSON(w, 200, map[string]any{"post": posts[0]})
}

func (s *Service) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	role := auth.RoleFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var authorID string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&authorID)
	if authorID == "" {
		writeError(w, 404, "post not found")
		return
	}
	if authorID != userID && role != "admin" && role != "moderator" {
		writeError(w, 403, "forbidden")
		return
	}

	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, id)
	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

// ── Likes ─────────────────────────────────────────────────────────────────────

func (s *Service) LikePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	s.db.Exec(`INSERT INTO likes (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, postID)

	var authorID string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1`, postID).Scan(&authorID)
	if authorID != "" && authorID != userID {
		go s.notif.Create(authorID, userID, "post_like", postID, "")
	}
	writeJSON(w, 200, map[string]string{"message": "liked"})
}

func (s *Service) UnlikePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, userID, postID)
	writeJSON(w, 200, map[string]string{"message": "unliked"})
}

// ── Reposts ───────────────────────────────────────────────────────────────────

func (s *Service) Repost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	repostOfID := chi.URLParam(r, "id")

	var req struct {
		Content    string `json:"content"`
		Visibility string `json:"visibility"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Visibility == "" {
		req.Visibility = "friends"
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, repost_of_id)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, userID, req.Content, req.Visibility, repostOfID).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not repost")
		return
	}

	var authorID string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1`, repostOfID).Scan(&authorID)
	if authorID != "" && authorID != userID {
		go s.notif.Create(authorID, userID, "post_repost", repostOfID, "")
	}

	writeJSON(w, 201, map[string]string{"id": id})
}

// ── Comments ──────────────────────────────────────────────────────────────────

func (s *Service) GetComments(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	viewerID := auth.UserIDFromCtx(r.Context())

	type Comment struct {
		ID          string    `json:"id"`
		AuthorID    string    `json:"author_id"`
		Username    string    `json:"username"`
		DisplayName string    `json:"display_name"`
		AvatarURL   string    `json:"avatar_url"`
		Content     string    `json:"content"`
		ImageURL    string    `json:"image_url"`
		CreatedAt   string    `json:"created_at"`
		EditedAt    *string   `json:"edited_at,omitempty"`
		LikeCount   int       `json:"like_count"`
		Liked       bool      `json:"liked"`
		Replies     []Comment `json:"replies"`
	}

	scanComment := func(rows interface {
		Scan(...any) error
	}) Comment {
		var c Comment
		rows.Scan(&c.ID, &c.AuthorID, &c.Username, &c.DisplayName, &c.AvatarURL,
			&c.Content, &c.ImageURL, &c.CreatedAt, &c.EditedAt, &c.LikeCount, &c.Liked)
		c.Replies = []Comment{}
		return c
	}

	const commentSQL = `
		SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url,
		       p.content, p.image_url, p.created_at, p.edited_at,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked
		FROM posts p
		JOIN users u ON u.id = p.author_id
		WHERE p.parent_id = $2 AND p.deleted_at IS NULL
		ORDER BY p.created_at ASC
	`

	// Top-level comments
	rows, err := s.db.Query(commentSQL, viewerID, postID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		comments = append(comments, scanComment(rows))
	}
	rows.Close()

	if comments == nil {
		comments = []Comment{}
	}

	// Replies for each top-level comment
	for i, c := range comments {
		rrows, err := s.db.Query(commentSQL, viewerID, c.ID)
		if err != nil { continue }
		for rrows.Next() {
			comments[i].Replies = append(comments[i].Replies, scanComment(rrows))
		}
		rrows.Close()
		if comments[i].Replies == nil {
			comments[i].Replies = []Comment{}
		}
	}

	writeJSON(w, 200, map[string]any{"comments": comments})
}

func (s *Service) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var req struct {
		Content   string `json:"content"`
		ImageURL  string `json:"image_url"`
		ReplyToID string `json:"reply_to_id"` // if set, this is a reply to a comment
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Content == "" && req.ImageURL == "") {
		writeError(w, 400, "content required")
		return
	}

	// Validate the post exists and get its author/visibility
	var visibility, postAuthorID string
	s.db.QueryRow(`SELECT visibility, author_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, postID).
		Scan(&visibility, &postAuthorID)
	if postAuthorID == "" {
		writeError(w, 404, "post not found")
		return
	}

	// Determine parent: either a reply to a comment, or a top-level comment on the post
	parentID := postID
	var replyToAuthorID string
	if req.ReplyToID != "" {
		// Validate the comment being replied to is a direct child of this post (depth = 1)
		var commentParentID string
		s.db.QueryRow(`SELECT parent_id, author_id FROM posts WHERE id = $1 AND deleted_at IS NULL`,
			req.ReplyToID).Scan(&commentParentID, &replyToAuthorID)
		if commentParentID == "" {
			writeError(w, 404, "comment not found")
			return
		}
		if commentParentID != postID {
			writeError(w, 400, "cannot reply to a reply")
			return
		}
		parentID = req.ReplyToID
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO posts (author_id, content, image_url, visibility, parent_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, userID, req.Content, req.ImageURL, visibility, parentID).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not post comment")
		return
	}

	// Notify post author (if not self)
	if postAuthorID != userID {
		go s.notif.Create(postAuthorID, userID, "post_comment", postID, "")
	}
	// Notify comment author when someone replies to their comment (if different from post author and self)
	if replyToAuthorID != "" && replyToAuthorID != userID && replyToAuthorID != postAuthorID {
		go s.notif.Create(replyToAuthorID, userID, "comment_reply", postID, "")
	}
	go s.notifyMentions(req.Content, userID, postID)

	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Service) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	role := auth.RoleFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	commentID := chi.URLParam(r, "commentID")

	var commentAuthor, parentAuthor string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1 AND parent_id = $2 AND deleted_at IS NULL`, commentID, postID).Scan(&commentAuthor)
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1`, postID).Scan(&parentAuthor)

	if commentAuthor == "" {
		writeError(w, 404, "comment not found")
		return
	}
	if commentAuthor != userID && parentAuthor != userID && role != "admin" && role != "moderator" {
		writeError(w, 403, "forbidden")
		return
	}

	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, commentID)
	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

// ── Edit ──────────────────────────────────────────────────────────────────────

func (s *Service) EditPost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var authorID string
	var repostOfID *string
	s.db.QueryRow(`SELECT author_id, repost_of_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&authorID, &repostOfID)
	if authorID == "" {
		writeError(w, 404, "post not found"); return
	}
	if authorID != userID {
		writeError(w, 403, "forbidden"); return
	}
	if repostOfID != nil {
		writeError(w, 400, "reposts cannot be edited"); return
	}

	var req struct {
		Content  *string `json:"content"`
		ImageURL *string `json:"image_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}
	if req.Content == nil && req.ImageURL == nil {
		writeError(w, 400, "nothing to update"); return
	}

	var sets []string
	var args []any
	i := 1
	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d", i)); args = append(args, *req.Content); i++
	}
	if req.ImageURL != nil {
		sets = append(sets, fmt.Sprintf("image_url = $%d", i)); args = append(args, *req.ImageURL); i++
	}
	sets = append(sets, "edited_at = NOW()")
	args = append(args, id)
	s.db.Exec(fmt.Sprintf("UPDATE posts SET %s WHERE id = $%d", strings.Join(sets, ", "), i), args...)
	writeJSON(w, 200, map[string]string{"message": "updated"})
}

func (s *Service) EditComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	commentID := chi.URLParam(r, "commentID")

	var authorID string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1 AND parent_id = $2 AND deleted_at IS NULL`,
		commentID, postID).Scan(&authorID)
	if authorID == "" {
		writeError(w, 404, "comment not found"); return
	}
	if authorID != userID {
		writeError(w, 403, "forbidden"); return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		writeError(w, 400, "content required"); return
	}

	s.db.Exec(`UPDATE posts SET content = $1, edited_at = NOW() WHERE id = $2`, req.Content, commentID)
	writeJSON(w, 200, map[string]string{"message": "updated"})
}

// ── Scan helpers ──────────────────────────────────────────────────────────────

type Post struct {
	ID             string  `json:"id"`
	AuthorID       string  `json:"author_id"`
	AuthorUsername string  `json:"author_username"`
	AuthorName     string  `json:"author_display_name"`
	AuthorAvatar   string  `json:"author_avatar_url"`
	Content        string  `json:"content"`
	ImageURL       string  `json:"image_url"`
	Visibility     string  `json:"visibility"`
	GroupID        *string `json:"group_id"`
	GroupName      *string `json:"group_name,omitempty"`
	GroupSlug      *string `json:"group_slug,omitempty"`
	RepostOfID     *string `json:"repost_of_id"`
	IsRemote       bool    `json:"is_remote"`
	RemoteInstance string  `json:"remote_instance,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	EditedAt       *string `json:"edited_at,omitempty"`
	LikeCount      int     `json:"like_count"`
	CommentCount   int     `json:"comment_count"`
	RepostCount    int     `json:"repost_count"`
	Liked          bool    `json:"liked"`
	Reposted       bool    `json:"reposted"`
	// Repost source
	RepostAuthorUsername *string `json:"repost_author_username,omitempty"`
	RepostAuthorName     *string `json:"repost_author_display_name,omitempty"`
	RepostAuthorAvatar   *string `json:"repost_author_avatar_url,omitempty"`
	RepostContent        *string `json:"repost_content,omitempty"`
	RepostImageURL       *string `json:"repost_image_url,omitempty"`
	RepostCreatedAt      *string `json:"repost_created_at,omitempty"`
}

func scanPosts(rows interface {
	Next() bool
	Scan(...any) error
}) []Post {
	var posts []Post
	for rows.Next() {
		var p Post
		rows.Scan(
			&p.ID, &p.AuthorID, &p.AuthorUsername, &p.AuthorName, &p.AuthorAvatar,
			&p.Content, &p.ImageURL, &p.Visibility, &p.GroupID, &p.GroupName, &p.GroupSlug,
			&p.RepostOfID, &p.IsRemote, &p.RemoteInstance,
			&p.CreatedAt, &p.UpdatedAt, &p.EditedAt,
			&p.LikeCount, &p.CommentCount, &p.RepostCount,
			&p.Liked, &p.Reposted,
			&p.RepostAuthorUsername, &p.RepostAuthorName, &p.RepostAuthorAvatar,
			&p.RepostContent, &p.RepostImageURL, &p.RepostCreatedAt,
		)
		posts = append(posts, p)
	}
	if posts == nil { return []Post{} }
	return posts
}

func pageParams(r *http.Request) (limit, offset int) {
	limit = 20
	offset = 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── Mention helpers ───────────────────────────────────────────────────────────

func (s *Service) notifyMentions(content, authorID, postID string) {
	matches := mentionRe.FindAllStringSubmatch(content, -1)
	seen := map[string]bool{}
	for _, m := range matches {
		username := strings.ToLower(m[1])
		if seen[username] { continue }
		seen[username] = true

		var userID string
		s.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = $1 AND deletion_scheduled_at IS NULL`, username).Scan(&userID)
		if userID == "" || userID == authorID { continue }

		s.notif.Create(userID, authorID, "post_mention", postID, "")
	}
}
