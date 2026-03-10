package feed

import (
	"net/http"
	"strconv"
	"time"

	"github.com/agora-social/agora/internal/media"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/pkg/middleware"
	"github.com/agora-social/agora/pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	db       *sqlx.DB
	redis    *redis.Client
	notifSvc *notifications.Service
	mediaSvc *media.Service
}

func NewService(db *sqlx.DB, redis *redis.Client, notifSvc *notifications.Service, mediaSvc *media.Service) *Service {
	return &Service{db: db, redis: redis, notifSvc: notifSvc, mediaSvc: mediaSvc}
}

type Post struct {
	ID           string         `db:"id" json:"id"`
	AuthorID     string         `db:"author_id" json:"author_id"`
	Content      string         `db:"content" json:"content"`
	MediaURLs    pq.StringArray `db:"media_urls" json:"media_urls"`
	Visibility   string         `db:"visibility" json:"visibility"`
	GroupID      *string        `db:"group_id" json:"group_id,omitempty"`
	RepostOf     *string        `db:"repost_of" json:"repost_of,omitempty"`
	LikeCount    int            `db:"like_count" json:"like_count"`
	CommentCount int            `db:"comment_count" json:"comment_count"`
	RepostCount  int            `db:"repost_count" json:"repost_count"`
	Liked        bool           `db:"liked" json:"liked"`
	Reposted     bool           `db:"reposted" json:"reposted"`
	IsRemote     bool           `db:"is_remote" json:"is_remote"`
	HomeInstance string         `db:"home_instance" json:"home_instance,omitempty"`
	CreatedAt    time.Time      `db:"created_at" json:"created_at"`
	// Joined author info
	AuthorUsername    string `db:"author_username" json:"author_username"`
	AuthorDisplayName string `db:"author_display_name" json:"author_display_name"`
	AuthorAvatarURL   string `db:"author_avatar_url" json:"author_avatar_url"`
}

func RegisterRoutes(r chi.Router, svc *Service) {
	r.Get("/feed", svc.GetFeed)
	r.Post("/posts", svc.CreatePost)
	r.Get("/posts/{postID}", svc.GetPost)
	r.Delete("/posts/{postID}", svc.DeletePost)
	r.Post("/posts/{postID}/like", svc.LikePost)
	r.Delete("/posts/{postID}/like", svc.UnlikePost)
	r.Post("/posts/{postID}/repost", svc.Repost)
	r.Get("/posts/{postID}/comments", svc.GetComments)
	r.Post("/posts/{postID}/comments", svc.CreateComment)
	r.Delete("/posts/{postID}/comments/{commentID}", svc.DeleteComment)
	r.Post("/posts/{postID}/comments/{commentID}/like", svc.LikeComment)
	r.Delete("/posts/{postID}/comments/{commentID}/like", svc.UnlikeComment)
	r.Post("/posts/{postID}/media", svc.UploadPostMedia)
	r.Get("/users/{userID}/posts", svc.GetUserPosts)
}

func (s *Service) GetFeed(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		limit, _ = strconv.Atoi(l)
		if limit > 50 {
			limit = 50
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		offset, _ = strconv.Atoi(o)
	}

	var posts []Post
	s.db.Select(&posts, `
		SELECT
			p.id, p.author_id, p.content, p.media_urls, p.visibility, p.group_id,
			p.repost_of, p.is_remote, p.home_instance, p.created_at,
			u.username as author_username,
			u.display_name as author_display_name,
			u.avatar_url as author_avatar_url,
			(SELECT COUNT(*) FROM likes WHERE post_id = p.id) as like_count,
			(SELECT COUNT(*) FROM comments WHERE post_id = p.id AND deleted_at IS NULL) as comment_count,
			(SELECT COUNT(*) FROM posts WHERE repost_of = p.id AND deleted_at IS NULL) as repost_count,
			EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) as liked,
			EXISTS(SELECT 1 FROM posts WHERE repost_of = p.id AND author_id = $1 AND deleted_at IS NULL) as reposted
		FROM posts p
		JOIN users u ON p.author_id = u.id
		WHERE p.deleted_at IS NULL
		AND (
			-- Public posts from friends
			(p.visibility = 'public')
			OR
			-- Friends posts from friends
			(p.visibility = 'friends' AND (
				p.author_id = $1
				OR EXISTS(
					SELECT 1 FROM friendships f
					WHERE f.status = 'accepted'
					AND ((f.requester_id = $1 AND f.addressee_id = p.author_id)
					OR (f.requester_id = p.author_id AND f.addressee_id = $1))
				)
			))
			OR
			-- Group posts where viewer is in the group
			(p.visibility = 'group' AND (
				p.author_id = $1
				OR EXISTS(
					SELECT 1 FROM friend_group_members fgm
					JOIN friend_groups fg ON fgm.group_id = fg.id
					WHERE fg.user_id = p.author_id
					AND fg.id = p.group_id
					AND fgm.friend_id = $1
				)
			))
		)
		ORDER BY p.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)

	if posts == nil {
		posts = []Post{}
	}

	utils.JSON(w, http.StatusOK, map[string]any{
		"posts":  posts,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Service) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	var req struct {
		Content    string   `json:"content"`
		Visibility string   `json:"visibility"`
		GroupID    *string  `json:"group_id"`
		MediaURLs  []string `json:"media_urls"`
	}
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.Content == "" {
		utils.Error(w, http.StatusBadRequest, "content required")
		return
	}

	var maxLen int
	s.db.Get(&maxLen, `SELECT value::int FROM instance_settings WHERE key = 'max_post_length'`)
	if maxLen == 0 {
		maxLen = 5000
	}
	if len(req.Content) > maxLen {
		utils.Error(w, http.StatusBadRequest, "post too long")
		return
	}

	if req.Visibility == "" {
		req.Visibility = "friends"
	}

	mediaURLs := pq.StringArray(req.MediaURLs)
	if mediaURLs == nil {
		mediaURLs = pq.StringArray{}
	}

	var postID string
	err := s.db.QueryRow(`
		INSERT INTO posts (author_id, content, media_urls, visibility, group_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, userID, req.Content, mediaURLs, req.Visibility, req.GroupID).Scan(&postID)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "failed to create post")
		return
	}

	utils.JSON(w, http.StatusCreated, map[string]string{"id": postID, "message": "post created"})
}

func (s *Service) GetPost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")

	var post Post
	err := s.db.Get(&post, `
		SELECT
			p.id, p.author_id, p.content, p.media_urls, p.visibility, p.group_id,
			p.repost_of, p.is_remote, p.home_instance, p.created_at,
			u.username as author_username,
			u.display_name as author_display_name,
			u.avatar_url as author_avatar_url,
			(SELECT COUNT(*) FROM likes WHERE post_id = p.id) as like_count,
			(SELECT COUNT(*) FROM comments WHERE post_id = p.id AND deleted_at IS NULL) as comment_count,
			(SELECT COUNT(*) FROM posts WHERE repost_of = p.id AND deleted_at IS NULL) as repost_count,
			EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) as liked,
			EXISTS(SELECT 1 FROM posts WHERE repost_of = p.id AND author_id = $1 AND deleted_at IS NULL) as reposted
		FROM posts p
		JOIN users u ON p.author_id = u.id
		WHERE p.id = $2 AND p.deleted_at IS NULL
	`, userID, postID)
	if err != nil {
		utils.Error(w, http.StatusNotFound, "post not found")
		return
	}

	utils.JSON(w, http.StatusOK, post)
}

func (s *Service) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")
	role := middleware.GetUserRole(r.Context())

	query := `UPDATE posts SET deleted_at = NOW() WHERE id = $1 AND author_id = $2`
	if role == "admin" || role == "moderator" {
		query = `UPDATE posts SET deleted_at = NOW() WHERE id = $1`
		s.db.Exec(query, postID)
	} else {
		s.db.Exec(query, postID, userID)
	}

	utils.JSON(w, http.StatusOK, map[string]string{"message": "post deleted"})
}

func (s *Service) LikePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")

	var authorID string
	s.db.Get(&authorID, "SELECT author_id FROM posts WHERE id = $1", postID)

	s.db.Exec(`INSERT INTO likes (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, postID)

	if authorID != userID {
		go s.notifSvc.Create(authorID, userID, "like", "post", postID, "liked your post")
	}

	utils.JSON(w, http.StatusOK, map[string]string{"message": "liked"})
}

func (s *Service) UnlikePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, userID, postID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "unliked"})
}

func (s *Service) Repost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")

	var req struct {
		Content    string `json:"content"`
		Visibility string `json:"visibility"`
	}
	utils.DecodeJSON(r, &req)
	if req.Visibility == "" {
		req.Visibility = "friends"
	}

	var originalAuthorID string
	s.db.Get(&originalAuthorID, "SELECT author_id FROM posts WHERE id = $1", postID)

	var newPostID string
	s.db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, repost_of, media_urls)
		VALUES ($1, $2, $3, $4, '{}')
		RETURNING id
	`, userID, req.Content, req.Visibility, postID).Scan(&newPostID)

	if originalAuthorID != "" && originalAuthorID != userID {
		go s.notifSvc.Create(originalAuthorID, userID, "repost", "post", postID, "reposted your post")
	}

	utils.JSON(w, http.StatusCreated, map[string]string{"id": newPostID, "message": "reposted"})
}

func (s *Service) GetComments(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")

	var comments []struct {
		ID                string         `db:"id" json:"id"`
		PostID            string         `db:"post_id" json:"post_id"`
		AuthorID          string         `db:"author_id" json:"author_id"`
		ParentID          *string        `db:"parent_id" json:"parent_id,omitempty"`
		Content           string         `db:"content" json:"content"`
		MediaURLs         pq.StringArray `db:"media_urls" json:"media_urls"`
		LikeCount         int            `db:"like_count" json:"like_count"`
		Liked             bool           `db:"liked" json:"liked"`
		AuthorUsername    string         `db:"author_username" json:"author_username"`
		AuthorDisplayName string         `db:"author_display_name" json:"author_display_name"`
		AuthorAvatarURL   string         `db:"author_avatar_url" json:"author_avatar_url"`
		CreatedAt         time.Time      `db:"created_at" json:"created_at"`
	}

	s.db.Select(&comments, `
		SELECT
			c.id, c.post_id, c.author_id, c.parent_id, c.content, c.media_urls, c.created_at,
			u.username as author_username,
			u.display_name as author_display_name,
			u.avatar_url as author_avatar_url,
			(SELECT COUNT(*) FROM likes WHERE comment_id = c.id) as like_count,
			EXISTS(SELECT 1 FROM likes WHERE comment_id = c.id AND user_id = $1) as liked
		FROM comments c
		JOIN users u ON c.author_id = u.id
		WHERE c.post_id = $2 AND c.deleted_at IS NULL
		ORDER BY c.created_at ASC
	`, userID, postID)

	if comments == nil {
		comments = []struct {
			ID                string         `db:"id" json:"id"`
			PostID            string         `db:"post_id" json:"post_id"`
			AuthorID          string         `db:"author_id" json:"author_id"`
			ParentID          *string        `db:"parent_id" json:"parent_id,omitempty"`
			Content           string         `db:"content" json:"content"`
			MediaURLs         pq.StringArray `db:"media_urls" json:"media_urls"`
			LikeCount         int            `db:"like_count" json:"like_count"`
			Liked             bool           `db:"liked" json:"liked"`
			AuthorUsername    string         `db:"author_username" json:"author_username"`
			AuthorDisplayName string         `db:"author_display_name" json:"author_display_name"`
			AuthorAvatarURL   string         `db:"author_avatar_url" json:"author_avatar_url"`
			CreatedAt         time.Time      `db:"created_at" json:"created_at"`
		}{}
	}

	utils.JSON(w, http.StatusOK, comments)
}

func (s *Service) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")

	var req struct {
		Content   string   `json:"content"`
		ParentID  *string  `json:"parent_id"`
		MediaURLs []string `json:"media_urls"`
	}
	if err := utils.DecodeJSON(r, &req); err != nil || req.Content == "" {
		utils.Error(w, http.StatusBadRequest, "content required")
		return
	}

	mediaURLs := pq.StringArray(req.MediaURLs)
	if mediaURLs == nil {
		mediaURLs = pq.StringArray{}
	}

	var commentID string
	err := s.db.QueryRow(`
		INSERT INTO comments (post_id, author_id, parent_id, content, media_urls)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, postID, userID, req.ParentID, req.Content, mediaURLs).Scan(&commentID)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "failed to create comment")
		return
	}

	// Notify post author
	var postAuthorID string
	s.db.Get(&postAuthorID, "SELECT author_id FROM posts WHERE id = $1", postID)
	if postAuthorID != userID {
		go s.notifSvc.Create(postAuthorID, userID, "comment", "post", postID, "commented on your post")
	}

	utils.JSON(w, http.StatusCreated, map[string]string{"id": commentID, "message": "comment created"})
}

func (s *Service) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")
	commentID := chi.URLParam(r, "commentID")
	role := middleware.GetUserRole(r.Context())

	// Author can delete their own comment; post owner can delete comments on their post; admins/mods can delete anything
	var postAuthorID string
	s.db.Get(&postAuthorID, "SELECT author_id FROM posts WHERE id = $1", postID)

	if role == "admin" || role == "moderator" || postAuthorID == userID {
		s.db.Exec(`UPDATE comments SET deleted_at = NOW() WHERE id = $1`, commentID)
	} else {
		s.db.Exec(`UPDATE comments SET deleted_at = NOW() WHERE id = $1 AND author_id = $2`, commentID, userID)
	}

	utils.JSON(w, http.StatusOK, map[string]string{"message": "comment deleted"})
}

func (s *Service) LikeComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	commentID := chi.URLParam(r, "commentID")
	s.db.Exec(`INSERT INTO likes (user_id, comment_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, commentID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "liked"})
}

func (s *Service) UnlikeComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	commentID := chi.URLParam(r, "commentID")
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND comment_id = $2`, userID, commentID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "unliked"})
}

func (s *Service) UploadPostMedia(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	url, err := s.mediaSvc.UploadImage(r, "post", userID)
	if err != nil {
		utils.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	utils.JSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Service) GetUserPosts(w http.ResponseWriter, r *http.Request) {
	viewerID := middleware.GetUserID(r.Context())
	targetUserID := chi.URLParam(r, "userID")
	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		limit, _ = strconv.Atoi(l)
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		offset, _ = strconv.Atoi(o)
	}

	// Check if friends
	var isFriend bool
	s.db.Get(&isFriend, `
		SELECT EXISTS(SELECT 1 FROM friendships WHERE status = 'accepted'
		AND ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1)))
	`, viewerID, targetUserID)

	visibilityFilter := `p.visibility = 'public'`
	if isFriend || viewerID == targetUserID {
		visibilityFilter = `p.visibility IN ('public', 'friends')`
	}

	var posts []Post
	query := `
		SELECT
			p.id, p.author_id, p.content, p.media_urls, p.visibility, p.group_id,
			p.repost_of, p.is_remote, p.home_instance, p.created_at,
			u.username as author_username, u.display_name as author_display_name, u.avatar_url as author_avatar_url,
			(SELECT COUNT(*) FROM likes WHERE post_id = p.id) as like_count,
			(SELECT COUNT(*) FROM comments WHERE post_id = p.id AND deleted_at IS NULL) as comment_count,
			(SELECT COUNT(*) FROM posts WHERE repost_of = p.id AND deleted_at IS NULL) as repost_count,
			EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) as liked,
			EXISTS(SELECT 1 FROM posts WHERE repost_of = p.id AND author_id = $1 AND deleted_at IS NULL) as reposted
		FROM posts p
		JOIN users u ON p.author_id = u.id
		WHERE p.author_id = $2 AND p.deleted_at IS NULL AND ` + visibilityFilter + `
		ORDER BY p.created_at DESC
		LIMIT $3 OFFSET $4
	`
	s.db.Select(&posts, query, viewerID, targetUserID, limit, offset)
	if posts == nil {
		posts = []Post{}
	}
	utils.JSON(w, http.StatusOK, map[string]any{"posts": posts, "limit": limit, "offset": offset})
}
