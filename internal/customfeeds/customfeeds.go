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
	r.Put("/feeds/pins/order", s.ReorderPins)
	r.Get("/feeds/{id}", s.GetFeed)
	r.Put("/feeds/{id}", s.UpdateFeed)
	r.Put("/feeds/{id}/pin", s.SetPinned)
	r.Delete("/feeds/{id}", s.DeleteFeed)
}

// Caps. maxPinnedFeeds is what the feed picker can show as pills before it
// stops being scannable (AGORA-303); everything past it lives in the picker's
// overflow menu, so this is a display budget rather than a storage limit.
const (
	maxCustomFeeds = 20
	maxPinnedFeeds = 3
)

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
	Pinned        bool     `json:"pinned"`
	Position      int      `json:"position"`
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
	if count >= maxCustomFeeds {
		writeError(w, http.StatusUnprocessableEntity, "maximum of 20 custom feeds reached")
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback()

	// A user's first few feeds pin themselves. Without this the picker would
	// show nothing but Home until the user found the pin control, which makes
	// creating a feed look like it did nothing. Past the cap, new feeds land in
	// the overflow menu and pinning becomes a deliberate choice.
	var pinnedCount, nextPos int
	if err := tx.QueryRow(
		`SELECT COUNT(*), COALESCE(MAX(position) + 1, 0)
		   FROM custom_feeds WHERE owner_id = $1 AND pinned`, ownerID,
	).Scan(&pinnedCount, &nextPos); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	pinned := pinnedCount < maxPinnedFeeds

	// Unpinned rows must stay at position 0. They sort by created_at, but only
	// because they all tie on position first. Give them real positions and the
	// tail would start ordering by whatever the pinned set happened to look
	// like when each was created.
	pos := 0
	if pinned {
		pos = nextPos
	}

	var feedID string
	if err := tx.QueryRow(
		`INSERT INTO custom_feeds (owner_id, name, smart_ranking, pinned, position)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		ownerID, req.Name, req.SmartRanking, pinned, pos,
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

	// Pinned first, in the user's own order: these become the picker's pills, so
	// their positions have to hold still as feeds are added and removed. The
	// unpinned tail stays newest-first, which is the right default for a list
	// the user browses rather than memorises.
	rows, err := s.db.Query(
		`SELECT id, owner_id, name, smart_ranking, pinned, position, created_at
		   FROM custom_feeds WHERE owner_id = $1
		  ORDER BY pinned DESC, position ASC, created_at DESC`,
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
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.Name, &f.SmartRanking, &f.Pinned, &f.Position, &f.CreatedAt); err != nil {
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

	tx, err := s.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(
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

	// Deleting a pinned feed leaves a hole in the sequence. Close it, so the
	// free pin slot is usable and positions stay 0..n-1.
	if err := compactPins(tx, ownerID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetPinned pins or unpins one feed. Pinning appends to the end of the pinned
// set; the client reorders separately via ReorderPins.
func (s *Service) SetPinned(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	feedID := chi.URLParam(r, "id")

	var req struct {
		Pinned bool `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback()

	var alreadyPinned bool
	err = tx.QueryRow(
		`SELECT pinned FROM custom_feeds WHERE id = $1 AND owner_id = $2`,
		feedID, ownerID,
	).Scan(&alreadyPinned)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "feed not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if req.Pinned && !alreadyPinned {
		var pinnedCount, nextPos int
		if err := tx.QueryRow(
			`SELECT COUNT(*), COALESCE(MAX(position) + 1, 0)
			   FROM custom_feeds WHERE owner_id = $1 AND pinned`, ownerID,
		).Scan(&pinnedCount, &nextPos); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if pinnedCount >= maxPinnedFeeds {
			writeError(w, http.StatusUnprocessableEntity,
				"you can pin up to 3 feeds; unpin one to make room")
			return
		}
		if _, err := tx.Exec(
			`UPDATE custom_feeds SET pinned = true, position = $3 WHERE id = $1 AND owner_id = $2`,
			feedID, ownerID, nextPos,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else if !req.Pinned && alreadyPinned {
		if _, err := tx.Exec(
			`UPDATE custom_feeds SET pinned = false, position = 0 WHERE id = $1 AND owner_id = $2`,
			feedID, ownerID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := compactPins(tx, ownerID); err != nil {
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

// ReorderPins takes the pinned feeds in their new order. Any pinned feed the
// caller omits keeps its relative order after the ones listed, so a stale
// client cannot silently unpin a feed it did not know about.
func (s *Service) ReorderPins(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())

	var req struct {
		FeedIDs []string `json:"feed_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback()

	// Negative positions keep the listed feeds ahead of any omitted ones, which
	// still hold positions 0 and up. compactPins then renumbers the whole set
	// from 0 in that combined order.
	for i, id := range req.FeedIDs {
		res, err := tx.Exec(
			`UPDATE custom_feeds SET position = $3 WHERE id = $1 AND owner_id = $2 AND pinned`,
			id, ownerID, i-len(req.FeedIDs),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusBadRequest, "feed not found or not pinned: "+id)
			return
		}
	}

	if err := compactPins(tx, ownerID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.ListFeeds(w, r)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// compactPins renumbers a user's pinned feeds to 0..n-1, preserving their
// current relative order. Every write that can leave a gap or a duplicate
// position ends here, so ORDER BY position is always unambiguous.
func compactPins(tx *sql.Tx, ownerID string) error {
	_, err := tx.Exec(
		`UPDATE custom_feeds cf
			SET position = r.rn - 1
			FROM (
				SELECT id, ROW_NUMBER() OVER (ORDER BY position ASC, created_at ASC) AS rn
				  FROM custom_feeds WHERE owner_id = $1 AND pinned
			) r
			WHERE cf.id = r.id AND cf.position <> r.rn - 1`,
		ownerID,
	)
	return err
}

func (s *Service) fetchFeed(feedID, ownerID string) (Feed, bool) {
	var f Feed
	err := s.db.QueryRow(
		`SELECT id, owner_id, name, smart_ranking, pinned, position, created_at
		   FROM custom_feeds WHERE id = $1 AND owner_id = $2`,
		feedID, ownerID,
	).Scan(&f.ID, &f.OwnerID, &f.Name, &f.SmartRanking, &f.Pinned, &f.Position, &f.CreatedAt)
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
		"include_page", "exclude_page", "fediverse_account", "fediverse_all",
		"atproto_account", "atproto_all",
		"exclude_fediverse_account", "exclude_atproto_account":
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
