package customfeeds

import (
	"database/sql"
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
	r.Post("/feeds", s.CreateFeed)
	r.Get("/feeds", s.ListFeeds)
	r.Get("/feeds/{id}", s.GetFeed)
	r.Put("/feeds/{id}", s.UpdateFeed)
	r.Delete("/feeds/{id}", s.DeleteFeed)
}

// ── Types ─────────────────────────────────────────────────────────────────────

type Filter struct {
	ID         string `json:"id"`
	FilterType string `json:"filter_type"`
	Value      string `json:"value"`
}

type Feed struct {
	ID            string   `json:"id"`
	OwnerID       string   `json:"owner_id"`
	Name          string   `json:"name"`
	SmartRanking  bool     `json:"smart_ranking"`
	CreatedAt     string   `json:"created_at"`
	Filters       []Filter `json:"filters,omitempty"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *Service) CreateFeed(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())

	var req struct {
		Name         string   `json:"name"`
		SmartRanking bool     `json:"smart_ranking"`
		Filters      []Filter `json:"filters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM custom_feeds WHERE owner_id = $1`, ownerID).Scan(&count)
	if count >= 20 {
		writeError(w, http.StatusUnprocessableEntity, "maximum of 20 custom feeds reached")
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback()

	var feedID string
	if err := tx.QueryRow(
		`INSERT INTO custom_feeds (owner_id, name, smart_ranking) VALUES ($1, $2, $3) RETURNING id`,
		ownerID, req.Name, req.SmartRanking,
	).Scan(&feedID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	for _, f := range req.Filters {
		if !validFilterType(f.FilterType) {
			writeError(w, http.StatusBadRequest, "invalid filter_type: "+f.FilterType)
			return
		}
		if _, err := tx.Exec(
			`INSERT INTO custom_feed_filters (feed_id, filter_type, value) VALUES ($1, $2, $3)`,
			feedID, f.FilterType, f.Value,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	feed, ok := s.fetchFeed(feedID, ownerID)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, feed)
}

func (s *Service) ListFeeds(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())

	rows, err := s.db.Query(
		`SELECT id, owner_id, name, smart_ranking, created_at FROM custom_feeds WHERE owner_id = $1 ORDER BY created_at DESC`,
		ownerID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	feeds := []Feed{}
	for rows.Next() {
		var f Feed
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.Name, &f.SmartRanking, &f.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		feeds = append(feeds, f)
	}
	writeJSON(w, http.StatusOK, feeds)
}

func (s *Service) GetFeed(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	feedID := chi.URLParam(r, "id")

	feed, ok := s.fetchFeed(feedID, ownerID)
	if !ok {
		writeError(w, http.StatusNotFound, "feed not found")
		return
	}
	writeJSON(w, http.StatusOK, feed)
}

func (s *Service) UpdateFeed(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	feedID := chi.URLParam(r, "id")

	var req struct {
		Name         string   `json:"name"`
		SmartRanking bool     `json:"smart_ranking"`
		Filters      []Filter `json:"filters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE custom_feeds SET name = $1, smart_ranking = $4 WHERE id = $2 AND owner_id = $3`,
		req.Name, feedID, ownerID, req.SmartRanking,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "feed not found")
		return
	}

	if _, err := tx.Exec(`DELETE FROM custom_feed_filters WHERE feed_id = $1`, feedID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	for _, f := range req.Filters {
		if !validFilterType(f.FilterType) {
			writeError(w, http.StatusBadRequest, "invalid filter_type: "+f.FilterType)
			return
		}
		if _, err := tx.Exec(
			`INSERT INTO custom_feed_filters (feed_id, filter_type, value) VALUES ($1, $2, $3)`,
			feedID, f.FilterType, f.Value,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	feed, ok := s.fetchFeed(feedID, ownerID)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, feed)
}

func (s *Service) DeleteFeed(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	feedID := chi.URLParam(r, "id")

	res, err := s.db.Exec(
		`DELETE FROM custom_feeds WHERE id = $1 AND owner_id = $2`,
		feedID, ownerID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "feed not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Service) fetchFeed(feedID, ownerID string) (Feed, bool) {
	var f Feed
	err := s.db.QueryRow(
		`SELECT id, owner_id, name, smart_ranking, created_at FROM custom_feeds WHERE id = $1 AND owner_id = $2`,
		feedID, ownerID,
	).Scan(&f.ID, &f.OwnerID, &f.Name, &f.SmartRanking, &f.CreatedAt)
	if err == sql.ErrNoRows {
		return Feed{}, false
	}
	if err != nil {
		return Feed{}, false
	}

	rows, err := s.db.Query(
		`SELECT id, filter_type, value FROM custom_feed_filters WHERE feed_id = $1 ORDER BY created_at ASC`,
		feedID,
	)
	if err != nil {
		return Feed{}, false
	}
	defer rows.Close()

	f.Filters = []Filter{}
	for rows.Next() {
		var fl Filter
		if err := rows.Scan(&fl.ID, &fl.FilterType, &fl.Value); err != nil {
			return Feed{}, false
		}
		f.Filters = append(f.Filters, fl)
	}
	return f, true
}

func validFilterType(t string) bool {
	switch t {
	case "friend_group", "community_group", "exclude_friend", "exclude_group", "post_type",
		"include_page", "exclude_page":
		return true
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
