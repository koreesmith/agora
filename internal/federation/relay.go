package federation

import (
	"encoding/json"
	"fmt"
	"log"
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

// enabledRelayInboxes returns every currently-enabled relay's inbox URL —
// used by BroadcastPublicPost/BroadcastUpdatePost/BroadcastDeletePost
// (AGORA-221) to add relays as extra recipients of a local public post's
// normal follower delivery. Deliberately delivered signed as the post's own
// author (via the existing enqueueAPDelivery/deliverAPActivity path), not
// the instance actor — a relay is "just another subscriber" from a Create's
// perspective, and at least one popular relay implementation verifies the
// delivered activity's attributedTo/actor against the HTTP signature's own
// keyId the same strict way Agora's own inbound handling does (see
// handleInboundCreate), which a delivery signed as the instance actor would
// fail. The instance actor's own delivery queue (instance_ap_delivery_queue)
// is reserved for the instance actor's own authored traffic — the
// Follow/Undo(Follow) subscription handshake (AGORA-220), not post content.
func (s *Service) enabledRelayInboxes() []string {
	rows, err := s.db.Query(`SELECT inbox_url FROM relays WHERE status = 'enabled'`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var inboxes []string
	for rows.Next() {
		var inbox string
		if rows.Scan(&inbox) == nil {
			inboxes = append(inboxes, inbox)
		}
	}
	return inboxes
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

// ── Inbound relay ingestion (AGORA-222) ───────────────────────────────────────
//
// A relay's entire purpose is delivering content from accounts nobody
// locally follows or authored — neither of the existing inbound paths
// accepts that: handleInboundCreate requires the Create's own signer to be
// the post's author (attributedTo == verifiedActor), and
// handleInboundAnnounce/resolveFederatableTarget only ever resolves an
// Announce's object against one of Agora's *own* posts. Both are correct
// and stay untouched for every other sender; a relay-sourced activity
// (verifiedActor matching a subscribed relay, checked in
// handleStandardInbox above) is routed here instead.
//
// Relay implementations vary in what they actually forward: the "true"
// relay convention (Mastodon's bundled relay, activityrelay, aoderrelay) is
// an Announce whose object is a bare IRI pointing at the original post,
// which must be dereferenced to get its content; some simpler
// implementations instead forward the full Create with the object already
// embedded. Both are handled.

// apRemoteNote is the subset of a dereferenced Note/Question object this
// package cares about for ingestion — mirrors the anonymous struct
// handleInboundCreate decodes inline, pulled out here since it's built from
// two different sources (an embedded Create's object, or a dereferenced
// bare-IRI fetch) rather than always from the same json.RawMessage.
type apRemoteNote struct {
	ID           string
	AttributedTo string
	Content      string
	Summary      string
	InReplyTo    string
	Attachment   []apAttachment
	Tag          []apTagEntry // AGORA-213
}

// ingestRelayForwardedCreate handles a relay forwarding a full Create
// directly (object already embedded) — the simpler of the two shapes,
// needing no extra network round trip.
func (s *Service) ingestRelayForwardedCreate(objectRaw json.RawMessage) {
	var note struct {
		ID           string         `json:"id"`
		AttributedTo string         `json:"attributedTo"`
		Content      string         `json:"content"`
		Summary      string         `json:"summary"`
		InReplyTo    string         `json:"inReplyTo"`
		Attachment   []apAttachment `json:"attachment"`
		Tag          []apTagEntry   `json:"tag"` // AGORA-213
	}
	if err := json.Unmarshal(objectRaw, &note); err != nil {
		return
	}
	s.ingestRelaySourcedNote(&apRemoteNote{
		ID: note.ID, AttributedTo: note.AttributedTo, Content: note.Content,
		Summary: note.Summary, InReplyTo: note.InReplyTo, Attachment: note.Attachment,
		Tag: note.Tag,
	})
}

// ingestRelayForwardedAnnounce handles the "true" relay convention — object
// is a bare IRI naming some other instance's post, which must be
// dereferenced to actually get its content (the relay itself never carries
// or rewrites the content, only rebroadcasts a pointer to it). Falls back
// to treating the object as an already-embedded Create if it isn't a bare
// string, for relay implementations that embed inside an Announce instead
// of using Create.
func (s *Service) ingestRelayForwardedAnnounce(objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" {
		s.ingestRelayForwardedCreate(objectRaw)
		return
	}
	if s.isInstanceBlocked(domainFromURL(objectURL)) {
		return
	}
	note, err := s.fetchRemoteNoteSignedAsInstance(objectURL)
	if err != nil || note == nil {
		return
	}
	s.ingestRelaySourcedNote(note)
}

// ingestRelaySourcedNote applies the shared eligibility checks (blocked
// instance, top-level only) and stores the post — the common tail of both
// relay ingestion paths above.
func (s *Service) ingestRelaySourcedNote(note *apRemoteNote) {
	if note.ID == "" || note.AttributedTo == "" {
		return
	}
	// A relay surfaces accounts nobody locally follows — useful for
	// top-level posts (discoverable via explore/search), but a reply into
	// some thread Agora has no other context for isn't independently
	// useful the same way, and resolveReplyTarget has no local thread to
	// attach it to anyway.
	if note.InReplyTo != "" {
		return
	}
	if s.isInstanceBlocked(domainFromURL(note.AttributedTo)) {
		return
	}
	imageURLs, videoURL := matchAttachments(note.Attachment)
	s.ingestRelayedPost(note.AttributedTo, note.ID, note.Content, note.Summary, imageURLs, videoURL, hashtagsFromAPTags(note.Tag))
}

// fetchRemoteNoteSignedAsInstance dereferences a relay-announced post URL,
// signed as the instance actor — there's no local user in context the way
// fetchActorProfileSigned's callers always have one, and this needs an
// actual Note/Question object rather than an Actor document, so it can't
// reuse signedActorProfileFetch/doActorProfileFetch either.
func (s *Service) fetchRemoteNoteSignedAsInstance(objectURL string) (*apRemoteNote, error) {
	if !strings.HasPrefix(objectURL, "https://") {
		return nil, fmt.Errorf("object url must be https")
	}
	_, _, privKey, err := s.getOrCreateInstanceKeyPair()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, objectURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/activity+json")
	if err := signRequest(req, s.instanceActorKeyID(), privKey, []byte{}); err != nil {
		return nil, err
	}

	resp, err := fedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("note fetch returned %d", resp.StatusCode)
	}

	var note struct {
		ID           string         `json:"id"`
		AttributedTo string         `json:"attributedTo"`
		Content      string         `json:"content"`
		Summary      string         `json:"summary"`
		InReplyTo    string         `json:"inReplyTo"`
		Attachment   []apAttachment `json:"attachment"`
		Tag          []apTagEntry   `json:"tag"` // AGORA-213
	}
	if err := json.NewDecoder(resp.Body).Decode(&note); err != nil {
		return nil, err
	}
	if note.ID == "" {
		return nil, fmt.Errorf("empty note id")
	}
	return &apRemoteNote{
		ID: note.ID, AttributedTo: note.AttributedTo, Content: note.Content,
		Summary: note.Summary, InReplyTo: note.InReplyTo, Attachment: note.Attachment,
		Tag: note.Tag,
	}, nil
}

// getOrCreateRemoteAPUserAsInstance mirrors getOrCreateRemoteAPUser, signing
// the cache-miss profile fetch as the instance actor rather than a specific
// local user — a relay-sourced author has no natural "signer" the way a
// reply's recipient or an existing follower does.
func (s *Service) getOrCreateRemoteAPUserAsInstance(actorURL string) (string, error) {
	var id, inboxURL string
	s.db.QueryRow(`SELECT id, ap_inbox_url FROM users WHERE ap_actor_url = $1`, actorURL).Scan(&id, &inboxURL)
	// AGORA-254: same self-healing reasoning as getOrCreateRemoteAPUser — a
	// cache hit with no inbox is a broken/stale stub, and short-circuiting on
	// it forever means every outbound delivery to this actor silently no-ops
	// with nothing logged anywhere.
	if id != "" && inboxURL != "" {
		return id, nil
	}
	if id != "" {
		log.Printf("federation: cached actor %s has no inbox URL, re-fetching to repair", actorURL)
	}
	profile, err := s.fetchActorProfileSignedAsInstance(actorURL)
	if err != nil {
		return "", err
	}
	return s.upsertRemoteAPUser(actorURL, profile)
}

// ingestRelayedPost stores a relay-forwarded top-level post from an author
// nobody locally follows — the entire point of a relay is surfacing content
// from accounts a local user has no reason to already know about. Mirrors
// ingestFollowedPost's idempotent-insert/attachment-storage shape, but with
// no ap_following gate and no per-follower notification loop, since there's
// no follower list to notify — a relay post surfaces via explore/search
// instead (its visibility='public', parent_id=NULL shape already qualifies
// it for PublicFeed same as any other top-level public post). The existing
// ON CONFLICT (remote_post_id, remote_instance) unique constraint on posts
// is what actually dedupes a post forwarded by more than one subscribed
// relay, or redelivered by the same one — no separate dedup table needed.
func (s *Service) ingestRelayedPost(actorURL, noteID, content, summary string, imageURLs []string, videoURL string, tags []string) {
	if actorURL == "" || noteID == "" {
		return
	}
	remoteUserID, err := s.getOrCreateRemoteAPUserAsInstance(actorURL)
	if err != nil || remoteUserID == "" {
		return
	}

	domain := domainFromURL(noteID)
	var postID string
	err = s.db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, parent_id, is_remote, remote_post_id, remote_instance, content_warning)
		VALUES ($1, $2, 'public', NULL, true, $3, $4, $5)
		ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO NOTHING
		RETURNING id
	`, remoteUserID, HTMLToPlainText(content), noteID, domain, HTMLToPlainText(summary)).Scan(&postID)
	if err != nil {
		// ON CONFLICT DO NOTHING + RETURNING yields sql.ErrNoRows on
		// redelivery (or a second subscribed relay forwarding the same
		// post) — expected, not an error.
		return
	}
	s.storeInboundImages(postID, imageURLs)
	s.storeInboundVideo(postID, videoURL)
	s.storeHashtags(postID, tags) // AGORA-213
}
