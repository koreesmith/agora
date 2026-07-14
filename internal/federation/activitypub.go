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
		SELECT id, content, created_at
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
		var id, content string
		var createdAt time.Time
		if err := rows.Scan(&id, &content, &createdAt); err != nil {
			continue
		}
		items = append(items, s.buildCreateActivity(actor, id, content, createdAt))
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

// buildCreateActivity builds a Create activity wrapping a Note, used by both
// the Outbox (historical posts) and BroadcastPublicPost (new posts).
func (s *Service) buildCreateActivity(actor, postID, content string, createdAt time.Time) map[string]any {
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
	return map[string]any{
		"@context":  "https://www.w3.org/ns/activitystreams",
		"id":        objID + "/activity",
		"type":      "Create",
		"actor":     actor,
		"published": published,
		"to":        []string{"https://www.w3.org/ns/activitystreams#Public"},
		"cc":        []string{actor + "/followers"},
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
	if err := verifyInboundSignature(r, body); err != nil {
		log.Printf("federation: ap signature verification failed: %v", err)
		writeError(w, 401, "invalid signature")
		return
	}

	var a struct {
		ID     string          `json:"id"`
		Type   string          `json:"type"`
		Actor  string          `json:"actor"`
		Object json.RawMessage `json:"object"`
	}
	if err := json.Unmarshal(body, &a); err != nil {
		writeError(w, 400, "invalid activity")
		return
	}

	switch a.Type {
	case "Follow":
		s.handleInboundFollow(a.ID, a.Actor, a.Object)
	case "Undo":
		var inner struct {
			Type   string          `json:"type"`
			Object json.RawMessage `json:"object"`
		}
		json.Unmarshal(a.Object, &inner)
		if inner.Type == "Follow" {
			s.handleInboundUndoFollow(a.Actor, inner.Object)
		}
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

	followerInbox, err := fetchActorInbox(followerActor)
	if err != nil || followerInbox == "" {
		return
	}

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

// fetchActorInbox dereferences a remote actor URL (via the SSRF-safe
// fedHTTPClient) to find their inbox.
func fetchActorInbox(actorURL string) (string, error) {
	if !strings.HasPrefix(actorURL, "https://") {
		return "", fmt.Errorf("actor url must be https")
	}
	req, err := http.NewRequest(http.MethodGet, actorURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/activity+json")
	resp, err := fedHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("actor fetch returned %d", resp.StatusCode)
	}
	var actor struct {
		Inbox string `json:"inbox"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&actor); err != nil {
		return "", err
	}
	if actor.Inbox == "" {
		return "", fmt.Errorf("actor has no inbox")
	}
	return actor.Inbox, nil
}

// ── Outbound: broadcast public posts to followers ─────────────────────────────

// BroadcastPublicPost is called after a new post is created. It re-checks
// eligibility itself (defense in depth — never trusts the caller) and enqueues
// a signed Create activity for each of the author's ActivityPub followers.
func (s *Service) BroadcastPublicPost(userID, postID string) {
	if !s.federationEnabled() {
		return
	}

	var username, visibility, content string
	var profilePrivate, apEnabled bool
	var createdAt time.Time
	err := s.db.QueryRow(`
		SELECT u.username, u.profile_private, u.activitypub_enabled, p.visibility, p.content, p.created_at
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, postID, userID).Scan(&username, &profilePrivate, &apEnabled, &visibility, &content, &createdAt)
	if err != nil || visibility != "public" || profilePrivate || !apEnabled {
		return
	}

	activity := s.buildCreateActivity(s.actorURL(username), postID, content, createdAt)
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
