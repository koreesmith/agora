// Package interactions records per-user feed interaction signals for use by
// the v2 personalised feed ranking algorithm (AGORA-103).
package interactions

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/store"
)

// retentionDays is the rolling window for interaction data.
const retentionDays = 90

type Service struct {
	db *store.DB
}

func NewService(db *store.DB) *Service {
	return &Service{db: db}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Post("/feed/interactions", s.Record)
}

// ── Record ────────────────────────────────────────────────────────────────────

// Record saves a single interaction event. The handler is designed to be
// fire-and-forget from the client: it always returns 204 and never blocks on
// errors so that client UX is unaffected if tracking fails.
func (s *Service) Record(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	var req struct {
		TargetUserID    string `json:"target_user_id"`
		PostID          string `json:"post_id"`
		InteractionType string `json:"interaction_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	validTypes := map[string]bool{
		"like": true, "comment": true, "repost": true,
		"profile_view": true, "link_click": true, "post_view": true,
	}
	if !validTypes[req.InteractionType] {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Fire-and-forget: write async, don't hold up the response.
	go func() {
		var targetID *string
		if req.TargetUserID != "" {
			targetID = &req.TargetUserID
		}
		var postID *string
		if req.PostID != "" {
			postID = &req.PostID
		}
		_, err := s.db.Exec(
			`INSERT INTO feed_interactions (user_id, target_user_id, post_id, interaction_type)
			 VALUES ($1, $2, $3, $4)`,
			userID, targetID, postID, req.InteractionType,
		)
		if err != nil {
			log.Printf("interactions: record error: %v", err)
		}
	}()

	w.WriteHeader(http.StatusNoContent)
}

// ── Background pruning ────────────────────────────────────────────────────────

// StartPruner runs a goroutine that deletes interaction records older than the
// retention window once per day.
func (s *Service) StartPruner(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Run immediately on startup, then on each tick.
	s.prune()

	for {
		select {
		case <-ticker.C:
			s.prune()
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) prune() {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	res, err := s.db.Exec(
		`DELETE FROM feed_interactions WHERE created_at < $1`, cutoff,
	)
	if err != nil {
		log.Printf("interactions: prune error: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("interactions: pruned %d records older than %d days", n, retentionDays)
	}
}
