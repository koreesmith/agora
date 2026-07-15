package federation

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
)

// Standard ActivityPub support — WebFinger, actor documents, HTTP-Signature
// verified Follow/Undo, an outbox, and signed outbound Create/Delete delivery
// to followers. This lives alongside the older custom Agora-to-Agora protocol
// in federation.go and inbox.go without altering it: real fediverse software
// (Mastodon, Pleroma, ...) only ever speaks the code in this file.

// ── Actor identity ───────────────────────────────────────────────────────────

type apUser struct {
	ID          string
	Username    string
	DisplayName string
	Bio         string
	AvatarURL   string
	PubKeyPEM   string
	PrivKeyPEM  string
}

// apEligibleUser returns the given local username if it's eligible to be
// federated: exists, local, not private, not scheduled for deletion, and
// hasn't opted out via the per-account activitypub_enabled column. Also
// checks the instance-wide activityPubEnabled() setting (AGORA-156) — not
// the same thing despite the similar name: that one is an admin-controlled
// instance_settings key, this column is a per-user opt-out. Used by every AP
// endpoint (WebFinger, actor doc, Outbox, Followers, inbound Follow) so both
// eligibility rules stay in exactly one place.
func (s *Service) apEligibleUser(handle string) (*apUser, bool) {
	if !s.activityPubEnabled() {
		return nil, false
	}
	var u apUser
	err := s.db.QueryRow(`
		SELECT id, username, display_name, bio, avatar_url,
		       federation_public_key, federation_private_key
		FROM users
		WHERE LOWER(username) = LOWER($1) AND is_remote = false AND profile_private = false
		  AND activitypub_enabled = true AND deletion_scheduled_at IS NULL
	`, handle).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.AvatarURL, &u.PubKeyPEM, &u.PrivKeyPEM)
	if err != nil {
		return nil, false
	}
	return &u, true
}

// absoluteURL resolves a possibly-relative URL (e.g. an avatar path like
// "/uploads/avatars/xyz.jpg", stored relative because the SPA resolves it
// against its own origin) against the instance domain, so it's dereferenceable
// by remote fediverse servers. Already-absolute URLs pass through unchanged.
func (s *Service) absoluteURL(u string) string {
	if u == "" || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return strings.TrimRight(s.cfg.InstanceDomain, "/") + "/" + strings.TrimLeft(u, "/")
}

func (s *Service) actorURL(username string) string {
	return strings.TrimRight(s.cfg.InstanceDomain, "/") + "/federation/users/" + username
}

func (s *Service) actorKeyID(username string) string {
	return s.actorURL(username) + "#main-key"
}

// ── Page actor identity (AGORA-115) ──────────────────────────────────────────
//
// A page gets its own ActivityPub actor, distinct from any of its members'
// personal user actors, at a separate /federation/pages/{slug} path so
// inbound object URLs are unambiguous about which kind of local actor they
// name.

type apPage struct {
	ID          string
	Slug        string
	DisplayName string
	Bio         string
	AvatarURL   string
	PubKeyPEM   string
	PrivKeyPEM  string
}

// apEligiblePage mirrors apEligibleUser: requires the instance-wide
// activityPubEnabled() toggle, the page to be public, and the page's own
// activitypub_enabled opt-out (owner-controlled) to be true.
func (s *Service) apEligiblePage(slug string) (*apPage, bool) {
	if !s.activityPubEnabled() {
		return nil, false
	}
	var p apPage
	err := s.db.QueryRow(`
		SELECT id, slug, display_name, bio, avatar_url,
		       federation_public_key, federation_private_key
		FROM pages
		WHERE LOWER(slug) = LOWER($1) AND privacy = 'public' AND activitypub_enabled = true
	`, slug).Scan(&p.ID, &p.Slug, &p.DisplayName, &p.Bio, &p.AvatarURL, &p.PubKeyPEM, &p.PrivKeyPEM)
	if err != nil {
		return nil, false
	}
	return &p, true
}

func (s *Service) pageActorURL(slug string) string {
	return strings.TrimRight(s.cfg.InstanceDomain, "/") + "/federation/pages/" + slug
}

func (s *Service) pageActorKeyID(slug string) string {
	return s.pageActorURL(slug) + "#main-key"
}

// ── Per-user RSA keys ─────────────────────────────────────────────────────────
//
// Standard HTTP Signatures (unlike the custom protocol's single instance-wide
// Ed25519 key) require each actor to have its own key, referenced by a keyId
// URL. We reuse the existing-but-previously-unused users.federation_public_key
// / federation_private_key TEXT columns to store the PEM-encoded pair.

func (s *Service) getOrCreateUserKeyPair(userID, pubPEM, privPEM string) (string, string, *rsa.PrivateKey, error) {
	if pubPEM != "" && privPEM != "" {
		if priv, err := parseRSAPrivateKeyPEM(privPEM); err == nil {
			return pubPEM, privPEM, priv, nil
		}
		// Fall through and regenerate if the stored PEM is somehow unparseable.
	}

	pubPEMOut, privPEMOut, priv, err := generateRSAKeyPairPEM()
	if err != nil {
		return "", "", nil, err
	}

	if _, err := s.db.Exec(`
		UPDATE users SET federation_public_key = $1, federation_private_key = $2 WHERE id = $3
	`, pubPEMOut, privPEMOut, userID); err != nil {
		return "", "", nil, err
	}

	log.Printf("federation: generated new RSA keypair for user %s", userID)
	return pubPEMOut, privPEMOut, priv, nil
}

// generateRSAKeyPairPEM generates a fresh RSA-2048 keypair, PEM-encoded —
// the pure-crypto part shared by getOrCreateUserKeyPair and
// getOrCreatePageKeyPair (AGORA-115), which differ only in which table they
// persist the result to.
func generateRSAKeyPairPEM() (pubPEM, privPEM string, priv *rsa.PrivateKey, err error) {
	priv, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", nil, err
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", nil, err
	}
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))

	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", "", nil, err
	}
	pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))

	return pubPEM, privPEM, priv, nil
}

// getOrCreatePageKeyPair mirrors getOrCreateUserKeyPair but for a Page actor
// (AGORA-115) — a page's federation identity is independent of any member's
// personal user key, so it gets its own RSA-2048 keypair stored on the
// pages row itself.
func (s *Service) getOrCreatePageKeyPair(pageID, pubPEM, privPEM string) (string, string, *rsa.PrivateKey, error) {
	if pubPEM != "" && privPEM != "" {
		if priv, err := parseRSAPrivateKeyPEM(privPEM); err == nil {
			return pubPEM, privPEM, priv, nil
		}
	}

	pubPEMOut, privPEMOut, priv, err := generateRSAKeyPairPEM()
	if err != nil {
		return "", "", nil, err
	}

	if _, err := s.db.Exec(`
		UPDATE pages SET federation_public_key = $1, federation_private_key = $2 WHERE id = $3
	`, pubPEMOut, privPEMOut, pageID); err != nil {
		return "", "", nil, err
	}

	log.Printf("federation: generated new RSA keypair for page %s", pageID)
	return pubPEMOut, privPEMOut, priv, nil
}

// ── WebFinger / host-meta ─────────────────────────────────────────────────────

func (s *Service) WebFinger(w http.ResponseWriter, r *http.Request) {
	resource := r.URL.Query().Get("resource")
	if !strings.HasPrefix(resource, "acct:") {
		writeError(w, 400, "invalid resource")
		return
	}
	acct := strings.TrimPrefix(resource, "acct:")
	parts := strings.SplitN(acct, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, 400, "invalid resource")
		return
	}
	username, domain := parts[0], parts[1]

	localDomain := domainFromURL(s.cfg.InstanceDomain)
	if !strings.EqualFold(domain, localDomain) {
		writeError(w, 404, "not found")
		return
	}

	if u, ok := s.apEligibleUser(username); ok {
		actor := s.actorURL(u.Username)
		profile := strings.TrimRight(s.cfg.InstanceDomain, "/") + "/profile/" + u.Username

		w.Header().Set("Content-Type", "application/jrd+json")
		json.NewEncoder(w).Encode(map[string]any{
			"subject": "acct:" + u.Username + "@" + localDomain,
			"aliases": []string{actor},
			"links": []map[string]string{
				{"rel": "http://webfinger.net/rel/profile-page", "type": "text/html", "href": profile},
				{"rel": "self", "type": "application/activity+json", "href": actor},
			},
		})
		return
	}

	// AGORA-115: fall back to a page if no user matches this handle. WebFinger's
	// namespace doesn't distinguish "user" vs "page" — on a slug/username
	// collision the user wins (checked first above), a documented edge case
	// rather than one this endpoint tries to fully resolve.
	p, ok := s.apEligiblePage(username)
	if !ok {
		writeError(w, 404, "not found")
		return
	}
	actor := s.pageActorURL(p.Slug)
	profile := strings.TrimRight(s.cfg.InstanceDomain, "/") + "/pages/" + p.Slug

	w.Header().Set("Content-Type", "application/jrd+json")
	json.NewEncoder(w).Encode(map[string]any{
		"subject": "acct:" + p.Slug + "@" + localDomain,
		"aliases": []string{actor},
		"links": []map[string]string{
			{"rel": "http://webfinger.net/rel/profile-page", "type": "text/html", "href": profile},
			{"rel": "self", "type": "application/activity+json", "href": actor},
		},
	})
}

func (s *Service) HostMeta(w http.ResponseWriter, r *http.Request) {
	if !s.activityPubEnabled() {
		writeError(w, 404, "not found")
		return
	}
	domain := strings.TrimRight(s.cfg.InstanceDomain, "/")
	w.Header().Set("Content-Type", "application/xrd+xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0">
  <Link rel="lrdd" type="application/xrd+xml" template="%s/.well-known/webfinger?resource={uri}"/>
</XRD>`, domain)
}

// ── Actor document ────────────────────────────────────────────────────────────
//
// Called from GetUser (federation.go) when the request Accept header asks for
// ActivityPub JSON. The legacy flat-JSON response GetUser returns otherwise is
// untouched.

func (s *Service) writeActorObject(w http.ResponseWriter, handle string) {
	u, ok := s.apEligibleUser(handle)
	if !ok {
		writeError(w, 404, "user not found")
		return
	}

	pubPEM, _, _, err := s.getOrCreateUserKeyPair(u.ID, u.PubKeyPEM, u.PrivKeyPEM)
	if err != nil {
		writeError(w, 500, "key error")
		return
	}

	actor := s.actorURL(u.Username)
	obj := map[string]any{
		"@context": []string{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":                 actor,
		"type":               "Person",
		"preferredUsername":  u.Username,
		"name":               u.DisplayName,
		"summary":            u.Bio,
		"inbox":              strings.TrimRight(s.cfg.InstanceDomain, "/") + "/federation/inbox",
		"outbox":             actor + "/outbox",
		"followers":          actor + "/followers",
		"url":                strings.TrimRight(s.cfg.InstanceDomain, "/") + "/profile/" + u.Username,
		"publicKey": map[string]string{
			"id":           actor + "#main-key",
			"owner":        actor,
			"publicKeyPem": pubPEM,
		},
	}
	if u.AvatarURL != "" {
		obj["icon"] = map[string]string{"type": "Image", "url": s.absoluteURL(u.AvatarURL)}
	}

	w.Header().Set("Content-Type", "application/activity+json")
	json.NewEncoder(w).Encode(obj)
}

// ── Outbox ─────────────────────────────────────────────────────────────────────

func (s *Service) Outbox(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
	u, ok := s.apEligibleUser(handle)
	if !ok {
		writeError(w, 404, "user not found")
		return
	}
	actor := s.actorURL(u.Username)

	// MVP: a single page of the most recent public posts, not a fully
	// paginated OrderedCollection — enough for AP crawlers/Mastodon's
	// initial fetch when someone follows this actor.
	rows, err := s.db.Query(`
		SELECT id, content, content_warning, created_at
		FROM posts
		WHERE author_id = $1 AND visibility = 'public' AND parent_id IS NULL
		  AND deleted_at IS NULL AND is_remote = false
		ORDER BY created_at DESC
		LIMIT 20
	`, u.ID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	var items []map[string]any
	for rows.Next() {
		var id, content, contentWarning string
		var createdAt time.Time
		if err := rows.Scan(&id, &content, &contentWarning, &createdAt); err != nil {
			continue
		}
		items = append(items, s.buildCreateActivity(actor, id, content, createdAt, "", contentWarning))
	}
	if items == nil {
		items = []map[string]any{}
	}

	w.Header().Set("Content-Type", "application/activity+json")
	json.NewEncoder(w).Encode(map[string]any{
		"@context":     "https://www.w3.org/ns/activitystreams",
		"id":           actor + "/outbox",
		"type":         "OrderedCollection",
		"totalItems":   len(items),
		"orderedItems": items,
	})
}

// buildCreateActivity builds a Create activity wrapping a Note, used by the
// Outbox (historical posts), BroadcastPublicPost (new top-level posts), and
// DeliverReply (comment replies — inReplyTo set, to targeted at the
// recipient instead of just Public).
// buildNoteObject builds the ActivityPub Note object for a post or comment,
// shared by Create (new post, AGORA-145) and Update (edited post, AGORA-150)
// activities. A non-empty contentWarning maps to Note.summary — the standard
// ActivityPub content-warning mechanism (AGORA-154): Mastodon and other
// fediverse clients show content behind a reveal prompt when it's set.
func (s *Service) buildNoteObject(actor, postID, content string, createdAt time.Time, inReplyTo, contentWarning string) map[string]any {
	objID := actor + "/posts/" + postID
	published := createdAt.UTC().Format(time.RFC3339)
	note := map[string]any{
		"id":           objID,
		"type":         "Note",
		"attributedTo": actor,
		"content":      plainTextToHTML(content),
		"published":    published,
		"to":           []string{"https://www.w3.org/ns/activitystreams#Public"},
		"cc":           []string{actor + "/followers"},
	}
	if inReplyTo != "" {
		note["inReplyTo"] = inReplyTo
	}
	if contentWarning != "" {
		note["summary"] = contentWarning
	}
	// AGORA-152: attach images so they render on Mastodon etc., not just as a
	// caption with no photo. Queried here (rather than threaded through every
	// caller) to keep buildCreateActivity/buildUpdateActivity's signatures stable.
	if images := s.postImageURLs(postID); len(images) > 0 {
		attachments := make([]map[string]any, 0, len(images))
		for _, u := range images {
			attachments = append(attachments, map[string]any{
				"type":      "Image",
				"mediaType": guessImageMediaType(u),
				"url":       u,
			})
		}
		note["attachment"] = attachments
	}
	return note
}

// postImageURLs returns the images attached to a post or comment, resolved
// to absolute URLs for remote consumption. Posts with 2+ images use
// post_photos (AGORA-93); everything else (including comments, which only
// ever have one) uses the single image_url column.
func (s *Service) postImageURLs(postID string) []string {
	rows, err := s.db.Query(`SELECT url FROM post_photos WHERE post_id = $1 ORDER BY position ASC`, postID)
	if err == nil {
		defer rows.Close()
		var urls []string
		for rows.Next() {
			var u string
			if rows.Scan(&u) == nil && u != "" {
				urls = append(urls, s.absoluteURL(u))
			}
		}
		if len(urls) > 0 {
			return urls
		}
	}
	var imageURL string
	s.db.QueryRow(`SELECT image_url FROM posts WHERE id = $1`, postID).Scan(&imageURL)
	if imageURL != "" {
		return []string{s.absoluteURL(imageURL)}
	}
	return nil
}

func guessImageMediaType(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// buildCreateActivity wraps a Note in a Create activity, used by the Outbox
// (historical posts), BroadcastPublicPost (new top-level posts), and
// DeliverReply (comment replies).
func (s *Service) buildCreateActivity(actor, postID, content string, createdAt time.Time, inReplyTo, contentWarning string) map[string]any {
	note := s.buildNoteObject(actor, postID, content, createdAt, inReplyTo, contentWarning)
	objID := actor + "/posts/" + postID
	to := note["to"]
	cc := note["cc"]
	return map[string]any{
		"@context":  "https://www.w3.org/ns/activitystreams",
		"id":        objID + "/activity",
		"type":      "Create",
		"actor":     actor,
		"published": note["published"],
		"to":        to,
		"cc":        cc,
		"object":    note,
	}
}

// buildUpdateActivity wraps the same Note shape in an Update activity, sent
// when a previously-federated post is edited (AGORA-150).
func (s *Service) buildUpdateActivity(actor, postID, content string, createdAt time.Time, inReplyTo, contentWarning string) map[string]any {
	note := s.buildNoteObject(actor, postID, content, createdAt, inReplyTo, contentWarning)
	objID := actor + "/posts/" + postID
	to := note["to"]
	cc := note["cc"]
	return map[string]any{
		"@context":  "https://www.w3.org/ns/activitystreams",
		"id":        fmt.Sprintf("%s/updates/%d", objID, time.Now().UnixNano()),
		"type":      "Update",
		"actor":     actor,
		"published": time.Now().UTC().Format(time.RFC3339),
		"to":        to,
		"cc":        cc,
		"object":    note,
	}
}

func plainTextToHTML(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>")
}

// ── Followers collection ──────────────────────────────────────────────────────
//
// Exposes only totalItems, not the follower list itself — consistent with
// this codebase's privacy-conscious defaults elsewhere (e.g. profile_private).

func (s *Service) Followers(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
	u, ok := s.apEligibleUser(handle)
	if !ok {
		writeError(w, 404, "user not found")
		return
	}

	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM ap_followers WHERE followed_user_id = $1`, u.ID).Scan(&count)

	actor := s.actorURL(u.Username)
	w.Header().Set("Content-Type", "application/activity+json")
	json.NewEncoder(w).Encode(map[string]any{
		"@context":   "https://www.w3.org/ns/activitystreams",
		"id":         actor + "/followers",
		"type":       "OrderedCollection",
		"totalItems": count,
	})
}

// ── Page actor document / outbox / followers (AGORA-115) ─────────────────────
//
// Unlike GetUser, there's no legacy custom-protocol JSON shape to fall back
// to for pages — this endpoint always serves the ActivityPub actor document.

func (s *Service) GetPageActor(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	p, ok := s.apEligiblePage(slug)
	if !ok {
		writeError(w, 404, "page not found")
		return
	}

	pubPEM, _, _, err := s.getOrCreatePageKeyPair(p.ID, p.PubKeyPEM, p.PrivKeyPEM)
	if err != nil {
		writeError(w, 500, "key error")
		return
	}

	actor := s.pageActorURL(p.Slug)
	obj := map[string]any{
		"@context": []string{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":                actor,
		"type":              "Organization",
		"preferredUsername": p.Slug,
		"name":              p.DisplayName,
		"summary":           p.Bio,
		"inbox":             strings.TrimRight(s.cfg.InstanceDomain, "/") + "/federation/inbox",
		"outbox":            actor + "/outbox",
		"followers":         actor + "/followers",
		"url":               strings.TrimRight(s.cfg.InstanceDomain, "/") + "/pages/" + p.Slug,
		"publicKey": map[string]string{
			"id":           actor + "#main-key",
			"owner":        actor,
			"publicKeyPem": pubPEM,
		},
	}
	if p.AvatarURL != "" {
		obj["icon"] = map[string]string{"type": "Image", "url": s.absoluteURL(p.AvatarURL)}
	}

	w.Header().Set("Content-Type", "application/activity+json")
	json.NewEncoder(w).Encode(obj)
}

func (s *Service) PageOutbox(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	p, ok := s.apEligiblePage(slug)
	if !ok {
		writeError(w, 404, "page not found")
		return
	}
	actor := s.pageActorURL(p.Slug)

	// Page posts are always visibility='public' (enforced at creation in
	// pages.CreatePost), so unlike the user outbox there's no per-post
	// privacy check needed here.
	rows, err := s.db.Query(`
		SELECT id, content, content_warning, created_at
		FROM posts
		WHERE page_id = $1 AND parent_id IS NULL AND deleted_at IS NULL AND is_remote = false
		ORDER BY created_at DESC
		LIMIT 20
	`, p.ID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	var items []map[string]any
	for rows.Next() {
		var id, content, contentWarning string
		var createdAt time.Time
		if err := rows.Scan(&id, &content, &contentWarning, &createdAt); err != nil {
			continue
		}
		items = append(items, s.buildCreateActivity(actor, id, content, createdAt, "", contentWarning))
	}
	if items == nil {
		items = []map[string]any{}
	}

	w.Header().Set("Content-Type", "application/activity+json")
	json.NewEncoder(w).Encode(map[string]any{
		"@context":     "https://www.w3.org/ns/activitystreams",
		"id":           actor + "/outbox",
		"type":         "OrderedCollection",
		"totalItems":   len(items),
		"orderedItems": items,
	})
}

func (s *Service) PageFollowers(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	p, ok := s.apEligiblePage(slug)
	if !ok {
		writeError(w, 404, "page not found")
		return
	}

	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM page_remote_subscribers WHERE page_id = $1`, p.ID).Scan(&count)

	actor := s.pageActorURL(p.Slug)
	w.Header().Set("Content-Type", "application/activity+json")
	json.NewEncoder(w).Encode(map[string]any{
		"@context":   "https://www.w3.org/ns/activitystreams",
		"id":         actor + "/followers",
		"type":       "OrderedCollection",
		"totalItems": count,
	})
}

// ── Inbound Follow / Undo(Follow) ─────────────────────────────────────────────

// handleStandardInbox is reached from Inbox (federation.go) once the payload
// has been identified as a standard ActivityPub activity rather than the
// legacy custom-protocol shape. It verifies the HTTP Signature (not the old
// embedded-JSON-field Ed25519 scheme) before doing anything else.
func (s *Service) handleStandardInbox(w http.ResponseWriter, r *http.Request, body []byte) {
	verifiedActor, err := verifyInboundSignature(r, body)
	if err != nil {
		log.Printf("federation: ap signature verification failed: %v", err)
		writeError(w, 401, "invalid signature")
		return
	}

	// verifiedActor (derived from the signature's keyId, above) is the
	// trustworthy signer identity — the body's own "actor"/"attributedTo"
	// fields are not cryptographically tied to the signature and are only
	// used below where they don't need to be trusted on their own.
	var a struct {
		ID     string          `json:"id"`
		Type   string          `json:"type"`
		Object json.RawMessage `json:"object"`
	}
	if err := json.Unmarshal(body, &a); err != nil {
		writeError(w, 400, "invalid activity")
		return
	}

	switch a.Type {
	case "Follow":
		s.handleInboundFollow(a.ID, verifiedActor, a.Object)
	case "Undo":
		var inner struct {
			Type   string          `json:"type"`
			Object json.RawMessage `json:"object"`
		}
		json.Unmarshal(a.Object, &inner)
		switch inner.Type {
		case "Follow":
			s.handleInboundUndoFollow(verifiedActor, inner.Object)
		case "Like":
			s.handleInboundUndoLike(verifiedActor, inner.Object)
		case "Announce":
			s.handleInboundUndoAnnounce(verifiedActor, inner.Object)
		}
	case "Create":
		s.handleInboundCreate(verifiedActor, a.Object)
	case "Like":
		s.handleInboundLike(verifiedActor, a.Object)
	case "Announce":
		s.handleInboundAnnounce(a.ID, verifiedActor, a.Object)
	case "Accept":
		s.handleInboundAcceptFollow(verifiedActor, a.Object)
	case "Reject":
		s.handleInboundRejectFollow(verifiedActor, a.Object)
	}

	writeJSON(w, 202, map[string]string{"message": "accepted"})
}

func (s *Service) handleInboundFollow(followID, followerActor string, objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" {
		return
	}

	domain := domainFromURL(followerActor)
	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, domain).Scan(&status)
	if status == "blocked" {
		return
	}

	if username := usernameFromActorURL(objectURL, s.cfg.InstanceDomain); username != "" {
		s.handleInboundFollowUser(followID, followerActor, objectURL, username)
		return
	}
	if slug := pageSlugFromActorURL(objectURL, s.cfg.InstanceDomain); slug != "" {
		s.handleInboundFollowPage(followID, followerActor, objectURL, slug)
	}
}

func (s *Service) handleInboundFollowUser(followID, followerActor, objectURL, username string) {
	u, ok := s.apEligibleUser(username)
	if !ok {
		return
	}

	profile, err := fetchActorProfile(followerActor)
	if err != nil || profile.Inbox == "" {
		return
	}
	followerInbox := profile.Inbox

	s.db.Exec(`
		INSERT INTO ap_followers (followed_user_id, follower_actor_url, follower_inbox_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (followed_user_id, follower_actor_url) DO UPDATE SET follower_inbox_url = $3
	`, u.ID, followerActor, followerInbox)

	followObj := map[string]any{
		"type":   "Follow",
		"actor":  followerActor,
		"object": objectURL,
	}
	if followID != "" {
		followObj["id"] = followID
	}
	accept := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       s.actorURL(u.Username) + fmt.Sprintf("/accepts/%d", time.Now().UnixNano()),
		"type":     "Accept",
		"actor":    s.actorURL(u.Username),
		"object":   followObj,
	}
	s.enqueueAPDelivery(u.ID, followerInbox, accept)
}

// handleInboundFollowPage mirrors handleInboundFollowUser (AGORA-115), except
// it records the subscription in page_remote_subscribers and signs the
// Accept with the page's own key rather than any member's.
func (s *Service) handleInboundFollowPage(followID, followerActor, objectURL, slug string) {
	p, ok := s.apEligiblePage(slug)
	if !ok {
		return
	}

	profile, err := fetchActorProfile(followerActor)
	if err != nil || profile.Inbox == "" {
		return
	}
	followerInbox := profile.Inbox

	s.db.Exec(`
		INSERT INTO page_remote_subscribers (page_id, follower_actor_url, follower_inbox_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (page_id, follower_actor_url) DO UPDATE SET follower_inbox_url = $3
	`, p.ID, followerActor, followerInbox)

	followObj := map[string]any{
		"type":   "Follow",
		"actor":  followerActor,
		"object": objectURL,
	}
	if followID != "" {
		followObj["id"] = followID
	}
	actor := s.pageActorURL(p.Slug)
	accept := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       actor + fmt.Sprintf("/accepts/%d", time.Now().UnixNano()),
		"type":     "Accept",
		"actor":    actor,
		"object":   followObj,
	}
	s.enqueuePageAPDelivery(p.ID, followerInbox, accept)
}

func (s *Service) handleInboundUndoFollow(followerActor string, objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" {
		return
	}
	if username := usernameFromActorURL(objectURL, s.cfg.InstanceDomain); username != "" {
		var userID string
		s.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = LOWER($1) AND is_remote = false`, username).Scan(&userID)
		if userID == "" {
			return
		}
		s.db.Exec(`DELETE FROM ap_followers WHERE followed_user_id = $1 AND follower_actor_url = $2`, userID, followerActor)
		return
	}
	if slug := pageSlugFromActorURL(objectURL, s.cfg.InstanceDomain); slug != "" {
		var pageID string
		s.db.QueryRow(`SELECT id FROM pages WHERE LOWER(slug) = LOWER($1)`, slug).Scan(&pageID)
		if pageID == "" {
			return
		}
		s.db.Exec(`DELETE FROM page_remote_subscribers WHERE page_id = $1 AND follower_actor_url = $2`, pageID, followerActor)
	}
}

// ── Inbound Accept/Reject(Follow) — outbound-follow confirmation (AGORA-146) ──
//
// The remote server echoes the original Follow's "actor" (us) and "object"
// (them) back inside the Accept/Reject — no separately-stored follow ID is
// needed to match it to the right ap_following row: follow.Actor tells us
// which local user's Follow this confirms, and verifiedActor (the Accept's
// own signer) is who's confirming it, which must be followed_actor_url.

func (s *Service) handleInboundAcceptFollow(verifiedActor string, objectRaw json.RawMessage) {
	var follow struct {
		Actor string `json:"actor"`
	}
	if err := json.Unmarshal(objectRaw, &follow); err != nil || follow.Actor == "" {
		return
	}
	username := usernameFromActorURL(follow.Actor, s.cfg.InstanceDomain)
	if username == "" {
		return
	}
	var userID string
	s.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = LOWER($1) AND is_remote = false`, username).Scan(&userID)
	if userID == "" {
		return
	}
	s.db.Exec(`UPDATE ap_following SET accepted = true WHERE follower_user_id = $1 AND followed_actor_url = $2`, userID, verifiedActor)
}

// handleInboundRejectFollow removes the pending ap_following row so the UI
// reverts to "not following" and the user can retry or give up.
func (s *Service) handleInboundRejectFollow(verifiedActor string, objectRaw json.RawMessage) {
	var follow struct {
		Actor string `json:"actor"`
	}
	if err := json.Unmarshal(objectRaw, &follow); err != nil || follow.Actor == "" {
		return
	}
	username := usernameFromActorURL(follow.Actor, s.cfg.InstanceDomain)
	if username == "" {
		return
	}
	var userID string
	s.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = LOWER($1) AND is_remote = false`, username).Scan(&userID)
	if userID == "" {
		return
	}
	s.db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1 AND followed_actor_url = $2`, userID, verifiedActor)
}

// ── Inbound Create (replies into threads we own, or a followed account's own
//    top-level posts — AGORA-146) ─────────────────────────────────────────────
func (s *Service) handleInboundCreate(verifiedActor string, objectRaw json.RawMessage) {
	var note struct {
		ID           string `json:"id"`
		AttributedTo string `json:"attributedTo"`
		Content      string `json:"content"`
		InReplyTo    string `json:"inReplyTo"`
		Summary      string `json:"summary"` // AGORA-154: content-warning text, if any
		Attachment   []struct {
			MediaType string `json:"mediaType"`
			URL       string `json:"url"`
		} `json:"attachment"`
	}
	if err := json.Unmarshal(objectRaw, &note); err != nil {
		return
	}
	// attributedTo must match the cryptographically verified signer — an
	// activity envelope signed by A cannot claim to contain a Note by B.
	if note.AttributedTo == "" || note.AttributedTo != verifiedActor {
		return
	}
	if note.ID == "" {
		return
	}

	// AGORA-148: an admin-blocked instance can't Follow, but until now could
	// still reply into threads — apply the same block-list check Follow uses.
	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, domainFromURL(verifiedActor)).Scan(&status)
	if status == "blocked" {
		return
	}

	var imageURLs []string
	for _, a := range note.Attachment {
		if strings.HasPrefix(a.MediaType, "image/") && a.URL != "" {
			imageURLs = append(imageURLs, a.URL)
		}
	}

	// AGORA-146: no inReplyTo means this isn't a reply into a thread we
	// own — it's either unrelated top-level fediverse noise (dropped) or a
	// followed account's own post (ingested), handled entirely separately
	// from the reply-threading path below.
	if note.InReplyTo == "" {
		s.ingestFollowedPost(verifiedActor, note.ID, note.Content, note.Summary, imageURLs)
		return
	}

	parentID, rootPostID, visibility, postAuthorID, ok := s.resolveReplyTarget(note.InReplyTo)
	if !ok {
		return
	}

	// Re-check the thread is still eligible now, not just when it was created —
	// mirrors the same defense-in-depth re-check BroadcastPublicPost does.
	var profilePrivate, apEnabled bool
	if err := s.db.QueryRow(`SELECT profile_private, activitypub_enabled FROM users WHERE id = $1`, postAuthorID).
		Scan(&profilePrivate, &apEnabled); err != nil || profilePrivate || !apEnabled || visibility != "public" {
		return
	}

	remoteUserID, err := s.getOrCreateRemoteAPUser(verifiedActor)
	if err != nil || remoteUserID == "" {
		return
	}

	domain := domainFromURL(note.ID)
	var commentID string
	err = s.db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, parent_id, is_remote, remote_post_id, remote_instance, content_warning)
		VALUES ($1, $2, $3, $4, true, $5, $6, $7)
		ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO NOTHING
		RETURNING id
	`, remoteUserID, htmlToPlainText(note.Content), visibility, parentID, note.ID, domain, htmlToPlainText(note.Summary)).Scan(&commentID)
	if err != nil {
		// ON CONFLICT DO NOTHING with a RETURNING clause yields sql.ErrNoRows
		// when the row already existed — expected on redelivery, not an error.
		return
	}
	s.storeInboundImages(commentID, imageURLs)

	if s.notif != nil {
		if postAuthorID != remoteUserID {
			s.notif.Create(postAuthorID, remoteUserID, "post_comment", rootPostID, "")
		}
	}
}

// ingestFollowedPost handles a followed account's own top-level post
// (AGORA-146) — distinct from the reply-threading path above, gated on
// whether any local user actively follows this actor rather than on the
// post being a reply into a thread Agora already owns. Ingested once
// regardless of how many local users follow the actor (idempotent insert,
// same redelivery-tolerant pattern as the reply path) — per-viewer
// visibility is enforced later at custom-feed query time (execCustomFeed),
// not here, since a single ingested post is shared by every local follower.
func (s *Service) ingestFollowedPost(actorURL, noteID, content, summary string, imageURLs []string) {
	var followed bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM ap_following WHERE followed_actor_url = $1 AND accepted = true)`, actorURL).Scan(&followed)
	if !followed {
		return
	}

	remoteUserID, err := s.getOrCreateRemoteAPUser(actorURL)
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
	`, remoteUserID, htmlToPlainText(content), noteID, domain, htmlToPlainText(summary)).Scan(&postID)
	if err != nil {
		// ON CONFLICT DO NOTHING + RETURNING yields sql.ErrNoRows on
		// redelivery (or a second local follower's copy of the same
		// delivery, since Agora presents one shared inbox per actor with no
		// sharedInbox optimization declared) — expected, not an error.
		return
	}
	s.storeInboundImages(postID, imageURLs)
}

// storeInboundImages persists a remote Note's image attachments — a single
// URL into posts.image_url, or all of them into post_photos plus the first
// into image_url too, mirroring how pages.CreatePost and postImageURLs'
// read-side already treat the multi-image case.
func (s *Service) storeInboundImages(postID string, imageURLs []string) {
	if len(imageURLs) == 0 {
		return
	}
	s.db.Exec(`UPDATE posts SET image_url = $1 WHERE id = $2`, imageURLs[0], postID)
	if len(imageURLs) > 1 {
		for i, u := range imageURLs {
			s.db.Exec(`INSERT INTO post_photos (post_id, url, position) VALUES ($1, $2, $3)`, postID, u, i)
		}
	}
}

// resolveReplyTarget resolves an inReplyTo URL to a local insertion point,
// applying the same two-level depth cap CreateComment enforces (feed.go)
// so inbound replies can't create threads deeper than the UI supports.
// inReplyTo may point at either one of our own post/comment AP object URLs,
// or a previously-ingested remote reply (looked up by remote_post_id).
func (s *Service) resolveReplyTarget(inReplyTo string) (parentID, rootPostID, visibility, postAuthorID string, ok bool) {
	targetID := localPostIDFromURL(inReplyTo, s.cfg.InstanceDomain)
	if targetID == "" {
		s.db.QueryRow(`SELECT id FROM posts WHERE remote_post_id = $1 AND is_remote = true`, inReplyTo).Scan(&targetID)
	}
	if targetID == "" {
		return "", "", "", "", false
	}

	var targetParentID *string
	if err := s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, targetID).
		Scan(&targetParentID); err != nil {
		return "", "", "", "", false
	}

	if targetParentID == nil {
		// Target is a top-level post — the reply becomes a depth-0 comment.
		rootPostID = targetID
	} else {
		// Target is itself a comment — walk up one more level. If ITS parent
		// also has a parent, target is already as deep as the UI allows.
		var grandParentID *string
		s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, *targetParentID).Scan(&grandParentID)
		if grandParentID != nil {
			return "", "", "", "", false
		}
		rootPostID = *targetParentID
	}
	parentID = targetID

	if err := s.db.QueryRow(`SELECT visibility, author_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, rootPostID).
		Scan(&visibility, &postAuthorID); err != nil || postAuthorID == "" {
		return "", "", "", "", false
	}
	return parentID, rootPostID, visibility, postAuthorID, true
}

// localPostIDFromURL extracts the trailing post/comment UUID from one of our
// own AP object URLs — either a user actor's
// (.../federation/users/{username}/posts/{id}[/activity]) or a page actor's
// (.../federation/pages/{slug}/posts/{id}[/activity]), added by AGORA-115 so
// future inbound reply/like support for pages doesn't need to touch this
// parsing again — returning "" for anything that isn't ours.
func localPostIDFromURL(u, instanceDomain string) string {
	for _, prefix := range []string{"/federation/users/", "/federation/pages/"} {
		base := strings.TrimRight(instanceDomain, "/") + prefix
		if !strings.HasPrefix(u, base) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(u, base), "/")
		if len(parts) < 3 || parts[1] != "posts" {
			return ""
		}
		return strings.SplitN(parts[2], "#", 2)[0]
	}
	return ""
}

// getOrCreateRemoteAPUser returns the local stub user id for a remote AP
// actor, fetching and caching their profile on first sight. Distinct from
// getOrCreateRemoteUser (federation.go), which is the old custom protocol's
// stub creation via its own non-standard profile endpoint.
func (s *Service) getOrCreateRemoteAPUser(actorURL string) (string, error) {
	var id string
	s.db.QueryRow(`SELECT id FROM users WHERE ap_actor_url = $1`, actorURL).Scan(&id)
	if id != "" {
		return id, nil
	}

	profile, err := fetchActorProfile(actorURL)
	if err != nil {
		return "", err
	}
	return s.upsertRemoteAPUser(actorURL, profile)
}

// upsertRemoteAPUser is getOrCreateRemoteAPUser's cache-miss body, split out
// so a caller that already has a freshly-fetched profile (e.g.
// FollowFediverseAccount, which fetches it anyway to get the inbox URL)
// doesn't need to fetch it again over the network a second time.
func (s *Service) upsertRemoteAPUser(actorURL string, profile *remoteActorProfile) (string, error) {
	var id string
	domain := domainFromURL(actorURL)
	handle := profile.PreferredUsername
	if handle == "" {
		handle = "user"
	}
	displayName := profile.Name
	if displayName == "" {
		displayName = handle + "@" + domain
	}
	syntheticUsername := handle + "@" + domain

	err := s.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, avatar_url, bio,
		                   email_verified, is_remote, remote_user_id, remote_instance, remote_synced_at,
		                   ap_actor_url, ap_inbox_url)
		VALUES ($1, $1, '', $2, $3, $4, true, true, $5, $6, NOW(), $7, $8)
		ON CONFLICT (ap_actor_url) WHERE ap_actor_url != '' DO UPDATE
		  SET display_name = $2, avatar_url = $3, bio = $4, remote_synced_at = NOW(), ap_inbox_url = $8
		RETURNING id
	`, syntheticUsername, displayName, profile.IconURL, profile.Summary,
		handle, domain, actorURL, profile.Inbox,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// htmlToPlainText converts the (sanitized) HTML content fediverse software
// sends in a Note's "content" field into plain text, consistent with how
// Agora's own renderContent expects plain text and does its own @mention/URL
// linkification. Good enough for the small tag set Mastodon etc. emit
// (p, br, a, span, strong, em, ...); not a general HTML sanitizer.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var htmlBlockBreakRe = regexp.MustCompile(`(?i)<(br|/p|/li)\s*/?>`)

func htmlToPlainText(s string) string {
	s = htmlBlockBreakRe.ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

// ── Inbound Like / Announce (favorites / boosts) ──────────────────────────────

// resolveFederatableTarget resolves an object URL to one of our own local
// posts the given actor is allowed to interact with — not blocked, post
// still exists/public, author still ap-enabled. Shared by inbound Like and
// Announce, which (unlike Create) only ever target a post directly, never a
// remote-comment reply chain, so this is simpler than resolveReplyTarget.
func (s *Service) resolveFederatableTarget(verifiedActor, objectURL string) (postID, postAuthorID string, ok bool) {
	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, domainFromURL(verifiedActor)).Scan(&status)
	if status == "blocked" {
		return "", "", false
	}
	postID = localPostIDFromURL(objectURL, s.cfg.InstanceDomain)
	if postID == "" {
		return "", "", false
	}
	var visibility string
	var profilePrivate, apEnabled bool
	err := s.db.QueryRow(`
		SELECT p.author_id, p.visibility, u.profile_private, u.activitypub_enabled
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.deleted_at IS NULL
	`, postID).Scan(&postAuthorID, &visibility, &profilePrivate, &apEnabled)
	if err != nil || visibility != "public" || profilePrivate || !apEnabled {
		return "", "", false
	}
	return postID, postAuthorID, true
}

func (s *Service) handleInboundLike(verifiedActor string, objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" {
		return
	}
	postID, postAuthorID, ok := s.resolveFederatableTarget(verifiedActor, objectURL)
	if !ok {
		return
	}
	remoteUserID, err := s.getOrCreateRemoteAPUser(verifiedActor)
	if err != nil || remoteUserID == "" {
		return
	}

	// AGORA-157: write to reactions (reaction_type='like'), not the legacy
	// likes table — the UI's like button (ReactPost) writes there, and
	// enrichReactions (feed.go) only falls back to likes for a post that has
	// zero reactions rows at all, so a Like landing in likes is invisible to
	// the reaction count/list for virtually every real post. Mirrors
	// ReactPost's own upsert + legacy-row cleanup exactly.
	res, err := s.db.Exec(`
		INSERT INTO reactions (user_id, post_id, reaction_type)
		VALUES ($1, $2, 'like')
		ON CONFLICT (user_id, post_id) DO NOTHING
	`, remoteUserID, postID)
	if err != nil {
		return
	}
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, remoteUserID, postID)
	if n, _ := res.RowsAffected(); n == 0 {
		return // already liked — redelivery, don't re-notify
	}

	if s.notif != nil && postAuthorID != remoteUserID {
		var parentID *string
		s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1`, postID).Scan(&parentID)
		notifType := "post_like"
		if parentID != nil {
			notifType = "comment_like"
		}
		s.notif.Create(postAuthorID, remoteUserID, notifType, postID, "")
	}
}

func (s *Service) handleInboundUndoLike(verifiedActor string, objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" {
		return
	}
	postID := localPostIDFromURL(objectURL, s.cfg.InstanceDomain)
	if postID == "" {
		return
	}
	var remoteUserID string
	s.db.QueryRow(`SELECT id FROM users WHERE ap_actor_url = $1`, verifiedActor).Scan(&remoteUserID)
	if remoteUserID == "" {
		return
	}
	// Only remove the reaction if it's still the 'like' we created — a remote
	// actor's Undo(Like) shouldn't be able to clear a since-changed reaction.
	s.db.Exec(`DELETE FROM reactions WHERE user_id = $1 AND post_id = $2 AND reaction_type = 'like'`, remoteUserID, postID)
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, remoteUserID, postID)
}

func (s *Service) handleInboundAnnounce(activityID, verifiedActor string, objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" || activityID == "" {
		return
	}
	postID, postAuthorID, ok := s.resolveFederatableTarget(verifiedActor, objectURL)
	if !ok {
		return
	}
	remoteUserID, err := s.getOrCreateRemoteAPUser(verifiedActor)
	if err != nil || remoteUserID == "" {
		return
	}

	var repostID string
	err = s.db.QueryRow(`
		INSERT INTO posts (author_id, visibility, repost_of_id, is_remote, remote_post_id, remote_instance)
		VALUES ($1, 'public', $2, true, $3, $4)
		ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO NOTHING
		RETURNING id
	`, remoteUserID, postID, activityID, domainFromURL(activityID)).Scan(&repostID)
	if err != nil {
		// ON CONFLICT DO NOTHING + RETURNING yields sql.ErrNoRows on
		// redelivery — expected, not an error.
		return
	}

	if s.notif != nil && postAuthorID != remoteUserID {
		s.notif.Create(postAuthorID, remoteUserID, "post_repost", postID, "")
	}
}

func (s *Service) handleInboundUndoAnnounce(verifiedActor string, objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" {
		return
	}
	postID := localPostIDFromURL(objectURL, s.cfg.InstanceDomain)
	if postID == "" {
		return
	}
	var remoteUserID string
	s.db.QueryRow(`SELECT id FROM users WHERE ap_actor_url = $1`, verifiedActor).Scan(&remoteUserID)
	if remoteUserID == "" {
		return
	}
	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE author_id = $1 AND repost_of_id = $2 AND is_remote = true AND deleted_at IS NULL`,
		remoteUserID, postID)
}

// usernameFromActorURL extracts the username from one of our own actor URLs
// (.../federation/users/{username}), rejecting anything that isn't ours.
func usernameFromActorURL(u, instanceDomain string) string {
	prefix := strings.TrimRight(instanceDomain, "/") + "/federation/users/"
	if !strings.HasPrefix(u, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(u, prefix)
	rest = strings.SplitN(rest, "/", 2)[0]
	rest = strings.SplitN(rest, "#", 2)[0]
	return rest
}

// pageSlugFromActorURL mirrors usernameFromActorURL for page actors
// (AGORA-115): (.../federation/pages/{slug}), rejecting anything that isn't ours.
func pageSlugFromActorURL(u, instanceDomain string) string {
	prefix := strings.TrimRight(instanceDomain, "/") + "/federation/pages/"
	if !strings.HasPrefix(u, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(u, prefix)
	rest = strings.SplitN(rest, "/", 2)[0]
	rest = strings.SplitN(rest, "#", 2)[0]
	return rest
}

// remoteActorProfile is what we need from a remote actor document, whether
// we're just resolving their inbox (Follow accept) or creating a full
// remote-user stub for them (inbound replies).
type remoteActorProfile struct {
	Inbox             string
	PreferredUsername string
	Name              string
	Summary           string
	IconURL           string
}

// fetchActorProfile dereferences a remote actor URL (via the SSRF-safe
// fedHTTPClient) and returns the fields we care about. Unsigned — most
// fediverse software (Mastodon et al.) serves actor documents to anonymous
// GETs, but some (Threads, and Mastodon instances with AUTHORIZED_FETCH
// enabled) require every actor fetch to carry a valid HTTP Signature and
// return a blanket 404 otherwise. Callers that have a local user context to
// sign as should prefer fetchActorProfileSigned.
func fetchActorProfile(actorURL string) (*remoteActorProfile, error) {
	if !strings.HasPrefix(actorURL, "https://") {
		return nil, fmt.Errorf("actor url must be https")
	}
	req, err := http.NewRequest(http.MethodGet, actorURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/activity+json")
	return doActorProfileFetch(req)
}

// fetchActorProfileSigned is fetchActorProfile's signed counterpart, needed
// for instances (Threads chief among them) that enforce "authorized fetch" —
// requiring a valid HTTP Signature on every actor GET, not just inbound
// activity deliveries. Signs as userID's own actor, generating a keypair for
// them first if they don't have one yet (mirrors deliverAPActivity's own
// lazy key handling for outbound POSTs). An empty-body GET still needs a
// Digest header for our own signedHeaderList to be self-consistent; SHA-256
// of the empty string is the standard value fediverse verifiers expect here.
func (s *Service) fetchActorProfileSigned(userID, actorURL string) (*remoteActorProfile, error) {
	if !strings.HasPrefix(actorURL, "https://") {
		return nil, fmt.Errorf("actor url must be https")
	}

	var username, pubPEM, privPEM string
	if err := s.db.QueryRow(`SELECT username, federation_public_key, federation_private_key FROM users WHERE id = $1`, userID).
		Scan(&username, &pubPEM, &privPEM); err != nil {
		return nil, err
	}
	_, _, privKey, err := s.getOrCreateUserKeyPair(userID, pubPEM, privPEM)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, actorURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/activity+json")
	if err := signRequest(req, s.actorKeyID(username), privKey, []byte{}); err != nil {
		return nil, err
	}
	return doActorProfileFetch(req)
}

func doActorProfileFetch(req *http.Request) (*remoteActorProfile, error) {
	resp, err := fedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("actor fetch returned %d", resp.StatusCode)
	}
	var actor struct {
		Inbox             string `json:"inbox"`
		PreferredUsername string `json:"preferredUsername"`
		Name              string `json:"name"`
		Summary           string `json:"summary"`
		Icon              struct {
			URL string `json:"url"`
		} `json:"icon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&actor); err != nil {
		return nil, err
	}
	if actor.Inbox == "" {
		return nil, fmt.Errorf("actor has no inbox")
	}
	return &remoteActorProfile{
		Inbox:             actor.Inbox,
		PreferredUsername: actor.PreferredUsername,
		Name:              actor.Name,
		Summary:           actor.Summary,
		IconURL:           actor.Icon.URL,
	}, nil
}

// resolveActorURLViaWebFinger is the client-side counterpart of the WebFinger
// HTTP handler above (AGORA-146) — that one answers WebFinger queries about
// our own local actors; this one queries a REMOTE instance's WebFinger
// endpoint to turn "user@instance.tld" into that user's actor URL, the first
// hop of resolving a typed-in fediverse handle (the second hop is
// fetchActorProfile, above).
func resolveActorURLViaWebFinger(handle, domain string) (string, error) {
	if !isValidInstanceHost(domain) {
		return "", fmt.Errorf("invalid instance host")
	}
	resource := url.QueryEscape("acct:" + handle + "@" + domain)
	req, err := http.NewRequest(http.MethodGet, "https://"+domain+"/.well-known/webfinger?resource="+resource, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/jrd+json, application/json")
	resp, err := fedHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("webfinger returned %d", resp.StatusCode)
	}
	var jrd struct {
		Links []struct {
			Rel  string `json:"rel"`
			Type string `json:"type"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jrd); err != nil {
		return "", err
	}
	for _, l := range jrd.Links {
		if l.Rel == "self" && (l.Type == "application/activity+json" || l.Type == "application/ld+json") && l.Href != "" {
			return l.Href, nil
		}
	}
	return "", fmt.Errorf("no self link found for %s@%s", handle, domain)
}

// ── Search / resolve a fediverse handle (AGORA-146) ───────────────────────────

// APLookup resolves a typed-in fediverse handle (user@instance.tld) or a
// direct profile URL to a preview card — the "search" AGORA-146 actually
// needs, since ActivityPub has no fediverse-wide search API. Authed-only
// (registered via RegisterAuthedRoutes), matching LookupUser's rationale.
func (s *Service) APLookup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	raw := strings.TrimSpace(r.URL.Query().Get("handle"))
	if raw == "" {
		writeError(w, 400, "handle required")
		return
	}
	raw = strings.TrimPrefix(raw, "@")

	var actorURL string
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		actorURL = raw
	} else {
		parts := strings.SplitN(raw, "@", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			writeError(w, 400, "expected user@instance.tld or a profile URL")
			return
		}
		var err error
		actorURL, err = resolveActorURLViaWebFinger(parts[0], parts[1])
		if err != nil {
			writeError(w, 404, "could not resolve handle")
			return
		}
	}

	domain := domainFromURL(actorURL)
	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, domain).Scan(&status)
	if status == "blocked" {
		writeError(w, 404, "instance is blocked")
		return
	}

	// Signed: Threads and any Mastodon instance running with
	// AUTHORIZED_FETCH require a valid HTTP Signature on every actor GET,
	// not just inbound activity deliveries, and return a blanket 404
	// otherwise — an anonymous fetchActorProfile would fail here even for a
	// perfectly valid handle.
	profile, err := s.fetchActorProfileSigned(userID, actorURL)
	if err != nil {
		writeError(w, 404, "could not resolve actor")
		return
	}

	writeJSON(w, 200, map[string]any{
		"actor_url":          actorURL,
		"preferred_username": profile.PreferredUsername,
		"name":               profile.Name,
		"summary":            profile.Summary,
		"icon_url":           profile.IconURL,
		"instance":           domain,
	})
}

// ── Outbound Follow / Unfollow of a remote fediverse account (AGORA-146) ──────

// FollowFediverseAccount sends a Follow from the caller's own actor to a
// remote actor resolved via APLookup. The inbox is always re-derived
// server-side (fetchActorProfile) rather than trusting a client-supplied
// value, since a spoofed inbox URL would redirect delivery (and thus the
// remote server's confirmation) wherever an attacker wants.
func (s *Service) FollowFediverseAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		ActorURL string `json:"actor_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ActorURL == "" {
		writeError(w, 400, "actor_url required")
		return
	}
	if !strings.HasPrefix(req.ActorURL, "https://") {
		writeError(w, 400, "actor_url must be https")
		return
	}

	domain := domainFromURL(req.ActorURL)
	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, domain).Scan(&status)
	if status == "blocked" {
		writeError(w, 403, "instance is blocked")
		return
	}

	profile, err := s.fetchActorProfileSigned(userID, req.ActorURL)
	if err != nil || profile.Inbox == "" {
		writeError(w, 404, "could not resolve actor")
		return
	}

	var username string
	s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
	if username == "" {
		writeError(w, 404, "user not found")
		return
	}

	if _, err := s.db.Exec(`
		INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted)
		VALUES ($1, $2, $3, false)
		ON CONFLICT (follower_user_id, followed_actor_url) DO UPDATE SET followed_inbox_url = $3
	`, userID, req.ActorURL, profile.Inbox); err != nil {
		writeError(w, 500, "could not follow")
		return
	}

	// Eagerly create the local stub for this remote actor now, using the
	// profile we already fetched above — otherwise it wouldn't exist until
	// their first post arrives, leaving the fediverse_account custom-feed
	// filter picker with nothing to offer for an account that's followed
	// but hasn't posted anything new since.
	s.upsertRemoteAPUser(req.ActorURL, profile)

	actor := s.actorURL(username)
	follow := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       actor + fmt.Sprintf("/follows/%d", time.Now().UnixNano()),
		"type":     "Follow",
		"actor":    actor,
		"object":   req.ActorURL,
	}
	s.enqueueAPDelivery(userID, profile.Inbox, follow)

	writeJSON(w, 201, map[string]string{"message": "follow requested"})
}

// UnfollowFediverseAccount deletes the local follow record and sends an
// outbound Undo(Follow) so the remote server actually stops delivering.
func (s *Service) UnfollowFediverseAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var actorURL, inboxURL string
	if err := s.db.QueryRow(`
		SELECT followed_actor_url, followed_inbox_url FROM ap_following WHERE id = $1 AND follower_user_id = $2
	`, id, userID).Scan(&actorURL, &inboxURL); err != nil {
		writeError(w, 404, "not found")
		return
	}
	s.db.Exec(`DELETE FROM ap_following WHERE id = $1 AND follower_user_id = $2`, id, userID)

	var username string
	s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
	if username != "" && inboxURL != "" {
		actor := s.actorURL(username)
		undo := map[string]any{
			"@context": "https://www.w3.org/ns/activitystreams",
			"id":       actor + fmt.Sprintf("/undos/%d", time.Now().UnixNano()),
			"type":     "Undo",
			"actor":    actor,
			"object": map[string]any{
				"type":   "Follow",
				"actor":  actor,
				"object": actorURL,
			},
		}
		s.enqueueAPDelivery(userID, inboxURL, undo)
	}

	writeJSON(w, 200, map[string]string{"message": "unfollowed"})
}

// ListFollowing returns the caller's fediverse follows, joined with the
// cached remote-actor profile (populated by getOrCreateRemoteAPUser the
// first time that actor's posts are ingested) for display and for the
// FeedBuilderModal's fediverse_account picker.
func (s *Service) ListFollowing(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	rows, err := s.db.Query(`
		SELECT af.id, af.followed_actor_url, af.accepted, af.created_at,
		       COALESCE(u.id::text, ''), COALESCE(u.username, ''), COALESCE(u.display_name, ''),
		       COALESCE(u.avatar_url, ''), COALESCE(u.remote_instance, '')
		FROM ap_following af
		LEFT JOIN users u ON u.ap_actor_url = af.followed_actor_url
		WHERE af.follower_user_id = $1
		ORDER BY af.created_at DESC
	`, userID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type followingEntry struct {
		ID          string `json:"id"`
		ActorURL    string `json:"actor_url"`
		Accepted    bool   `json:"accepted"`
		CreatedAt   string `json:"created_at"`
		UserID      string `json:"user_id,omitempty"`
		Username    string `json:"username,omitempty"`
		DisplayName string `json:"display_name,omitempty"`
		AvatarURL   string `json:"avatar_url,omitempty"`
		Instance    string `json:"instance,omitempty"`
	}
	var list []followingEntry
	for rows.Next() {
		var f followingEntry
		var createdAt time.Time
		if err := rows.Scan(&f.ID, &f.ActorURL, &f.Accepted, &createdAt,
			&f.UserID, &f.Username, &f.DisplayName, &f.AvatarURL, &f.Instance); err != nil {
			continue
		}
		f.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		list = append(list, f)
	}

	// Backfill: a follow accepted before the stub-eager-creation fix (or one
	// whose stub creation failed at follow time) has no matching users row
	// yet, which would otherwise leave it permanently missing from the
	// fediverse_account custom-feed filter picker. Self-healing — this only
	// does real work once per such entry, since upsertRemoteAPUser caches.
	for i := range list {
		if !list[i].Accepted || list[i].UserID != "" {
			continue
		}
		uid, err := s.getOrCreateRemoteAPUser(list[i].ActorURL)
		if err != nil || uid == "" {
			continue
		}
		list[i].UserID = uid
		s.db.QueryRow(`SELECT username, display_name, avatar_url, remote_instance FROM users WHERE id = $1`, uid).
			Scan(&list[i].Username, &list[i].DisplayName, &list[i].AvatarURL, &list[i].Instance)
	}

	if list == nil {
		list = []followingEntry{}
	}
	writeJSON(w, 200, map[string]any{"following": list})
}

// ── Outbound: broadcast public posts to followers ─────────────────────────────

// BroadcastPublicPost is called after a new post is created. It re-checks
// eligibility itself (defense in depth — never trusts the caller) and enqueues
// a signed Create activity for each of the author's ActivityPub followers.
func (s *Service) BroadcastPublicPost(userID, postID string) {
	if !s.activityPubEnabled() {
		return
	}

	var username, visibility, content, contentWarning string
	var profilePrivate, apEnabled bool
	var createdAt time.Time
	err := s.db.QueryRow(`
		SELECT u.username, u.profile_private, u.activitypub_enabled, p.visibility, p.content, p.content_warning, p.created_at
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, postID, userID).Scan(&username, &profilePrivate, &apEnabled, &visibility, &content, &contentWarning, &createdAt)
	if err != nil || visibility != "public" || profilePrivate || !apEnabled {
		return
	}

	activity := s.buildCreateActivity(s.actorURL(username), postID, content, createdAt, "", contentWarning)
	s.deliverToFollowers(userID, activity)
}

// BroadcastUpdatePost delivers a signed Update activity when a previously-
// federated post is edited (AGORA-150), re-deriving current state the same
// defense-in-depth way BroadcastPublicPost does rather than trusting the caller.
func (s *Service) BroadcastUpdatePost(userID, postID string) {
	if !s.activityPubEnabled() {
		return
	}

	var username, visibility, content, contentWarning string
	var profilePrivate, apEnabled bool
	var createdAt time.Time
	err := s.db.QueryRow(`
		SELECT u.username, u.profile_private, u.activitypub_enabled, p.visibility, p.content, p.content_warning, p.created_at
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, postID, userID).Scan(&username, &profilePrivate, &apEnabled, &visibility, &content, &contentWarning, &createdAt)
	if err != nil || visibility != "public" || profilePrivate || !apEnabled {
		return
	}

	activity := s.buildUpdateActivity(s.actorURL(username), postID, content, createdAt, "", contentWarning)
	s.deliverToFollowers(userID, activity)
}

// BroadcastDeletePost enqueues a signed Delete/Tombstone for a removed post.
// Followers who never received the original Create simply ignore an unknown
// object id, so this doesn't need to re-derive the post's past visibility.
func (s *Service) BroadcastDeletePost(userID, postID string) {
	if !s.activityPubEnabled() {
		return
	}

	var username string
	var profilePrivate, apEnabled bool
	if err := s.db.QueryRow(`SELECT username, profile_private, activitypub_enabled FROM users WHERE id = $1`, userID).
		Scan(&username, &profilePrivate, &apEnabled); err != nil || profilePrivate || !apEnabled {
		return
	}

	actor := s.actorURL(username)
	objID := actor + "/posts/" + postID
	activity := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       objID + "/delete",
		"type":     "Delete",
		"actor":    actor,
		"object": map[string]any{
			"id":   objID,
			"type": "Tombstone",
		},
		"to": []string{"https://www.w3.org/ns/activitystreams#Public"},
	}
	s.deliverToFollowers(userID, activity)
}

// DeliverReply delivers a new comment to the fediverse when it's a direct
// reply to a remote AP participant (someone whose reply we previously
// ingested via handleInboundCreate) — the minimum viable half of "at least
// the actor being replied to" from AGORA-147's AC. A plain comment on a local
// thread, or a reply to another local user, is a no-op here.
func (s *Service) DeliverReply(userID, commentID, replyToID string) {
	if !s.activityPubEnabled() {
		return
	}

	var targetIsRemote bool
	var targetActorURL, targetInboxURL, targetRemotePostID string
	err := s.db.QueryRow(`
		SELECT u.is_remote, u.ap_actor_url, u.ap_inbox_url, p.remote_post_id
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1
	`, replyToID).Scan(&targetIsRemote, &targetActorURL, &targetInboxURL, &targetRemotePostID)
	if err != nil || !targetIsRemote || targetActorURL == "" || targetInboxURL == "" || targetRemotePostID == "" {
		return
	}

	var username, content, contentWarning string
	var apEnabled bool
	var createdAt time.Time
	if err := s.db.QueryRow(`
		SELECT u.username, u.activitypub_enabled, p.content, p.content_warning, p.created_at
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2
	`, commentID, userID).Scan(&username, &apEnabled, &content, &contentWarning, &createdAt); err != nil || !apEnabled {
		return
	}

	actor := s.actorURL(username)
	activity := s.buildCreateActivity(actor, commentID, content, createdAt, targetRemotePostID, contentWarning)
	// Address the reply at the recipient directly rather than only Public/followers.
	if note, ok := activity["object"].(map[string]any); ok {
		note["to"] = []string{targetActorURL}
	}
	activity["to"] = []string{targetActorURL}

	s.enqueueAPDelivery(userID, targetInboxURL, activity)
}

// ── Outbound: broadcast page posts to page followers (AGORA-115) ─────────────
//
// Mirrors BroadcastPublicPost/BroadcastUpdatePost/BroadcastDeletePost, but
// attributed to the page's own actor rather than whichever member authored
// the post, and delivered via the page-specific queue/followers tables.

func (s *Service) BroadcastPagePost(pageID, postID string) {
	if !s.activityPubEnabled() {
		return
	}

	var slug, content, contentWarning string
	var apEnabled bool
	var privacy string
	var createdAt time.Time
	err := s.db.QueryRow(`
		SELECT pg.slug, pg.privacy, pg.activitypub_enabled, p.content, p.content_warning, p.created_at
		FROM posts p JOIN pages pg ON pg.id = p.page_id
		WHERE p.id = $1 AND p.page_id = $2 AND p.deleted_at IS NULL
	`, postID, pageID).Scan(&slug, &privacy, &apEnabled, &content, &contentWarning, &createdAt)
	if err != nil || privacy != "public" || !apEnabled {
		return
	}

	activity := s.buildCreateActivity(s.pageActorURL(slug), postID, content, createdAt, "", contentWarning)
	s.deliverToPageFollowers(pageID, activity)
}

func (s *Service) BroadcastPagePostUpdate(pageID, postID string) {
	if !s.activityPubEnabled() {
		return
	}

	var slug, content, contentWarning string
	var apEnabled bool
	var privacy string
	var createdAt time.Time
	err := s.db.QueryRow(`
		SELECT pg.slug, pg.privacy, pg.activitypub_enabled, p.content, p.content_warning, p.created_at
		FROM posts p JOIN pages pg ON pg.id = p.page_id
		WHERE p.id = $1 AND p.page_id = $2 AND p.deleted_at IS NULL
	`, postID, pageID).Scan(&slug, &privacy, &apEnabled, &content, &contentWarning, &createdAt)
	if err != nil || privacy != "public" || !apEnabled {
		return
	}

	activity := s.buildUpdateActivity(s.pageActorURL(slug), postID, content, createdAt, "", contentWarning)
	s.deliverToPageFollowers(pageID, activity)
}

func (s *Service) BroadcastPagePostDelete(pageID, postID string) {
	if !s.activityPubEnabled() {
		return
	}

	var slug string
	var apEnabled bool
	if err := s.db.QueryRow(`SELECT slug, activitypub_enabled FROM pages WHERE id = $1`, pageID).
		Scan(&slug, &apEnabled); err != nil || !apEnabled {
		return
	}

	actor := s.pageActorURL(slug)
	objID := actor + "/posts/" + postID
	activity := map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       objID + "/delete",
		"type":     "Delete",
		"actor":    actor,
		"object": map[string]any{
			"id":   objID,
			"type": "Tombstone",
		},
		"to": []string{"https://www.w3.org/ns/activitystreams#Public"},
	}
	s.deliverToPageFollowers(pageID, activity)
}

func (s *Service) deliverToPageFollowers(pageID string, activity map[string]any) {
	rows, err := s.db.Query(`SELECT follower_inbox_url FROM page_remote_subscribers WHERE page_id = $1`, pageID)
	if err != nil {
		return
	}
	defer rows.Close()

	var inboxes []string
	for rows.Next() {
		var inbox string
		if rows.Scan(&inbox) == nil {
			inboxes = append(inboxes, inbox)
		}
	}
	rows.Close()

	for _, inbox := range inboxes {
		s.enqueuePageAPDelivery(pageID, inbox, activity)
	}
}

func (s *Service) enqueuePageAPDelivery(pageID, inboxURL string, activity any) {
	payload, err := json.Marshal(activity)
	if err != nil {
		return
	}
	s.db.Exec(`
		INSERT INTO page_ap_delivery_queue (actor_page_id, inbox_url, activity, next_attempt)
		VALUES ($1, $2, $3, NOW())
	`, pageID, inboxURL, string(payload))
}

// drainPageAPQueue mirrors drainAPQueue for page-authored deliveries.
func (s *Service) drainPageAPQueue() {
	rows, err := s.db.Query(`
		SELECT id, actor_page_id, inbox_url, activity
		FROM page_ap_delivery_queue
		WHERE attempts < 10 AND next_attempt <= NOW()
		ORDER BY next_attempt ASC
		LIMIT 20
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	type job struct {
		id, pageID, inboxURL string
		activity             []byte
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.id, &j.pageID, &j.inboxURL, &j.activity) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, j := range jobs {
		sendErr := s.deliverPageAPActivity(j.pageID, j.inboxURL, j.activity)
		if sendErr == nil {
			s.db.Exec(`DELETE FROM page_ap_delivery_queue WHERE id = $1`, j.id)
		} else {
			s.db.Exec(`
				UPDATE page_ap_delivery_queue
				SET attempts = attempts + 1,
				    last_error = $1,
				    next_attempt = NOW() + (LEAST(POWER(2, attempts), 1440) * INTERVAL '1 minute')
				WHERE id = $2
			`, sendErr.Error(), j.id)
		}
	}
}

func (s *Service) deliverPageAPActivity(pageID, inboxURL string, activity []byte) error {
	var slug, pubPEM, privPEM string
	if err := s.db.QueryRow(`
		SELECT slug, federation_public_key, federation_private_key FROM pages WHERE id = $1
	`, pageID).Scan(&slug, &pubPEM, &privPEM); err != nil {
		return err
	}

	_, _, privKey, err := s.getOrCreatePageKeyPair(pageID, pubPEM, privPEM)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, inboxURL, bytes.NewReader(activity))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Accept", "application/activity+json")

	if err := signRequest(req, s.pageActorKeyID(slug), privKey, activity); err != nil {
		return err
	}

	resp, err := fedHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("remote inbox returned %d", resp.StatusCode)
	}
	return nil
}

func (s *Service) deliverToFollowers(userID string, activity map[string]any) {
	rows, err := s.db.Query(`SELECT follower_inbox_url FROM ap_followers WHERE followed_user_id = $1`, userID)
	if err != nil {
		return
	}
	defer rows.Close()

	var inboxes []string
	for rows.Next() {
		var inbox string
		if rows.Scan(&inbox) == nil {
			inboxes = append(inboxes, inbox)
		}
	}
	rows.Close()

	for _, inbox := range inboxes {
		s.enqueueAPDelivery(userID, inbox, activity)
	}
}

func (s *Service) enqueueAPDelivery(userID, inboxURL string, activity any) {
	payload, err := json.Marshal(activity)
	if err != nil {
		return
	}
	s.db.Exec(`
		INSERT INTO ap_delivery_queue (actor_user_id, inbox_url, activity, next_attempt)
		VALUES ($1, $2, $3, NOW())
	`, userID, inboxURL, string(payload))
}

// ── Outbound delivery worker ──────────────────────────────────────────────────

// drainAPQueue processes pending standard-ActivityPub deliveries. Kept
// separate from the legacy drainQueue (federation.go) because HTTP Signatures
// must be computed at send time (a fresh Date header each attempt), not once
// at enqueue time like the custom protocol's embedded-signature scheme.
func (s *Service) drainAPQueue() {
	rows, err := s.db.Query(`
		SELECT id, actor_user_id, inbox_url, activity
		FROM ap_delivery_queue
		WHERE attempts < 10 AND next_attempt <= NOW()
		ORDER BY next_attempt ASC
		LIMIT 20
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	type job struct {
		id, userID, inboxURL string
		activity             []byte
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.id, &j.userID, &j.inboxURL, &j.activity) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, j := range jobs {
		sendErr := s.deliverAPActivity(j.userID, j.inboxURL, j.activity)
		if sendErr == nil {
			s.db.Exec(`DELETE FROM ap_delivery_queue WHERE id = $1`, j.id)
		} else {
			s.db.Exec(`
				UPDATE ap_delivery_queue
				SET attempts = attempts + 1,
				    last_error = $1,
				    next_attempt = NOW() + (LEAST(POWER(2, attempts), 1440) * INTERVAL '1 minute')
				WHERE id = $2
			`, sendErr.Error(), j.id)
		}
	}
}

func (s *Service) deliverAPActivity(userID, inboxURL string, activity []byte) error {
	var username, pubPEM, privPEM string
	if err := s.db.QueryRow(`
		SELECT username, federation_public_key, federation_private_key FROM users WHERE id = $1
	`, userID).Scan(&username, &pubPEM, &privPEM); err != nil {
		return err
	}

	_, _, privKey, err := s.getOrCreateUserKeyPair(userID, pubPEM, privPEM)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, inboxURL, bytes.NewReader(activity))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Accept", "application/activity+json")

	if err := signRequest(req, s.actorKeyID(username), privKey, activity); err != nil {
		return err
	}

	resp, err := fedHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("remote inbox returned %d", resp.StatusCode)
	}
	return nil
}
