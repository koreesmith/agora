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
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
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
// hasn't opted out via activitypub_enabled. Used by every new AP endpoint so
// the eligibility rule stays in exactly one place.
func (s *Service) apEligibleUser(handle string) (*apUser, bool) {
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

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", nil, err
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", nil, err
	}
	privPEMOut := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))

	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", "", nil, err
	}
	pubPEMOut := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))

	if _, err := s.db.Exec(`
		UPDATE users SET federation_public_key = $1, federation_private_key = $2 WHERE id = $3
	`, pubPEMOut, privPEMOut, userID); err != nil {
		return "", "", nil, err
	}

	log.Printf("federation: generated new RSA keypair for user %s", userID)
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

	u, ok := s.apEligibleUser(username)
	if !ok {
		writeError(w, 404, "not found")
		return
	}

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
}

func (s *Service) HostMeta(w http.ResponseWriter, r *http.Request) {
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
	}

	writeJSON(w, 202, map[string]string{"message": "accepted"})
}

func (s *Service) handleInboundFollow(followID, followerActor string, objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" {
		return
	}
	username := usernameFromActorURL(objectURL, s.cfg.InstanceDomain)
	if username == "" {
		return
	}
	u, ok := s.apEligibleUser(username)
	if !ok {
		return
	}

	domain := domainFromURL(followerActor)
	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, domain).Scan(&status)
	if status == "blocked" {
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

func (s *Service) handleInboundUndoFollow(followerActor string, objectRaw json.RawMessage) {
	var objectURL string
	if err := json.Unmarshal(objectRaw, &objectURL); err != nil || objectURL == "" {
		return
	}
	username := usernameFromActorURL(objectURL, s.cfg.InstanceDomain)
	if username == "" {
		return
	}
	var userID string
	s.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = LOWER($1) AND is_remote = false`, username).Scan(&userID)
	if userID == "" {
		return
	}
	s.db.Exec(`DELETE FROM ap_followers WHERE followed_user_id = $1 AND follower_actor_url = $2`, userID, followerActor)
}

// ── Inbound Create (replies into threads we own) ──────────────────────────────

// handleInboundCreate ingests a reply from the fediverse into an existing
// Agora-owned thread. Top-level remote posts (no inReplyTo, or inReplyTo not
// resolving to something we own) are not ingested — that's AGORA-146's scope,
// a general fediverse timeline, not this ticket's reply-threading.
func (s *Service) handleInboundCreate(verifiedActor string, objectRaw json.RawMessage) {
	var note struct {
		ID           string `json:"id"`
		AttributedTo string `json:"attributedTo"`
		Content      string `json:"content"`
		InReplyTo    string `json:"inReplyTo"`
		Summary      string `json:"summary"` // AGORA-154: content-warning text, if any
	}
	if err := json.Unmarshal(objectRaw, &note); err != nil {
		return
	}
	// attributedTo must match the cryptographically verified signer — an
	// activity envelope signed by A cannot claim to contain a Note by B.
	if note.AttributedTo == "" || note.AttributedTo != verifiedActor {
		return
	}
	if note.ID == "" || note.InReplyTo == "" {
		return
	}

	// AGORA-148: an admin-blocked instance can't Follow, but until now could
	// still reply into threads — apply the same block-list check Follow uses.
	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, domainFromURL(verifiedActor)).Scan(&status)
	if status == "blocked" {
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

	if s.notif != nil {
		if postAuthorID != remoteUserID {
			s.notif.Create(postAuthorID, remoteUserID, "post_comment", rootPostID, "")
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
// own AP object URLs (.../federation/users/{username}/posts/{id}[/activity]),
// returning "" for anything that isn't ours.
func localPostIDFromURL(u, instanceDomain string) string {
	base := strings.TrimRight(instanceDomain, "/") + "/federation/users/"
	if !strings.HasPrefix(u, base) {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(u, base), "/")
	if len(parts) < 3 || parts[1] != "posts" {
		return ""
	}
	return strings.SplitN(parts[2], "#", 2)[0]
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

	err = s.db.QueryRow(`
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

	res, err := s.db.Exec(`INSERT INTO likes (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, remoteUserID, postID)
	if err != nil {
		return
	}
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
// fedHTTPClient) and returns the fields we care about.
func fetchActorProfile(actorURL string) (*remoteActorProfile, error) {
	if !strings.HasPrefix(actorURL, "https://") {
		return nil, fmt.Errorf("actor url must be https")
	}
	req, err := http.NewRequest(http.MethodGet, actorURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/activity+json")
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

// ── Outbound: broadcast public posts to followers ─────────────────────────────

// BroadcastPublicPost is called after a new post is created. It re-checks
// eligibility itself (defense in depth — never trusts the caller) and enqueues
// a signed Create activity for each of the author's ActivityPub followers.
func (s *Service) BroadcastPublicPost(userID, postID string) {
	if !s.federationEnabled() {
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
	if !s.federationEnabled() {
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
	if !s.federationEnabled() {
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
	if !s.federationEnabled() {
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
