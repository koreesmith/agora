package federation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
)

// Fediverse relay support (AGORA-220) — a relay is a third-party server this
// instance subscribes to (via the instance actor, AGORA-219) to receive a
// firehose of public posts from every other server that publishes to it,
// and in turn Announces its own local public posts to (AGORA-221) so others
// can discover it. Modeled after Mastodon's own admin Relays feature: an
// admin enters a relay's inbox URL directly (not an actor URL — relay
// software varies too much for a normal actor-resolution round trip to be
// reliable, and Mastodon's own UI takes the inbox URL as-is), and the
// subscription handshake follows the same "object: Public collection"
// convention most relay implementations (Mastodon's bundled relay,
// activityrelay, aoderrelay, ...) expect rather than a relay-specific
// per-actor Follow.

// RegisterAdminRoutes registers relay-management endpoints, gated by
// RequireAdmin at the router-group level in cmd/server/main.go — the same
// pattern pages.RegisterAdminRoutes uses for a non-admin package's own
// admin-only routes. These live here rather than in admin.go because
// subscribing to/unsubscribing from a relay requires signing a
// Follow/Undo(Follow) as the instance actor, machinery that only exists in
// this package; admin.go's Service has no reference to it.
func RegisterAdminRoutes(r chi.Router, s *Service) {
	r.Get("/admin/relays", s.ListRelays)
	r.Post("/admin/relays", s.AddRelay)
	r.Post("/admin/relays/{id}/enable", s.EnableRelay)
	r.Post("/admin/relays/{id}/disable", s.DisableRelay)
	r.Delete("/admin/relays/{id}", s.DeleteRelay)
}

func (s *Service) ListRelays(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, inbox_url, actor_url, status, created_at FROM relays ORDER BY created_at DESC`)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	relays := []map[string]any{}
	for rows.Next() {
		var id, inboxURL, actorURL, status string
		var createdAt time.Time
		if rows.Scan(&id, &inboxURL, &actorURL, &status, &createdAt) != nil {
			continue
		}
		relays = append(relays, map[string]any{
			"id": id, "inbox_url": inboxURL, "actor_url": actorURL,
			"status": status, "created_at": createdAt,
		})
	}
	writeJSON(w, 200, map[string]any{"relays": relays})
}

// AddRelay registers a new relay by its inbox URL and immediately sends it a
// Follow from the instance actor. Status starts "pending" until the relay's
// own Accept/Reject arrives (handleRelayAccept/handleRelayReject below) —
// some relay implementations never respond at all and just start
// forwarding, so "pending" isn't treated as an error state, only an
// unconfirmed one.
func (s *Service) AddRelay(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		InboxURL string `json:"inbox_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InboxURL == "" {
		writeError(w, 400, "inbox_url required")
		return
	}
	if !strings.HasPrefix(req.InboxURL, "https://") {
		writeError(w, 400, "inbox_url must be https")
		return
	}

	var addedBy any
	if userID != "" {
		addedBy = userID
	}
	var id string
	err := s.db.QueryRow(`
		INSERT INTO relays (inbox_url, status, added_by)
		VALUES ($1, 'pending', $2)
		ON CONFLICT (inbox_url) DO UPDATE SET status = 'pending'
		RETURNING id
	`, req.InboxURL, addedBy).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not add relay")
		return
	}

	s.sendRelayFollow(req.InboxURL)
	writeJSON(w, 201, map[string]string{"id": id, "message": "relay follow requested"})
}

// EnableRelay re-subscribes a previously disabled relay by sending a fresh
// Follow — a no-op (not an error) if it's already enabled, since the admin
// screen's Enable/Disable state can be clicked idempotently.
func (s *Service) EnableRelay(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var inboxURL, status string
	if err := s.db.QueryRow(`SELECT inbox_url, status FROM relays WHERE id = $1`, id).Scan(&inboxURL, &status); err != nil {
		writeError(w, 404, "relay not found")
		return
	}
	if status == "enabled" {
		writeJSON(w, 200, map[string]string{"message": "already enabled"})
		return
	}
	s.db.Exec(`UPDATE relays SET status = 'pending' WHERE id = $1`, id)
	s.sendRelayFollow(inboxURL)
	writeJSON(w, 200, map[string]string{"message": "relay follow re-requested"})
}

// DisableRelay sends Undo(Follow) and marks the relay disabled, stopping
// both the inbound firehose and outbound Announces (AGORA-221) without
// forgetting the relay entirely the way DeleteRelay does.
func (s *Service) DisableRelay(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var inboxURL string
	if err := s.db.QueryRow(`SELECT inbox_url FROM relays WHERE id = $1`, id).Scan(&inboxURL); err != nil {
		writeError(w, 404, "relay not found")
		return
	}
	s.db.Exec(`UPDATE relays SET status = 'disabled' WHERE id = $1`, id)
	s.sendRelayUndo(inboxURL)
	writeJSON(w, 200, map[string]string{"message": "relay disabled"})
}

// DeleteRelay sends Undo(Follow) first if the subscription might still be
// live (enabled or awaiting confirmation), then forgets the relay entirely.
func (s *Service) DeleteRelay(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var inboxURL, status string
	if err := s.db.QueryRow(`SELECT inbox_url, status FROM relays WHERE id = $1`, id).Scan(&inboxURL, &status); err != nil {
		writeError(w, 404, "relay not found")
		return
	}
	if status == "enabled" || status == "pending" {
		s.sendRelayUndo(inboxURL)
	}
	s.db.Exec(`DELETE FROM relays WHERE id = $1`, id)
	writeJSON(w, 200, map[string]string{"message": "relay removed"})
}

// sendRelayFollow sends a Follow from the instance actor to a relay's
// inbox, object set to the special "Public collection" IRI rather than the
// relay's own actor URL — the convention relay software (Mastodon's own
// admin Relays feature, activityrelay, aoderrelay, ...) expects, since a
// relay isn't followed *as* an individual actor the way a user or page is.
func (s *Service) sendRelayFollow(inboxURL string) {
	actor := s.instanceActorURL()
	follow := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       actor + fmt.Sprintf("/follows/%d", time.Now().UnixNano()),
		"type":     "Follow",
		"actor":    actor,
		"object":   "https://www.w3.org/ns/activitystreams#Public",
	}
	s.enqueueInstanceAPDelivery(inboxURL, follow)
}

func (s *Service) sendRelayUndo(inboxURL string) {
	actor := s.instanceActorURL()
	undo := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       actor + fmt.Sprintf("/undos/%d", time.Now().UnixNano()),
		"type":     "Undo",
		"actor":    actor,
		"object": map[string]any{
			"type":   "Follow",
			"actor":  actor,
			"object": "https://www.w3.org/ns/activitystreams#Public",
		},
	}
	s.enqueueInstanceAPDelivery(inboxURL, undo)
}

// handleRelayAccept marks a relay subscription as enabled once its Accept
// arrives. There's no per-relay Follow id to match against — a relay
// Follow's object is the generic "Public collection" sentinel, not a
// relay-specific IRI (see sendRelayFollow) — so this matches by comparing
// the Accept's verified signer domain against each pending/disabled relay's
// inbox_url domain, the same domain-based identity isInstanceBlocked
// already uses elsewhere. Also caches the relay's real actor URL (now known
// from the verified signer) for display, since it wasn't resolved up front.
func (s *Service) handleRelayAccept(verifiedActor string) {
	matchID := s.matchRelayByDomain(verifiedActor, "pending", "disabled")
	if matchID == "" {
		return
	}
	s.db.Exec(`UPDATE relays SET status = 'enabled', actor_url = $1 WHERE id = $2`, verifiedActor, matchID)
}

// handleRelayReject reverts a pending relay subscription so the admin
// screen shows it needs attention rather than looking stuck forever.
func (s *Service) handleRelayReject(verifiedActor string) {
	matchID := s.matchRelayByDomain(verifiedActor, "pending")
	if matchID == "" {
		return
	}
	s.db.Exec(`UPDATE relays SET status = 'rejected' WHERE id = $1`, matchID)
}

// matchRelayByDomain finds the relay row (in one of the given statuses)
// whose inbox_url shares a domain with the given actor URL — see
// handleRelayAccept's comment for why domain equality is the only
// correlation available for a relay's Accept/Reject.
func (s *Service) matchRelayByDomain(actorURL string, statuses ...string) string {
	domain := domainFromURL(actorURL)
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, st := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = st
	}
	rows, err := s.db.Query(
		fmt.Sprintf(`SELECT id, inbox_url FROM relays WHERE status IN (%s)`, strings.Join(placeholders, ",")),
		args...,
	)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var id, inboxURL string
		if rows.Scan(&id, &inboxURL) != nil {
			continue
		}
		if domainFromURL(inboxURL) == domain {
			return id
		}
	}
	return ""
}
