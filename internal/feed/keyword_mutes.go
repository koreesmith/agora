package feed

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/auth"
)

// AGORA-428: keyword muting. A muted word or phrase hides matching posts from
// the viewer's timelines (main feed, friend-list feeds, custom feeds and the
// public feed). It is deliberately a timeline filter only: Bookmarks, a post
// opened directly, a profile and search still show the post, because the
// viewer went looking for those.
//
// Matching is case-insensitive and whole-word (so muting "cat" does not hide
// "category"), and looks at the post text, its content warning, its image
// description and the text of a post it reposts. A hashtag is matched by
// muting the word with or without the "#". Posts from the Fediverse and
// Bluesky live in the same posts table, so they are covered too.

const (
	maxKeywordMutes   = 200
	maxKeywordLength  = 100
	maxKeywordMuteDay = 3650
)

// keywordMuteClause is "post p (and the post it reposts, rp) matches none of
// the viewer's ($1) live keyword mutes". It needs the p and rp aliases the feed
// queries all use. pattern is a POSIX regex built when the mute is saved, so
// the query never has to escape user text.
const keywordMuteClause = `AND NOT EXISTS (
	SELECT 1 FROM keyword_mutes km
	WHERE km.user_id = $1
	  AND (km.expires_at IS NULL OR km.expires_at > NOW())
	  AND (p.content ~* km.pattern
	    OR p.content_warning ~* km.pattern
	    OR p.image_alt ~* km.pattern
	    OR rp.content ~* km.pattern)
)`

// keywordPattern turns a muted keyword into a whole-word, case-insensitive
// POSIX pattern: the match must not be glued to a letter, digit or underscore
// on either side.
func keywordPattern(keyword string) string {
	return `(^|[^[:alnum:]_])` + regexp.QuoteMeta(keyword) + `($|[^[:alnum:]_])`
}

type keywordMute struct {
	ID        string     `json:"id"`
	Keyword   string     `json:"keyword"`
	ExpiresAt *time.Time `json:"expires_at"` // null = forever
	CreatedAt time.Time  `json:"created_at"`
}

func registerKeywordMuteRoutes(r chi.Router, s *Service) {
	r.Get("/keyword-mutes", s.ListKeywordMutes)
	r.Post("/keyword-mutes", s.AddKeywordMute)
	r.Delete("/keyword-mutes/{id}", s.RemoveKeywordMute)
}

// ListKeywordMutes returns the caller's live mutes, soonest to expire last.
// Expired ones are not listed (and are cleared the next time one is added).
func (s *Service) ListKeywordMutes(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}
	rows, err := s.db.Query(`
		SELECT id, keyword, expires_at, created_at
		FROM keyword_mutes
		WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()
	mutes := []keywordMute{}
	for rows.Next() {
		var m keywordMute
		if rows.Scan(&m.ID, &m.Keyword, &m.ExpiresAt, &m.CreatedAt) == nil {
			mutes = append(mutes, m)
		}
	}
	writeJSON(w, 200, map[string]any{"mutes": mutes})
}

// AddKeywordMute saves a mute for {keyword, days}. days is a whole number of
// days, or absent/0 for a permanent mute. Adding a keyword the caller already
// muted replaces its duration rather than failing.
func (s *Service) AddKeywordMute(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}
	var req struct {
		Keyword string `json:"keyword"`
		Days    int    `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request")
		return
	}
	keyword := strings.Join(strings.Fields(req.Keyword), " ")
	if keyword == "" {
		writeError(w, 400, "keyword is required")
		return
	}
	if utf8.RuneCountInString(keyword) > maxKeywordLength {
		writeError(w, 400, "keyword is too long")
		return
	}
	if req.Days < 0 || req.Days > maxKeywordMuteDay {
		writeError(w, 400, "invalid duration")
		return
	}

	// Housekeeping: drop this user's expired mutes so they cannot pile up.
	s.db.Exec(`DELETE FROM keyword_mutes WHERE user_id = $1 AND expires_at <= NOW()`, userID)

	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM keyword_mutes WHERE user_id = $1`, userID).Scan(&count)
	if count >= maxKeywordMutes {
		writeError(w, 400, "too many muted keywords")
		return
	}

	var expires *time.Time
	if req.Days > 0 {
		t := time.Now().Add(time.Duration(req.Days) * 24 * time.Hour)
		expires = &t
	}
	var m keywordMute
	err := s.db.QueryRow(`
		INSERT INTO keyword_mutes (user_id, keyword, pattern, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, lower(keyword))
		DO UPDATE SET expires_at = EXCLUDED.expires_at, created_at = NOW()
		RETURNING id, keyword, expires_at, created_at
	`, userID, keyword, keywordPattern(keyword), expires).Scan(&m.ID, &m.Keyword, &m.ExpiresAt, &m.CreatedAt)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	writeJSON(w, 200, m)
}

// RemoveKeywordMute deletes one of the caller's mutes. Idempotent.
func (s *Service) RemoveKeywordMute(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}
	if _, err := s.db.Exec(`DELETE FROM keyword_mutes WHERE id::text = $1 AND user_id = $2`,
		chi.URLParam(r, "id"), userID); err != nil {
		writeError(w, 500, "db error")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
