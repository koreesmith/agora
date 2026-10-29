// Package hidden implements per-user, per-post timeline hiding (AGORA-309).
//
// A user's home timeline surfaces posts from accounts they follow. Sometimes one
// specific post is unwanted without the author being unwanted, and until now the
// only ways to say so were unfollow, block and report, all three of which are
// about the person rather than the post.
//
// Deliberately a client of one. Hiding does not affect the post for anyone else,
// does not notify the author, and feeds nothing into ranking. Stored server-side
// rather than on the device so it holds across web, mobile and a re-login.
//
// Shaped after internal/blocks, which is the closest existing thing: a small
// table, three routes, and a filter clause every feed query already has a place
// for.
package hidden

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/store"
)

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/hidden-posts", s.List)
	r.Post("/posts/{id}/hide", s.Hide)
	r.Delete("/posts/{id}/hide", s.Unhide)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Hide removes a post from the caller's own timeline.
//
// Idempotent, because the button that calls it optimistically removes the row
// from the list and a double tap should not be an error. Refuses the caller's
// own post: hiding your own post from your own timeline is a no-op nobody asked
// for, and offering it would only raise the question of what it means.
func (s *Service) Hide(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var authorID string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, postID).Scan(&authorID)
	if authorID == "" {
		writeError(w, 404, "post not found")
		return
	}
	if authorID == userID {
		writeError(w, 400, "you cannot hide your own post")
		return
	}

	if _, err := s.db.Exec(`
		INSERT INTO hidden_posts (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, userID, postID); err != nil {
		writeError(w, 500, "could not hide post")
		return
	}
	writeJSON(w, 200, map[string]string{"message": "hidden"})
}

// Unhide restores a post to the caller's timeline. Also idempotent, and
// deliberately does not check the post still exists: unhiding something already
// deleted should clear the row rather than fail.
func (s *Service) Unhide(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	if _, err := s.db.Exec(`DELETE FROM hidden_posts WHERE user_id = $1 AND post_id = $2`, userID, postID); err != nil {
		writeError(w, 500, "could not unhide post")
		return
	}
	writeJSON(w, 200, map[string]string{"message": "unhidden"})
}

// List backs the management screen, so a hidden post can be found again.
//
// Carries enough to recognise the post (a preview, its author, when it was
// hidden) without being a second feed implementation. A post the author has
// since deleted is gone from here too, via the FK cascade.
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	rows, err := s.db.Query(`
		SELECT p.id, LEFT(p.content, 200), COALESCE(p.image_url, ''),
		       u.username, COALESCE(u.display_name, ''), COALESCE(u.avatar_url, ''),
		       p.created_at, h.created_at
		FROM hidden_posts h
		JOIN posts p ON p.id = h.post_id AND p.deleted_at IS NULL
		JOIN users u ON u.id = p.author_id
		WHERE h.user_id = $1
		ORDER BY h.created_at DESC
	`, userID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type HiddenPost struct {
		ID          string `json:"id"`
		Content     string `json:"content"`
		ImageURL    string `json:"image_url"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		CreatedAt   string `json:"created_at"`
		HiddenAt    string `json:"hidden_at"`
	}
	var posts []HiddenPost
	for rows.Next() {
		var p HiddenPost
		rows.Scan(&p.ID, &p.Content, &p.ImageURL, &p.Username, &p.DisplayName, &p.AvatarURL, &p.CreatedAt, &p.HiddenAt)
		posts = append(posts, p)
	}
	if posts == nil {
		posts = []HiddenPost{}
	}
	writeJSON(w, 200, map[string]any{"posts": posts})
}
