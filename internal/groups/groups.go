package groups

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

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(name)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

// ── Routes ────────────────────────────────────────────────────────────────────

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/groups",                                    s.ListGroups)
	r.Post("/groups",                                   s.CreateGroup)
	r.Get("/groups/{slug}",                             s.GetGroup)
	r.Patch("/groups/{slug}",                           s.UpdateGroup)
	r.Delete("/groups/{slug}",                          s.DeleteGroup)
	r.Get("/groups/{slug}/members",                     s.ListMembers)
	r.Post("/groups/{slug}/join",                       s.Join)
	r.Delete("/groups/{slug}/leave",                    s.Leave)
	r.Patch("/groups/{slug}/members/{userID}/role",     s.SetMemberRole)
	r.Delete("/groups/{slug}/members/{userID}",         s.RemoveMember)
	r.Get("/groups/{slug}/feed",                        s.GetFeed)
	r.Post("/groups/{slug}/posts",                      s.CreatePost)
}

// ── Group CRUD ────────────────────────────────────────────────────────────────

type Group struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	CoverURL    string `json:"cover_url"`
	AvatarURL   string `json:"avatar_url"`
	Privacy     string `json:"privacy"`
	CreatedBy   string `json:"created_by"`
	MemberCount int    `json:"member_count"`
	PostCount   int    `json:"post_count"`
	CreatedAt   string `json:"created_at"`
	MemberRole  string `json:"member_role,omitempty"`
	IsMember    bool   `json:"is_member"`
}

func (s *Service) ListGroups(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	q := r.URL.Query().Get("q")
	filter := r.URL.Query().Get("filter")
	limit := 20
	offset := 0
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 0 {
		offset = p * limit
	}

	var query string
	var args []any

	switch filter {
	case "mine":
		query = `
			SELECT g.id, g.name, g.slug, g.description, g.cover_url, g.avatar_url,
			       g.privacy, g.created_by, g.member_count, g.post_count, g.created_at,
			       m.role, true
			FROM community_groups g
			JOIN community_group_members m ON m.group_id = g.id AND m.user_id = $1
			WHERE m.role = 'owner'
			ORDER BY g.created_at DESC LIMIT $2 OFFSET $3`
		args = []any{userID, limit, offset}
	case "joined":
		query = `
			SELECT g.id, g.name, g.slug, g.description, g.cover_url, g.avatar_url,
			       g.privacy, g.created_by, g.member_count, g.post_count, g.created_at,
			       m.role, true
			FROM community_groups g
			JOIN community_group_members m ON m.group_id = g.id AND m.user_id = $1
			ORDER BY g.created_at DESC LIMIT $2 OFFSET $3`
		args = []any{userID, limit, offset}
	default:
		if q != "" {
			query = `
				SELECT g.id, g.name, g.slug, g.description, g.cover_url, g.avatar_url,
				       g.privacy, g.created_by, g.member_count, g.post_count, g.created_at,
				       COALESCE(m.role,''), (m.user_id IS NOT NULL)
				FROM community_groups g
				LEFT JOIN community_group_members m ON m.group_id = g.id AND m.user_id = $1
				WHERE (g.name ILIKE $4 OR g.description ILIKE $4)
				  AND (g.privacy = 'public' OR m.user_id IS NOT NULL)
				ORDER BY g.member_count DESC LIMIT $2 OFFSET $3`
			args = []any{userID, limit, offset, "%" + q + "%"}
		} else {
			query = `
				SELECT g.id, g.name, g.slug, g.description, g.cover_url, g.avatar_url,
				       g.privacy, g.created_by, g.member_count, g.post_count, g.created_at,
				       COALESCE(m.role,''), (m.user_id IS NOT NULL)
				FROM community_groups g
				LEFT JOIN community_group_members m ON m.group_id = g.id AND m.user_id = $1
				WHERE g.privacy = 'public' OR m.user_id IS NOT NULL
				ORDER BY g.member_count DESC LIMIT $2 OFFSET $3`
			args = []any{userID, limit, offset}
		}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		rows.Scan(&g.ID, &g.Name, &g.Slug, &g.Description, &g.CoverURL, &g.AvatarURL,
			&g.Privacy, &g.CreatedBy, &g.MemberCount, &g.PostCount, &g.CreatedAt,
			&g.MemberRole, &g.IsMember)
		groups = append(groups, g)
	}
	if groups == nil {
		groups = []Group{}
	}
	writeJSON(w, 200, map[string]any{"groups": groups})
}

func (s *Service) GetGroup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var g Group
	err := s.db.QueryRow(`
		SELECT g.id, g.name, g.slug, g.description, g.cover_url, g.avatar_url,
		       g.privacy, g.created_by, g.member_count, g.post_count, g.created_at,
		       COALESCE(m.role,''), (m.user_id IS NOT NULL)
		FROM community_groups g
		LEFT JOIN community_group_members m ON m.group_id = g.id AND m.user_id = $1
		WHERE g.slug = $2
	`, userID, slug).Scan(&g.ID, &g.Name, &g.Slug, &g.Description, &g.CoverURL, &g.AvatarURL,
		&g.Privacy, &g.CreatedBy, &g.MemberCount, &g.PostCount, &g.CreatedAt,
		&g.MemberRole, &g.IsMember)
	if err != nil {
		writeError(w, 404, "group not found"); return
	}
	if g.Privacy == "private" && !g.IsMember {
		writeError(w, 403, "this group is private"); return
	}
	writeJSON(w, 200, map[string]any{"group": g})
}

func (s *Service) CreateGroup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Privacy     string `json:"privacy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, 400, "name required"); return
	}
	if req.Privacy != "private" {
		req.Privacy = "public"
	}

	baseSlug := slugify(req.Name)
	slug := baseSlug
	for i := 2; i <= 99; i++ {
		var exists bool
		s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM community_groups WHERE slug = $1)`, slug).Scan(&exists)
		if !exists {
			break
		}
		slug = fmt.Sprintf("%s-%d", baseSlug, i)
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO community_groups (name, slug, description, privacy, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, strings.TrimSpace(req.Name), slug, req.Description, req.Privacy, userID).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not create group"); return
	}
	s.db.Exec(`INSERT INTO community_group_members (group_id, user_id, role) VALUES ($1, $2, 'owner')`, id, userID)
	writeJSON(w, 201, map[string]string{"id": id, "slug": slug})
}

func (s *Service) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	if !s.hasRole(slug, userID, "owner", "mod") {
		writeError(w, 403, "forbidden"); return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Privacy     *string `json:"privacy"`
		CoverURL    *string `json:"cover_url"`
		AvatarURL   *string `json:"avatar_url"`
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
	if req.Name != nil        { add("name", strings.TrimSpace(*req.Name)) }
	if req.Description != nil { add("description", *req.Description) }
	if req.CoverURL != nil    { add("cover_url", *req.CoverURL) }
	if req.AvatarURL != nil   { add("avatar_url", *req.AvatarURL) }
	if req.Privacy != nil && s.hasRole(slug, userID, "owner") {
		p := *req.Privacy
		if p != "private" { p = "public" }
		add("privacy", p)
	}
	if len(sets) == 0 {
		writeJSON(w, 200, map[string]string{"message": "nothing to update"}); return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, slug)
	s.db.Exec(fmt.Sprintf("UPDATE community_groups SET %s WHERE slug = $%d", strings.Join(sets, ", "), i), args...)
	writeJSON(w, 200, map[string]string{"message": "updated"})
}

func (s *Service) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	role := auth.RoleFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	if !s.hasRole(slug, userID, "owner") && role != "admin" {
		writeError(w, 403, "only the group owner can delete it"); return
	}
	s.db.Exec(`DELETE FROM community_groups WHERE slug = $1`, slug)
	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

// ── Membership ────────────────────────────────────────────────────────────────

type Member struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Role        string `json:"role"`
	JoinedAt    string `json:"joined_at"`
}

func (s *Service) ListMembers(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var groupID, privacy string
	s.db.QueryRow(`SELECT id, privacy FROM community_groups WHERE slug = $1`, slug).Scan(&groupID, &privacy)
	if groupID == "" {
		writeError(w, 404, "group not found"); return
	}
	if privacy == "private" && !s.isMember(groupID, userID) {
		writeError(w, 403, "forbidden"); return
	}

	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url, m.role, m.joined_at
		FROM community_group_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.group_id = $1
		ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'mod' THEN 1 ELSE 2 END, m.joined_at ASC
	`, groupID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.AvatarURL, &m.Role, &m.JoinedAt)
		members = append(members, m)
	}
	if members == nil { members = []Member{} }
	writeJSON(w, 200, map[string]any{"members": members})
}

func (s *Service) Join(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var groupID, privacy string
	s.db.QueryRow(`SELECT id, privacy FROM community_groups WHERE slug = $1`, slug).Scan(&groupID, &privacy)
	if groupID == "" {
		writeError(w, 404, "group not found"); return
	}
	if privacy == "private" {
		writeError(w, 403, "this group is invite-only"); return
	}
	s.db.Exec(`INSERT INTO community_group_members (group_id, user_id, role) VALUES ($1, $2, 'member') ON CONFLICT DO NOTHING`, groupID, userID)
	s.db.Exec(`UPDATE community_groups SET member_count = (SELECT COUNT(*) FROM community_group_members WHERE group_id = $1) WHERE id = $1`, groupID)
	writeJSON(w, 200, map[string]string{"message": "joined"})
}

func (s *Service) Leave(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var groupID string
	s.db.QueryRow(`SELECT id FROM community_groups WHERE slug = $1`, slug).Scan(&groupID)
	if groupID == "" {
		writeError(w, 404, "group not found"); return
	}
	var role string
	s.db.QueryRow(`SELECT role FROM community_group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID).Scan(&role)
	if role == "owner" {
		writeError(w, 400, "owner cannot leave — transfer ownership or delete the group"); return
	}
	s.db.Exec(`DELETE FROM community_group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID)
	s.db.Exec(`UPDATE community_groups SET member_count = (SELECT COUNT(*) FROM community_group_members WHERE group_id = $1) WHERE id = $1`, groupID)
	writeJSON(w, 200, map[string]string{"message": "left"})
}

func (s *Service) SetMemberRole(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")
	targetID := chi.URLParam(r, "userID")

	if !s.hasRole(slug, userID, "owner") {
		writeError(w, 403, "only owners can change roles"); return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Role != "mod" && req.Role != "member") {
		writeError(w, 400, "role must be 'mod' or 'member'"); return
	}
	var groupID string
	s.db.QueryRow(`SELECT id FROM community_groups WHERE slug = $1`, slug).Scan(&groupID)
	s.db.Exec(`UPDATE community_group_members SET role = $1 WHERE group_id = $2 AND user_id = $3`, req.Role, groupID, targetID)
	writeJSON(w, 200, map[string]string{"message": "role updated"})
}

func (s *Service) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")
	targetID := chi.URLParam(r, "userID")

	var groupID string
	s.db.QueryRow(`SELECT id FROM community_groups WHERE slug = $1`, slug).Scan(&groupID)
	if groupID == "" {
		writeError(w, 404, "group not found"); return
	}
	if !s.hasRole(slug, userID, "owner", "mod") {
		writeError(w, 403, "forbidden"); return
	}
	var targetRole string
	s.db.QueryRow(`SELECT role FROM community_group_members WHERE group_id = $1 AND user_id = $2`, groupID, targetID).Scan(&targetRole)
	callerRole := s.memberRole(groupID, userID)
	if targetRole == "owner" || (targetRole == "mod" && callerRole != "owner") {
		writeError(w, 403, "insufficient permissions"); return
	}
	s.db.Exec(`DELETE FROM community_group_members WHERE group_id = $1 AND user_id = $2`, groupID, targetID)
	s.db.Exec(`UPDATE community_groups SET member_count = (SELECT COUNT(*) FROM community_group_members WHERE group_id = $1) WHERE id = $1`, groupID)
	writeJSON(w, 200, map[string]string{"message": "removed"})
}

// ── Group Feed & Posts ────────────────────────────────────────────────────────

type GroupPost struct {
	ID           string `json:"id"`
	AuthorID     string `json:"author_id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	AvatarURL    string `json:"avatar_url"`
	AuthorRole   string `json:"author_role"`
	Content      string `json:"content"`
	ImageURL     string `json:"image_url"`
	LikeCount    int    `json:"like_count"`
	CommentCount int    `json:"comment_count"`
	Liked        bool   `json:"liked"`
	CreatedAt    string `json:"created_at"`
}

func (s *Service) GetFeed(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")
	limit := 20
	offset := 0
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 0 {
		offset = p * limit
	}

	var groupID, privacy string
	s.db.QueryRow(`SELECT id, privacy FROM community_groups WHERE slug = $1`, slug).Scan(&groupID, &privacy)
	if groupID == "" {
		writeError(w, 404, "group not found"); return
	}
	if privacy == "private" && !s.isMember(groupID, userID) {
		writeError(w, 403, "forbidden"); return
	}

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.avatar_url,
		       COALESCE(m.role,'') AS author_role,
		       p.content, p.image_url,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts c WHERE c.parent_id = p.id AND c.deleted_at IS NULL) AS comment_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       p.created_at
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN community_group_members m ON m.group_id = $2 AND m.user_id = p.author_id
		WHERE p.community_group_id = $2
		  AND p.parent_id IS NULL
		  AND p.deleted_at IS NULL
		ORDER BY p.created_at DESC
		LIMIT $3 OFFSET $4
	`, userID, groupID, limit, offset)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	var posts []GroupPost
	for rows.Next() {
		var p GroupPost
		rows.Scan(&p.ID, &p.AuthorID, &p.Username, &p.DisplayName, &p.AvatarURL,
			&p.AuthorRole, &p.Content, &p.ImageURL,
			&p.LikeCount, &p.CommentCount, &p.Liked, &p.CreatedAt)
		posts = append(posts, p)
	}
	if posts == nil { posts = []GroupPost{} }
	writeJSON(w, 200, map[string]any{"posts": posts})
}

func (s *Service) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	slug := chi.URLParam(r, "slug")

	var groupID, privacy string
	s.db.QueryRow(`SELECT id, privacy FROM community_groups WHERE slug = $1`, slug).Scan(&groupID, &privacy)
	if groupID == "" {
		writeError(w, 404, "group not found"); return
	}
	if !s.isMember(groupID, userID) {
		writeError(w, 403, "you must be a member to post"); return
	}

	var req struct {
		Content  string `json:"content"`
		ImageURL string `json:"image_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Content == "" && req.ImageURL == "") {
		writeError(w, 400, "content required"); return
	}

	var postID string
	err := s.db.QueryRow(`
		INSERT INTO posts (author_id, content, image_url, visibility, community_group_id)
		VALUES ($1, $2, $3, 'public', $4) RETURNING id
	`, userID, req.Content, req.ImageURL, groupID).Scan(&postID)
	if err != nil {
		writeError(w, 500, "could not create post"); return
	}
	s.db.Exec(`UPDATE community_groups SET post_count = post_count + 1 WHERE id = $1`, groupID)
	writeJSON(w, 201, map[string]string{"id": postID})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Service) isMember(groupID, userID string) bool {
	var exists bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM community_group_members WHERE group_id = $1 AND user_id = $2)`, groupID, userID).Scan(&exists)
	return exists
}

func (s *Service) memberRole(groupID, userID string) string {
	var role string
	s.db.QueryRow(`SELECT role FROM community_group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID).Scan(&role)
	return role
}

func (s *Service) hasRole(slug, userID string, roles ...string) bool {
	var groupID string
	s.db.QueryRow(`SELECT id FROM community_groups WHERE slug = $1`, slug).Scan(&groupID)
	if groupID == "" { return false }
	role := s.memberRole(groupID, userID)
	for _, r := range roles {
		if role == r { return true }
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
