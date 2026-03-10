package search

import (
	"encoding/json"
	"net/http"

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
}

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
		       COALESCE(f.status, '') AS friendship_status
		FROM users u
		LEFT JOIN friendships f ON (
			(f.requester_id = $1 AND f.addressee_id = u.id)
		 OR (f.requester_id = u.id AND f.addressee_id = $1)
		)
		WHERE u.deletion_scheduled_at IS NULL
		  AND u.is_suspended = false
		  AND (
		    u.username ILIKE '%' || $2 || '%'
		    OR u.display_name ILIKE '%' || $2 || '%'
		  )
		ORDER BY
		  CASE WHEN LOWER(u.username) = LOWER($2) THEN 0 ELSE 1 END,
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = chi.URLParam
