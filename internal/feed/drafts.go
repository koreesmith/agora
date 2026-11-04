package feed

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agora-social/agora/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

// AGORA-431: draft posts. Someone can save what is in the compose box, on the web
// or in the app, and finish it later from either. The composer's fields are kept
// as one JSON object that the server doesn't interpret, so adding a field to the
// composer needs no change here. The server does know three things about it:
// which uploaded files it points to (so cleaning up unused media leaves them),
// how big it may be, and how many a person may keep.

const (
	// MaxDrafts is how many drafts one account can keep.
	MaxDrafts = 25
	// DraftExpiry is how long an untouched draft is kept before it is removed.
	DraftExpiry = 90 * 24 * time.Hour
	// maxDraftBytes bounds the JSON a draft may carry.
	maxDraftBytes = 64 << 10
	// maxDraftChars is the longest text a draft holds.
	maxDraftChars = 10000
)

func registerDraftRoutes(r chi.Router, s *Service) {
	r.Get("/drafts", s.ListDrafts)
	r.Post("/drafts", s.CreateDraft)
	r.Put("/drafts/{id}", s.UpdateDraft)
	r.Delete("/drafts/{id}", s.DeleteDraft)
}

type draftBody struct {
	Content string          `json:"content"`
	Data    json.RawMessage `json:"data"`
}

type draftOut struct {
	ID        string          `json:"id"`
	Content   string          `json:"content"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// draftMedia lists the uploaded files a draft's data points to: the images, the
// video and its thumbnail. Only this instance's own uploads count.
func draftMedia(data map[string]any) []string {
	var urls []string
	add := func(v any) {
		if u, ok := v.(string); ok && strings.HasPrefix(u, "/uploads/") {
			urls = append(urls, u)
		}
	}
	if list, ok := data["image_urls"].([]any); ok {
		for _, v := range list {
			add(v)
		}
	}
	add(data["image_url"])
	add(data["video_url"])
	add(data["video_thumb_url"])
	if urls == nil {
		urls = []string{}
	}
	return urls
}

// parseDraft validates a request body and returns what to store.
func parseDraft(r *http.Request) (content string, data []byte, media []string, status int, msg string) {
	var body draftBody
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxDraftBytes+4096)).Decode(&body); err != nil {
		return "", nil, nil, 400, "invalid json"
	}
	parsed := map[string]any{}
	if len(body.Data) > 0 && string(body.Data) != "null" {
		if err := json.Unmarshal(body.Data, &parsed); err != nil {
			return "", nil, nil, 400, "data must be a JSON object"
		}
	}
	if len(body.Data) > maxDraftBytes {
		return "", nil, nil, 413, "this draft is too big to save"
	}
	if utf8.RuneCountInString(body.Content) > maxDraftChars {
		return "", nil, nil, 413, "this draft is too long to save"
	}
	media = draftMedia(parsed)
	// A draft of nothing is not worth keeping.
	hasPoll := false
	if opts, ok := parsed["poll_options"].([]any); ok {
		for _, o := range opts {
			if s, ok := o.(string); ok && strings.TrimSpace(s) != "" {
				hasPoll = true
			}
		}
	}
	if strings.TrimSpace(body.Content) == "" && len(media) == 0 && !hasPoll {
		return "", nil, nil, 400, "there is nothing to save yet"
	}
	out, _ := json.Marshal(parsed)
	return body.Content, out, media, 0, ""
}

// ListDrafts returns the viewer's drafts, most recently changed first.
func (s *Service) ListDrafts(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	rows, err := s.db.Query(`
		SELECT id, content, data, created_at, updated_at FROM post_drafts
		 WHERE user_id = $1 AND updated_at > NOW() - $2 * INTERVAL '1 second'
		 ORDER BY updated_at DESC, id DESC LIMIT $3`, userID, int64(DraftExpiry/time.Second), MaxDrafts)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()
	out := []draftOut{}
	for rows.Next() {
		var d draftOut
		var data []byte
		if rows.Scan(&d.ID, &d.Content, &data, &d.CreatedAt, &d.UpdatedAt) == nil {
			d.Data = json.RawMessage(data)
			out = append(out, d)
		}
	}
	writeJSON(w, 200, map[string]any{"drafts": out, "max_drafts": MaxDrafts})
}

// CreateDraft saves a new draft. 409 when the account is at its limit.
func (s *Service) CreateDraft(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	content, data, media, status, msg := parseDraft(r)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	// One statement, so two quick saves can't both slip under the limit.
	var d draftOut
	var raw []byte
	err := s.db.QueryRow(`
		INSERT INTO post_drafts (user_id, content, data, media_urls)
		SELECT $1::uuid, $2, $3::jsonb, $4
		 WHERE (SELECT COUNT(*) FROM post_drafts WHERE user_id = $1::uuid) < $5
		RETURNING id, content, data, created_at, updated_at`,
		userID, content, string(data), pq.Array(media), MaxDrafts,
	).Scan(&d.ID, &d.Content, &raw, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		// No row came back: the account is at its limit.
		writeError(w, 409, "You can keep up to 25 drafts. Delete one to save another.")
		return
	}
	if err != nil {
		writeError(w, 500, "could not save this draft")
		return
	}
	d.Data = json.RawMessage(raw)
	writeJSON(w, 201, d)
}

// UpdateDraft replaces a draft's contents.
func (s *Service) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")
	content, data, media, status, msg := parseDraft(r)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	var d draftOut
	var raw []byte
	err := s.db.QueryRow(`
		UPDATE post_drafts SET content = $3, data = $4::jsonb, media_urls = $5, updated_at = NOW()
		 WHERE id = $1::uuid AND user_id = $2
		RETURNING id, content, data, created_at, updated_at`,
		id, userID, content, string(data), pq.Array(media),
	).Scan(&d.ID, &d.Content, &raw, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		writeError(w, 404, "draft not found")
		return
	}
	d.Data = json.RawMessage(raw)
	writeJSON(w, 200, d)
}

// DeleteDraft removes a draft. Idempotent; someone else's is simply not found
// and left alone.
func (s *Service) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	s.db.Exec(`DELETE FROM post_drafts WHERE id = $1::uuid AND user_id = $2`, chi.URLParam(r, "id"), userID)
	writeJSON(w, 200, map[string]any{"deleted": true})
}

// StartDraftCleanup removes drafts nobody has touched for DraftExpiry, once a
// day, so abandoned ones don't hold uploaded files forever.
func (s *Service) StartDraftCleanup(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	s.cleanupDrafts()
	for {
		select {
		case <-ticker.C:
			s.cleanupDrafts()
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) cleanupDrafts() {
	res, err := s.db.Exec(`DELETE FROM post_drafts WHERE updated_at < NOW() - $1 * INTERVAL '1 second'`, int64(DraftExpiry/time.Second))
	if err != nil {
		log.Printf("feed: draft cleanup: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("feed: draft cleanup: removed %d untouched drafts", n)
	}
}
