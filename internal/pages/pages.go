package pages

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db    *store.DB
	notif *notifications.Service
}

func NewService(db *store.DB, notif *notifications.Service) *Service {
	return &Service{db: db, notif: notif}
}

var slugRe = regexp.MustCompile(`[^a-z0-9_]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = slugRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// ── Routes ────────────────────────────────────────────────────────────────────

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/pages",                        s.ListPages)
	r.Post("/pages",                       s.CreatePage)
	r.Get("/pages/mine",                   s.MyPages)
	r.Get("/pages/{slug}",                 s.GetPage)
	r.Patch("/pages/{slug}",               s.UpdatePage)
	r.Delete("/pages/{slug}",              s.DeletePage)
	r.Post("/pages/{slug}/subscribe",      s.Subscribe)
	r.Delete("/pages/{slug}/subscribe",    s.Unsubscribe)
	r.Get("/pages/{slug}/feed",            s.GetFeed)
	r.Post("/pages/{slug}/posts",          s.CreatePost)
	r.Get("/pages/{slug}/analytics",       s.GetAnalytics)
}

// ── Types ─────────────────────────────────────────────────────────────────────

type Page struct {
	ID              string `json:"id"`
	Slug            string `json:"slug"`
	DisplayName     string `json:"display_name"`
	Bio             string `json:"bio"`
	AvatarURL       string `json:"avatar_url"`
	CoverURL        string `json:"cover_url"`
	CoverPosition   string `json:"cover_position"`
	PageType        string `json:"page_type"`
	OwnerID         string `json:"owner_id"`
	Privacy         string `json:"privacy"`
	SubscriberCount int    `json:"subscriber_count"`
	PostCount       int    `json:"post_count"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	IsSubscribed    bool   `json:"is_subscribed"`
	IsOwner         bool   `json:"is_owner"`
}

type PagePost struct {
	ID           string `json:"id"`
	AuthorID     string `json:"author_id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	AvatarURL    string `json:"avatar_url"`
	Content      string `json:"content"`
	ImageURL     string `json:"image_url"`
	LikeCount    int    `json:"like_count"`
	CommentCount int    `json:"comment_count"`
	Liked        bool   `json:"liked"`
	CreatedAt    string `json:"created_at"`
}

// ── List / Discover ───────────────────────────────────────────────────────────

func (s *Service) ListPages(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 20
	offset := 0
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 0 {
		offset = p * limit
	}

	if q != "" {
		rows, err := s.db.Query(`
			SELECT p.id, p.slug, p.display_name, p.bio, p.avatar_url, p.cover_url,
			       p.cover_position, p.page_type, p.owner_id, p.privacy,
			       p.subscriber_count, p.post_count, p.created_at, p.updated_at,
			       EXISTS(SELECT 1 FROM page_subscribers ps WHERE ps.page_id = p.id AND ps.user_id = $1),
			       (p.owner_id = $1)
			FROM pages p
			WHERE p.privacy = 'public'
			  AND (p.display_name ILIKE $4 OR p.bio ILIKE $4 OR p.slug ILIKE $4)
			ORDER BY p.subscriber_count DESC
			LIMIT $2 OFFSET $3
		`, userID, limit, offset, "%"+q+"%")
		if err != nil {
			writeError(w, 500, "db error"); return
		}
		defer rows.Close()
		pages := scanPages(rows)
		writeJSON(w, 200, map[string]any{"pages": pages})
		return
	}

	// Discover: subscribed pages first, then popular
	rows, err := s.db.Query(`
		SELECT p.id, p.slug, p.display_name, p.bio, p.avatar_url, p.cover_url,
		       p.cover_position, p.page_type, p.owner_id, p.privacy,
		       p.subscriber_count, p.post_count, p.created_at, p.updated_at,
		       EXISTS(SELECT 1 FROM page_subscribers ps WHERE ps.page_id = p.id AND ps.user_id = $1),
		       (p.owner_id = $1)
		FROM pages p
		WHERE p.privacy = 'public'
		ORDER BY
		  EXISTS(SELECT 1 FROM page_subscribers ps WHERE ps.page_id = p.id AND ps.user_id = $1) DESC,
		  p.subscriber_count DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()
	pages := scanPages(rows)
	writeJSON(w, 200, map[string]any{"pages": pages})
}

func (s *Service) MyPages(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	rows, err := s.db.Query(`
		SELECT p.id, p.slug, p.display_name, p.bio, p.avatar_url, p.cover_url,
		       p.cover_position, p.page_type, p.owner_id, p.privacy,
		       p.subscriber_count, p.post_count, p.created_at, p.updated_at,
		       true, true
		FROM pages p
		WHERE p.owner_id = $1
		ORDER BY p.created_at DESC
	`, userID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()
	pages := scanPages(rows)
	writeJSON(w, 200, map[string]any{"pages": pages})
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

func (s *Service) GetPage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var p Page
	err := s.db.QueryRow(`
		SELECT p.id, p.slug, p.display_name, p.bio, p.avatar_url, p.cover_url,
		       p.cover_position, p.page_type, p.owner_id, p.privacy,
		       p.subscriber_count, p.post_count, p.created_at, p.updated_at,
		       EXISTS(SELECT 1 FROM page_subscribers ps WHERE ps.page_id = p.id AND ps.user_id = $1),
		       (p.owner_id = $1)
		FROM pages p
		WHERE p.slug = $2
	`, userID, slug).Scan(
		&p.ID, &p.Slug, &p.DisplayName, &p.Bio, &p.AvatarURL, &p.CoverURL,
		&p.CoverPosition, &p.PageType, &p.OwnerID, &p.Privacy,
		&p.SubscriberCount, &p.PostCount, &p.CreatedAt, &p.UpdatedAt,
		&p.IsSubscribed, &p.IsOwner,
	)
	if err != nil {
		writeError(w, 404, "page not found"); return
	}
	if p.Privacy == "private" && !p.IsOwner {
		writeError(w, 403, "this page is private"); return
	}
	writeJSON(w, 200, map[string]any{"page": p})
}

func (s *Service) CreatePage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	var req struct {
		DisplayName string `json:"display_name"`
		Bio         string `json:"bio"`
		PageType    string `json:"page_type"`
		Privacy     string `json:"privacy"`
		AvatarURL   string `json:"avatar_url"`
		CoverURL    string `json:"cover_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DisplayName) == "" {
		writeError(w, 400, "display_name required"); return
	}
	if req.Privacy != "private" {
		req.Privacy = "public"
	}
	validTypes := map[string]bool{"band": true, "business": true, "organization": true, "creator": true, "": true}
	if !validTypes[req.PageType] {
		writeError(w, 400, "invalid page_type"); return
	}

	baseSlug := slugify(req.DisplayName)
	if baseSlug == "" {
		writeError(w, 400, "display_name produces empty slug"); return
	}
	slug := baseSlug
	for i := 2; i <= 99; i++ {
		var exists bool
		s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pages WHERE slug = $1)`, slug).Scan(&exists)
		if !exists {
			break
		}
		slug = fmt.Sprintf("%s_%d", baseSlug, i)
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO pages (slug, display_name, bio, avatar_url, cover_url, page_type, owner_id, privacy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id
	`, slug, strings.TrimSpace(req.DisplayName), req.Bio, req.AvatarURL, req.CoverURL,
		req.PageType, userID, req.Privacy).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not create page"); return
	}
	writeJSON(w, 201, map[string]string{"id": id, "slug": slug})
}

func (s *Service) UpdatePage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	if !s.isOwner(slug, userID) {
		writeError(w, 403, "only the page owner can update it"); return
	}

	var req struct {
		DisplayName   *string `json:"display_name"`
		Bio           *string `json:"bio"`
		PageType      *string `json:"page_type"`
		Privacy       *string `json:"privacy"`
		AvatarURL     *string `json:"avatar_url"`
		CoverURL      *string `json:"cover_url"`
		CoverPosition *string `json:"cover_position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}

	var sets []string
	var args []any
	i := 1
	add := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}
	if req.DisplayName != nil && strings.TrimSpace(*req.DisplayName) != "" {
		add("display_name", strings.TrimSpace(*req.DisplayName))
	}
	if req.Bio != nil           { add("bio", *req.Bio) }
	if req.AvatarURL != nil     { add("avatar_url", *req.AvatarURL) }
	if req.CoverURL != nil      { add("cover_url", *req.CoverURL) }
	if req.CoverPosition != nil { add("cover_position", *req.CoverPosition) }
	if req.PageType != nil {
		validTypes := map[string]bool{"band": true, "business": true, "organization": true, "creator": true, "": true}
		if !validTypes[*req.PageType] {
			writeError(w, 400, "invalid page_type"); return
		}
		add("page_type", *req.PageType)
	}
	if req.Privacy != nil {
		p := *req.Privacy
		if p != "private" { p = "public" }
		add("privacy", p)
	}
	if len(sets) == 0 {
		writeJSON(w, 200, map[string]string{"message": "nothing to update"}); return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, slug)
	s.db.Exec(fmt.Sprintf("UPDATE pages SET %s WHERE slug = $%d", strings.Join(sets, ", "), i), args...)
	writeJSON(w, 200, map[string]string{"message": "updated"})
}

func (s *Service) DeletePage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	role := auth.RoleFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	if !s.isOwner(slug, userID) && role != "admin" {
		writeError(w, 403, "only the page owner can delete it"); return
	}
	s.db.Exec(`DELETE FROM pages WHERE slug = $1`, slug)
	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

// ── Subscribe / Unsubscribe ───────────────────────────────────────────────────

func (s *Service) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var pageID, privacy string
	s.db.QueryRow(`SELECT id, privacy FROM pages WHERE slug = $1`, slug).Scan(&pageID, &privacy)
	if pageID == "" {
		writeError(w, 404, "page not found"); return
	}
	if privacy == "private" {
		writeError(w, 403, "this page is private"); return
	}

	s.db.Exec(`INSERT INTO page_subscribers (page_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, pageID, userID)
	s.db.Exec(`UPDATE pages SET subscriber_count = (SELECT COUNT(*) FROM page_subscribers WHERE page_id = $1) WHERE id = $1`, pageID)
	go s.recordEvent(pageID, "subscribe")
	writeJSON(w, 200, map[string]string{"message": "subscribed"})
}

func (s *Service) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var pageID string
	s.db.QueryRow(`SELECT id FROM pages WHERE slug = $1`, slug).Scan(&pageID)
	if pageID == "" {
		writeError(w, 404, "page not found"); return
	}

	s.db.Exec(`DELETE FROM page_subscribers WHERE page_id = $1 AND user_id = $2`, pageID, userID)
	s.db.Exec(`UPDATE pages SET subscriber_count = (SELECT COUNT(*) FROM page_subscribers WHERE page_id = $1) WHERE id = $1`, pageID)
	go s.recordEvent(pageID, "unsubscribe")
	writeJSON(w, 200, map[string]string{"message": "unsubscribed"})
}

// ── Page Feed ─────────────────────────────────────────────────────────────────

func (s *Service) GetFeed(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")
	limit := 20
	offset := 0
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 0 {
		offset = p * limit
	}

	var pageID, privacy string
	s.db.QueryRow(`SELECT id, privacy FROM pages WHERE slug = $1`, slug).Scan(&pageID, &privacy)
	if pageID == "" {
		writeError(w, 404, "page not found"); return
	}
	if privacy == "private" && !s.isOwner(slug, userID) {
		writeError(w, 403, "this page is private"); return
	}

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url,
		       p.content, p.image_url,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts c WHERE c.parent_id = p.id AND c.deleted_at IS NULL) AS comment_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       p.created_at
		FROM posts p
		JOIN users u ON u.id = p.author_id
		WHERE p.page_id = $2
		  AND p.parent_id IS NULL
		  AND p.deleted_at IS NULL
		ORDER BY p.created_at DESC
		LIMIT $3 OFFSET $4
	`, userID, pageID, limit, offset)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	var posts []PagePost
	for rows.Next() {
		var p PagePost
		rows.Scan(&p.ID, &p.AuthorID, &p.Username, &p.DisplayName, &p.AvatarURL,
			&p.Content, &p.ImageURL, &p.LikeCount, &p.CommentCount, &p.Liked, &p.CreatedAt)
		posts = append(posts, p)
	}
	if posts == nil {
		posts = []PagePost{}
	}
	writeJSON(w, 200, map[string]any{"posts": posts})
}

// ── Post as a Page ────────────────────────────────────────────────────────────

func (s *Service) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	if !s.isOwner(slug, userID) {
		writeError(w, 403, "only the page owner can post"); return
	}

	var pageID string
	s.db.QueryRow(`SELECT id FROM pages WHERE slug = $1`, slug).Scan(&pageID)
	if pageID == "" {
		writeError(w, 404, "page not found"); return
	}

	var req struct {
		Content  string   `json:"content"`
		ImageURL string   `json:"image_url"`
		ImageURLs []string `json:"image_urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}
	if strings.TrimSpace(req.Content) == "" && req.ImageURL == "" && len(req.ImageURLs) == 0 {
		writeError(w, 400, "content required"); return
	}

	// Prefer image_urls array; fall back to single image_url for compat
	imageURL := req.ImageURL
	if len(req.ImageURLs) > 0 {
		imageURL = req.ImageURLs[0]
	}

	var postID string
	err := s.db.QueryRow(`
		INSERT INTO posts (author_id, content, image_url, visibility, page_id)
		VALUES ($1, $2, $3, 'public', $4) RETURNING id
	`, userID, strings.TrimSpace(req.Content), imageURL, pageID).Scan(&postID)
	if err != nil {
		writeError(w, 500, "could not create post"); return
	}

	// Insert additional photos
	for i, url := range req.ImageURLs {
		s.db.Exec(`INSERT INTO post_photos (post_id, url, position) VALUES ($1, $2, $3)`, postID, url, i)
	}

	// Update page post count
	s.db.Exec(`UPDATE pages SET post_count = post_count + 1 WHERE id = $1`, pageID)

	// Notify all subscribers
	go s.notifySubscribers(pageID, userID, postID)

	writeJSON(w, 201, map[string]string{"id": postID})
}

func (s *Service) notifySubscribers(pageID, actorID, postID string) {
	rows, err := s.db.Query(`SELECT user_id FROM page_subscribers WHERE page_id = $1`, pageID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		rows.Scan(&uid)
		if uid == actorID {
			continue // don't notify the poster
		}
		s.notif.Create(uid, actorID, "page_post", postID, pageID)
	}
}

// ── Analytics (AGORA-113) ─────────────────────────────────────────────────────

func (s *Service) recordEvent(pageID, eventType string) {
	s.db.Exec(`INSERT INTO page_analytics_events (page_id, event_type) VALUES ($1, $2)`, pageID, eventType)
}

func (s *Service) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var pageID, ownerID string
	s.db.QueryRow(`SELECT id, owner_id FROM pages WHERE slug = $1`, slug).Scan(&pageID, &ownerID)
	if pageID == "" { writeError(w, 404, "page not found"); return }
	if ownerID != userID {
		writeError(w, 403, "restricted to page owner"); return
	}

	var totalSubs int
	s.db.QueryRow(`SELECT subscriber_count FROM pages WHERE id = $1`, pageID).Scan(&totalSubs)

	growth := func(days int) int {
		var gained, lost int
		s.db.QueryRow(`SELECT COUNT(*) FROM page_analytics_events WHERE page_id=$1 AND event_type='subscribe'   AND created_at > NOW() - ($2 || ' days')::interval`, pageID, fmt.Sprintf("%d", days)).Scan(&gained)
		s.db.QueryRow(`SELECT COUNT(*) FROM page_analytics_events WHERE page_id=$1 AND event_type='unsubscribe' AND created_at > NOW() - ($2 || ' days')::interval`, pageID, fmt.Sprintf("%d", days)).Scan(&lost)
		return gained - lost
	}

	type TopPost struct {
		ID           string `json:"id"`
		Content      string `json:"content"`
		LikeCount    int    `json:"like_count"`
		CommentCount int    `json:"comment_count"`
		CreatedAt    string `json:"created_at"`
	}
	topRows, _ := s.db.Query(`
		SELECT p.id, LEFT(p.content, 120),
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id),
		       (SELECT COUNT(*) FROM posts c WHERE c.parent_id = p.id AND c.deleted_at IS NULL),
		       p.created_at
		FROM posts p
		WHERE p.page_id = $1 AND p.deleted_at IS NULL AND p.parent_id IS NULL
		ORDER BY (SELECT COUNT(*) FROM likes WHERE post_id = p.id) +
		         (SELECT COUNT(*) FROM posts c WHERE c.parent_id = p.id AND c.deleted_at IS NULL) DESC
		LIMIT 5
	`, pageID)
	var topPosts []TopPost
	if topRows != nil {
		defer topRows.Close()
		for topRows.Next() {
			var tp TopPost
			topRows.Scan(&tp.ID, &tp.Content, &tp.LikeCount, &tp.CommentCount, &tp.CreatedAt)
			topPosts = append(topPosts, tp)
		}
	}
	if topPosts == nil { topPosts = []TopPost{} }

	var totalPosts, totalLikes, totalComments int
	s.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE page_id=$1 AND deleted_at IS NULL AND parent_id IS NULL`, pageID).Scan(&totalPosts)
	s.db.QueryRow(`SELECT COUNT(*) FROM likes l JOIN posts p ON p.id = l.post_id WHERE p.page_id=$1`, pageID).Scan(&totalLikes)
	s.db.QueryRow(`SELECT COUNT(*) FROM posts c JOIN posts p ON p.id = c.parent_id WHERE p.page_id=$1 AND c.deleted_at IS NULL`, pageID).Scan(&totalComments)

	writeJSON(w, 200, map[string]any{
		"total_subscribers": totalSubs,
		"subscriber_growth": map[string]int{
			"7d":  growth(7),
			"30d": growth(30),
			"90d": growth(90),
		},
		"total_posts":    totalPosts,
		"total_likes":    totalLikes,
		"total_comments": totalComments,
		"top_posts":      topPosts,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Service) isOwner(slug, userID string) bool {
	var ownerID string
	s.db.QueryRow(`SELECT owner_id FROM pages WHERE slug = $1`, slug).Scan(&ownerID)
	return ownerID == userID
}

func scanPages(rows interface {
	Next() bool
	Scan(...any) error
}) []Page {
	var pages []Page
	for rows.Next() {
		var p Page
		rows.Scan(
			&p.ID, &p.Slug, &p.DisplayName, &p.Bio, &p.AvatarURL, &p.CoverURL,
			&p.CoverPosition, &p.PageType, &p.OwnerID, &p.Privacy,
			&p.SubscriberCount, &p.PostCount, &p.CreatedAt, &p.UpdatedAt,
			&p.IsSubscribed, &p.IsOwner,
		)
		pages = append(pages, p)
	}
	if pages == nil {
		pages = []Page{}
	}
	return pages
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
