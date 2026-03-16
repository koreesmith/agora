package blocks

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

func New(db *store.DB) *Service {
	return &Service{db: db}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/blocks",                s.ListBlocks)
	r.Post("/blocks/{username}",    s.Block)
	r.Delete("/blocks/{username}",  s.Unblock)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── IsBlocked helper (used by other packages via DB query) ────────────────────

// IsBlocked returns true if either user has blocked the other.
func (s *Service) IsBlocked(userA, userB string) bool {
	if userA == "" || userB == "" { return false }
	var exists bool
	s.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM blocks
			WHERE (blocker_id = $1 AND blocked_id = $2)
			   OR (blocker_id = $2 AND blocked_id = $1)
		)
	`, userA, userB).Scan(&exists)
	return exists
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *Service) Block(w http.ResponseWriter, r *http.Request) {
	blockerID := auth.UserIDFromCtx(r.Context())
	username  := chi.URLParam(r, "username")

	var blockedID string
	s.db.QueryRow(`SELECT id FROM users WHERE username = $1 AND deletion_scheduled_at IS NULL`, username).Scan(&blockedID)
	if blockedID == ""        { writeError(w, 404, "user not found"); return }
	if blockedID == blockerID { writeError(w, 400, "cannot block yourself"); return }

	// Remove any existing friendship
	s.db.Exec(`DELETE FROM friendships WHERE (requester_id=$1 AND addressee_id=$2) OR (requester_id=$2 AND addressee_id=$1)`, blockerID, blockedID)

	// Insert block (idempotent)
	s.db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, blockerID, blockedID)

	writeJSON(w, 200, map[string]string{"message": "blocked"})
}

func (s *Service) Unblock(w http.ResponseWriter, r *http.Request) {
	blockerID := auth.UserIDFromCtx(r.Context())
	username  := chi.URLParam(r, "username")

	var blockedID string
	s.db.QueryRow(`SELECT id FROM users WHERE username = $1 AND deletion_scheduled_at IS NULL`, username).Scan(&blockedID)
	if blockedID == "" { writeError(w, 404, "user not found"); return }

	s.db.Exec(`DELETE FROM blocks WHERE blocker_id=$1 AND blocked_id=$2`, blockerID, blockedID)
	writeJSON(w, 200, map[string]string{"message": "unblocked"})
}

func (s *Service) ListBlocks(w http.ResponseWriter, r *http.Request) {
	blockerID := auth.UserIDFromCtx(r.Context())

	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url, b.created_at
		FROM blocks b
		JOIN users u ON u.id = b.blocked_id
		WHERE b.blocker_id = $1
		ORDER BY b.created_at DESC
	`, blockerID)
	if err != nil { writeError(w, 500, "db error"); return }
	defer rows.Close()

	type BlockedUser struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		BlockedAt   string `json:"blocked_at"`
	}

	var blocked []BlockedUser
	for rows.Next() {
		var u BlockedUser
		rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.BlockedAt)
		blocked = append(blocked, u)
	}
	if blocked == nil { blocked = []BlockedUser{} }
	writeJSON(w, 200, map[string]any{"blocked": blocked})
}
