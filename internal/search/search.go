package search

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db *store.DB
}

func NewService(db *store.DB) *Service {
	return &Service{db: db}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/search/users", s.SearchUsers)
	r.Get("/search/posts", s.SearchPosts)
}

// ── User search ───────────────────────────────────────────────────────────────

func (s *Service) SearchUsers(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		writeJSON(w, 200, map[string]any{"users": []any{}})
		return
	}

	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url, u.bio,
		       u.is_remote, u.remote_instance,
		       COALESCE(
		           CASE
		               WHEN u.id = $1 THEN 'self'
		               WHEN f.requester_id = $1 AND f.status = 'pending'  THEN 'pending'
		               WHEN f.addressee_id = $1 AND f.status = 'pending'  THEN 'pending_incoming'
		               WHEN f.status = 'accepted' THEN 'accepted'
		               ELSE ''
		           END, ''
		       ) AS friendship_status
		FROM users u
		LEFT JOIN friendships f ON (
			(f.requester_id = $1 AND f.addressee_id = u.id)
		 OR (f.requester_id = u.id AND f.addressee_id = $1)
		)
		WHERE u.deletion_scheduled_at IS NULL
		  AND u.is_suspended = false
		  AND (
		    u.username     ILIKE '%' || $2 || '%'
		    OR u.display_name ILIKE '%' || $2 || '%'
		  )
		ORDER BY
		  CASE WHEN LOWER(u.username) = LOWER($2) THEN 0 ELSE 1 END,
		  (u.is_remote = false) DESC,
		  u.display_name
		LIMIT 30
	`, viewerID, q)
	if err != nil {
		writeError(w, 500, "search error")
		return
	}
	defer rows.Close()

	type Result struct {
		ID             string `json:"id"`
		Username       string `json:"username"`
		DisplayName    string `json:"display_name"`
		AvatarURL      string `json:"avatar_url"`
		Bio            string `json:"bio"`
		IsRemote       bool   `json:"is_remote"`
		RemoteInstance string `json:"remote_instance,omitempty"`
		FriendStatus   string `json:"friendship_status"`
	}

	var results []Result
	for rows.Next() {
		var u Result
		rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio,
			&u.IsRemote, &u.RemoteInstance, &u.FriendStatus)
		results = append(results, u)
	}
	if results == nil { results = []Result{} }
	writeJSON(w, 200, map[string]any{"users": results})
}

// ── Post search ───────────────────────────────────────────────────────────────

func (s *Service) SearchPosts(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		writeJSON(w, 200, map[string]any{"posts": []any{}})
		return
	}

	limit := 20
	offset := 0
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 0 {
		offset = p * limit
	}

	// Only return posts visible to the viewer:
	// - public posts from anyone
	// - friends-only posts from accepted friends
	// - own posts (all visibilities except private... actually include private for own)
	// - never group posts from groups they're not in
	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.created_at,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts c WHERE c.parent_id = p.id AND c.deleted_at IS NULL) AS comment_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked
		FROM posts p
		JOIN users u ON u.id = p.author_id
		WHERE p.parent_id IS NULL
		  AND p.deleted_at IS NULL
		  AND p.is_remote = false
		  AND p.content ILIKE '%' || $2 || '%'
		  AND (
		    -- Own posts
		    p.author_id = $1
		    OR (
		      -- Public posts
		      p.visibility = 'public'
		      AND p.community_group_id IS NULL
		    )
		    OR (
		      -- Friends-only posts from accepted friends
		      p.visibility = 'friends'
		      AND EXISTS(
		        SELECT 1 FROM friendships f
		        WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
		            OR (f.addressee_id = $1 AND f.requester_id = p.author_id))
		        AND f.status = 'accepted'
		      )
		    )
		    OR (
		      -- Group posts for groups the viewer is in
		      p.community_group_id IS NOT NULL
		      AND EXISTS(
		        SELECT 1 FROM community_group_members cgm
		        WHERE cgm.group_id = p.community_group_id AND cgm.user_id = $1
		      )
		    )
		  )
		ORDER BY p.created_at DESC
		LIMIT $3 OFFSET $4
	`, viewerID, q, limit, offset)
	if err != nil {
		writeError(w, 500, "search error")
		return
	}
	defer rows.Close()

	type PostResult struct {
		ID           string `json:"id"`
		AuthorID     string `json:"author_id"`
		Username     string `json:"username"`
		DisplayName  string `json:"display_name"`
		AvatarURL    string `json:"avatar_url"`
		Content      string `json:"content"`
		ImageURL     string `json:"image_url"`
		Visibility   string `json:"visibility"`
		CreatedAt    string `json:"created_at"`
		LikeCount    int    `json:"like_count"`
		CommentCount int    `json:"comment_count"`
		Liked        bool   `json:"liked"`
	}

	var posts []PostResult
	for rows.Next() {
		var p PostResult
		rows.Scan(&p.ID, &p.AuthorID, &p.Username, &p.DisplayName, &p.AvatarURL,
			&p.Content, &p.ImageURL, &p.Visibility, &p.CreatedAt,
			&p.LikeCount, &p.CommentCount, &p.Liked)
		posts = append(posts, p)
	}
	if posts == nil { posts = []PostResult{} }
	writeJSON(w, 200, map[string]any{"posts": posts})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = chi.URLParam
