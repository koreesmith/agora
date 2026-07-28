package search

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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
	r.Get("/search/pages", s.SearchPages)
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
		       u.is_remote, u.remote_instance, COALESCE(u.emojis::text,'{}'),
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
		  AND u.id != $1
		  AND NOT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=u.id) OR (blocker_id=u.id AND blocked_id=$1))
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
		ID             string          `json:"id"`
		Username       string          `json:"username"`
		DisplayName    string          `json:"display_name"`
		AvatarURL      string          `json:"avatar_url"`
		Bio            string          `json:"bio"`
		IsRemote       bool            `json:"is_remote"`
		RemoteInstance string          `json:"remote_instance,omitempty"`
		Emojis         json.RawMessage `json:"emojis,omitempty"`
		FriendStatus   string          `json:"friendship_status"`
	}

	var results []Result
	for rows.Next() {
		var u Result
		var emojis string
		rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio,
			&u.IsRemote, &u.RemoteInstance, &emojis, &u.FriendStatus)
		u.Emojis = json.RawMessage(emojis)
		results = append(results, u)
	}
	if results == nil { results = []Result{} }
	writeJSON(w, 200, map[string]any{"users": results})
}

// ── Post search ───────────────────────────────────────────────────────────────

// hashtagFromQuery reports whether q names a single hashtag (a bare "#tag"
// query, whitespace-trimmed) rather than a free-text keyword search — the
// two are matched completely differently below (AGORA-214): a hashtag is a
// case-insensitive exact match against the real post_hashtags table
// (AGORA-213), never a content substring, since an ILIKE on "#games" would
// both miss "#Games" and false-positive on a word like "average".
func hashtagFromQuery(q string) (tag string, ok bool) {
	q = strings.TrimSpace(q)
	if !strings.HasPrefix(q, "#") {
		return "", false
	}
	tag = strings.ToLower(strings.TrimPrefix(q, "#"))
	return tag, tag != ""
}

// postVisibilityClause is the same "posts visible to the viewer" scoping
// SearchPosts always enforced, factored out only because it's now shared
// verbatim by both the hashtag and keyword query branches below — own
// posts, public posts, friends-only posts from accepted friends, or group
// posts for groups the viewer is in.
const postVisibilityClause = `
	(
	  p.author_id = $1
	  OR (p.visibility = 'public' AND p.community_group_id IS NULL)
	  OR (
	    p.visibility = 'friends'
	    AND EXISTS(
	      SELECT 1 FROM friendships f
	      WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
	          OR (f.addressee_id = $1 AND f.requester_id = p.author_id))
	      AND f.status = 'accepted'
	    )
	  )
	  OR (
	    p.community_group_id IS NOT NULL
	    AND EXISTS(
	      SELECT 1 FROM community_group_members cgm
	      WHERE cgm.group_id = p.community_group_id AND cgm.user_id = $1
	    )
	  )
	)`

const postResultColumns = `
	p.id, p.author_id, u.username, u.display_name, u.avatar_url,
	p.content, p.image_url, p.visibility, p.created_at,
	p.is_remote, p.remote_instance,
	(SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
	(SELECT COUNT(*) FROM posts c WHERE c.parent_id = p.id AND c.deleted_at IS NULL) AS comment_count,
	EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
	COALESCE(u.emojis::text,'{}'), COALESCE(p.emojis::text,'{}')`

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

	// AGORA-214: an already-ingested Fediverse/Bluesky post is public by
	// construction (both ingestion paths only ever pull in public content),
	// so dropping the old "is_remote = false" restriction here doesn't need
	// any new visibility rule — the existing scoping already covers it.
	var rows *sql.Rows
	var err error
	if tag, isHashtag := hashtagFromQuery(q); isHashtag {
		rows, err = s.db.Query(`
			SELECT `+postResultColumns+`
			FROM posts p
			JOIN users u ON u.id = p.author_id
			JOIN post_hashtags ph ON ph.post_id = p.id AND ph.tag = $2
			WHERE p.parent_id IS NULL
			  AND p.deleted_at IS NULL
			  AND `+postVisibilityClause+`
			ORDER BY p.created_at DESC
			LIMIT $3 OFFSET $4
		`, viewerID, tag, limit, offset)
	} else {
		rows, err = s.db.Query(`
			SELECT `+postResultColumns+`
			FROM posts p
			JOIN users u ON u.id = p.author_id
			WHERE p.parent_id IS NULL
			  AND p.deleted_at IS NULL
			  AND p.content ILIKE '%' || $2 || '%'
			  AND `+postVisibilityClause+`
			ORDER BY p.created_at DESC
			LIMIT $3 OFFSET $4
		`, viewerID, q, limit, offset)
	}
	if err != nil {
		writeError(w, 500, "search error")
		return
	}
	defer rows.Close()

	type PostResult struct {
		ID             string          `json:"id"`
		AuthorID       string          `json:"author_id"`
		Username       string          `json:"username"`
		DisplayName    string          `json:"display_name"`
		AvatarURL      string          `json:"avatar_url"`
		Content        string          `json:"content"`
		ImageURL       string          `json:"image_url"`
		Visibility     string          `json:"visibility"`
		CreatedAt      string          `json:"created_at"`
		IsRemote       bool            `json:"is_remote"`
		RemoteInstance string          `json:"remote_instance,omitempty"`
		LikeCount      int             `json:"like_count"`
		CommentCount   int             `json:"comment_count"`
		Liked          bool            `json:"liked"`
		AuthorEmojis   json.RawMessage `json:"author_emojis,omitempty"`
		ContentEmojis  json.RawMessage `json:"content_emojis,omitempty"`
	}

	var posts []PostResult
	for rows.Next() {
		var p PostResult
		var authorEmojis, contentEmojis string
		rows.Scan(&p.ID, &p.AuthorID, &p.Username, &p.DisplayName, &p.AvatarURL,
			&p.Content, &p.ImageURL, &p.Visibility, &p.CreatedAt,
			&p.IsRemote, &p.RemoteInstance,
			&p.LikeCount, &p.CommentCount, &p.Liked,
			&authorEmojis, &contentEmojis)
		p.AuthorEmojis = json.RawMessage(authorEmojis)
		p.ContentEmojis = json.RawMessage(contentEmojis)
		posts = append(posts, p)
	}
	if posts == nil { posts = []PostResult{} }
	writeJSON(w, 200, map[string]any{"posts": posts})
}

// ── Page search (AGORA-127) ───────────────────────────────────────────────────

func (s *Service) SearchPages(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		writeJSON(w, 200, map[string]any{"pages": []any{}})
		return
	}

	limit := 20
	offset := 0
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 0 {
		offset = p * limit
	}

	rows, err := s.db.Query(`
		SELECT p.id, p.slug, p.display_name, p.bio, p.avatar_url,
		       p.page_type, p.subscriber_count, p.post_count, p.is_verified,
		       EXISTS(SELECT 1 FROM page_subscribers ps WHERE ps.page_id = p.id AND ps.user_id = $1) AS is_subscribed
		FROM pages p
		WHERE p.privacy = 'public'
		  AND (p.display_name ILIKE '%' || $2 || '%'
		       OR p.bio ILIKE '%' || $2 || '%'
		       OR p.slug ILIKE '%' || $2 || '%')
		ORDER BY
		  CASE WHEN LOWER(p.slug) = LOWER($2) THEN 0
		       WHEN LOWER(p.display_name) = LOWER($2) THEN 1
		       ELSE 2 END,
		  p.subscriber_count DESC
		LIMIT $3 OFFSET $4
	`, viewerID, q, limit, offset)
	if err != nil {
		writeError(w, 500, "search error")
		return
	}
	defer rows.Close()

	type PageResult struct {
		ID              string `json:"id"`
		Slug            string `json:"slug"`
		DisplayName     string `json:"display_name"`
		Bio             string `json:"bio"`
		AvatarURL       string `json:"avatar_url"`
		PageType        string `json:"page_type"`
		SubscriberCount int    `json:"subscriber_count"`
		PostCount       int    `json:"post_count"`
		IsVerified      bool   `json:"is_verified"`
		IsSubscribed    bool   `json:"is_subscribed"`
	}

	var pages []PageResult
	for rows.Next() {
		var p PageResult
		rows.Scan(&p.ID, &p.Slug, &p.DisplayName, &p.Bio, &p.AvatarURL,
			&p.PageType, &p.SubscriberCount, &p.PostCount, &p.IsVerified, &p.IsSubscribed)
		pages = append(pages, p)
	}
	if pages == nil {
		pages = []PageResult{}
	}
	writeJSON(w, 200, map[string]any{"pages": pages})
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
