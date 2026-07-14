package federation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db    *store.DB
	cfg   *config.Config
	notif *notifications.Service
}

func NewService(db *store.DB, cfg *config.Config, notif *notifications.Service) *Service {
	return &Service{db: db, cfg: cfg, notif: notif}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/.well-known/agora-instance", s.InstanceInfo)
	// Standard ActivityPub discovery — what Mastodon/Pleroma/etc. actually query.
	r.Get("/.well-known/webfinger", s.WebFinger)
	r.Get("/.well-known/host-meta", s.HostMeta)
	r.Post("/federation/inbox",          s.Inbox)
	r.Get("/federation/users/{handle}",  s.GetUser)
	r.Get("/federation/users/{handle}/outbox",    s.Outbox)
	r.Get("/federation/users/{handle}/followers", s.Followers)
	r.Get("/federation/search",          s.Search)
}

// RegisterAuthedRoutes registers federation routes that require a valid Agora
// session. LookupUser (AGORA-139) is only ever called by Agora's own
// authenticated frontend (SearchPage) — requiring auth removes it as an
// anonymous-callable surface, on top of the SSRF protection fedHTTPClient's
// dialer already provides on the outbound fetch it triggers.
func RegisterAuthedRoutes(r chi.Router, s *Service) {
	r.Get("/federation/lookup", s.LookupUser) // resolve user@instance.com
}

// ── Instance info (public) ────────────────────────────────────────────────────

func (s *Service) InstanceInfo(w http.ResponseWriter, r *http.Request) {
	if !s.federationEnabled() {
		writeError(w, 404, "federation not enabled")
		return
	}

	pubKey, _, err := s.getOrCreateKeyPair()
	if err != nil {
		writeError(w, 500, "key error")
		return
	}

	var name, description string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_name'`).Scan(&name)
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_description'`).Scan(&description)

	var userCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_remote = false AND is_suspended = false`).Scan(&userCount)

	// Include instance rules
	rows, _ := s.db.Query(`SELECT text FROM instance_rules ORDER BY position ASC`)
	var rules []string
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			rows.Scan(&t)
			rules = append(rules, t)
		}
	}
	if rules == nil { rules = []string{} }

	writeJSON(w, 200, map[string]any{
		"domain":      domainFromURL(s.cfg.InstanceDomain),
		"name":        name,
		"description": description,
		"public_key":  base64.StdEncoding.EncodeToString(pubKey),
		"api_version": "1",
		"user_count":  userCount,
		"software":    "agora",
		"rules":       rules,
	})
}

// ── Inbox (receives activities from remote instances) ─────────────────────────

type Activity struct {
	Type       string          `json:"type"`
	Actor      string          `json:"actor"`
	Object     json.RawMessage `json:"object"`
	InstanceID string          `json:"instance_id"`
	Timestamp  int64           `json:"timestamp"`
	Signature  string          `json:"signature"`
}

func (s *Service) Inbox(w http.ResponseWriter, r *http.Request) {
	if !s.federationEnabled() {
		writeError(w, 404, "federation not enabled")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, 400, "could not read body")
		return
	}

	// Standard ActivityPub activities (from Mastodon etc.) are verified via
	// HTTP Signatures, not the custom protocol's embedded-JSON-field Ed25519
	// signature below — detect and branch before the custom verification path
	// ever runs, since a standard activity has no "signature"/"instance_id"
	// fields and would otherwise always fail it.
	var probe struct {
		Context json.RawMessage `json:"@context"`
		Type    string          `json:"type"`
	}
	json.Unmarshal(body, &probe)
	if len(probe.Context) > 0 || probe.Type == "Follow" || probe.Type == "Undo" {
		s.handleStandardInbox(w, r, body)
		return
	}

	var activity Activity
	if err := json.Unmarshal(body, &activity); err != nil {
		writeError(w, 400, "invalid activity")
		return
	}

	if err := s.verifyActivity(body, activity); err != nil {
		log.Printf("federation: signature verification failed from %s: %v", activity.InstanceID, err)
		writeError(w, 401, "invalid signature")
		return
	}

	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, activity.InstanceID).Scan(&status)
	if status == "blocked" {
		writeError(w, 403, "instance is blocked")
		return
	}

	s.db.Exec(`UPDATE federated_instances SET last_seen_at = NOW() WHERE domain = $1`, activity.InstanceID)

	payload, _ := json.Marshal(activity)
	s.db.Exec(`INSERT INTO audit_log (action, target_type, target_id, details) VALUES ('federation_inbox', 'activity', $1, $2)`,
		activity.Type, string(payload))

	switch activity.Type {
	case "post":
		s.handleInboundPost(activity)
	case "delete_post":
		s.handleInboundDelete(activity)
	case "friend_request":
		s.handleInboundFriendRequest(activity)
	case "friend_accept":
		s.handleInboundFriendAccept(activity)
	case "profile_update":
		s.handleInboundProfileUpdate(activity)
	}

	writeJSON(w, 202, map[string]string{"message": "accepted"})
}

func (s *Service) handleInboundPost(a Activity) {
	var obj struct {
		ID         string `json:"id"`
		Content    string `json:"content"`
		ImageURL   string `json:"image_url"`
		Visibility string `json:"visibility"`
		AuthorID   string `json:"author_handle"`
		CreatedAt  string `json:"created_at"`
	}
	if err := json.Unmarshal(a.Object, &obj); err != nil { return }
	if obj.Visibility != "public" { return }

	authorID := s.getOrCreateRemoteUser(obj.AuthorID, a.InstanceID)
	s.db.Exec(`
		INSERT INTO posts (author_id, content, image_url, visibility, is_remote, remote_post_id, remote_instance)
		VALUES ($1, $2, $3, 'public', true, $4, $5)
		ON CONFLICT DO NOTHING
	`, authorID, obj.Content, obj.ImageURL, obj.ID, a.InstanceID)
}

func (s *Service) handleInboundDelete(a Activity) {
	var obj struct{ ID string `json:"id"` }
	json.Unmarshal(a.Object, &obj)
	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE remote_post_id = $1 AND remote_instance = $2`, obj.ID, a.InstanceID)
}

func (s *Service) handleInboundFriendRequest(a Activity) {
	var obj struct {
		FromHandle string `json:"from_handle"`
		ToHandle   string `json:"to_handle"`
	}
	if err := json.Unmarshal(a.Object, &obj); err != nil { return }

	remoteUserID := s.getOrCreateRemoteUser(obj.FromHandle, a.InstanceID)
	var localUserID string
	s.db.QueryRow(`SELECT id FROM users WHERE username = $1 AND is_remote = false`, obj.ToHandle).Scan(&localUserID)
	if localUserID == "" { return }

	s.db.Exec(`
		INSERT INTO friendships (requester_id, addressee_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT DO NOTHING
	`, remoteUserID, localUserID)
}

func (s *Service) handleInboundFriendAccept(a Activity) {
	var obj struct {
		FromHandle string `json:"from_handle"`
		ToHandle   string `json:"to_handle"`
	}
	if err := json.Unmarshal(a.Object, &obj); err != nil { return }

	remoteUserID := s.getOrCreateRemoteUser(obj.FromHandle, a.InstanceID)
	var localUserID string
	s.db.QueryRow(`SELECT id FROM users WHERE username = $1 AND is_remote = false`, obj.ToHandle).Scan(&localUserID)
	if localUserID == "" || remoteUserID == "" { return }

	s.db.Exec(`
		UPDATE friendships SET status = 'accepted', updated_at = NOW()
		WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'
	`, localUserID, remoteUserID)
}

// handleInboundProfileUpdate syncs a remote user's profile fields
func (s *Service) handleInboundProfileUpdate(a Activity) {
	var obj struct {
		Handle      string `json:"handle"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Bio         string `json:"bio"`
	}
	if err := json.Unmarshal(a.Object, &obj); err != nil { return }

	s.db.Exec(`
		UPDATE users
		SET display_name = $1, avatar_url = $2, bio = $3, remote_synced_at = NOW()
		WHERE remote_user_id = $4 AND remote_instance = $5
	`, obj.DisplayName, obj.AvatarURL, obj.Bio, obj.Handle, a.InstanceID)
}

// ── Remote user lookup + sync ─────────────────────────────────────────────────

// getOrCreateRemoteUser returns the local UUID for a remote handle, fetching
// their profile from the remote instance if they don't exist yet.
func (s *Service) getOrCreateRemoteUser(handle, instance string) string {
	var id string
	s.db.QueryRow(`SELECT id FROM users WHERE remote_user_id = $1 AND remote_instance = $2`, handle, instance).Scan(&id)
	if id != "" {
		return id
	}

	// Fetch profile from the remote instance
	profile := s.fetchRemoteProfile(handle, instance)

	displayName := profile["display_name"]
	if displayName == "" { displayName = handle + "@" + instance }
	avatarURL  := profile["avatar_url"]
	bio        := profile["bio"]

	s.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, avatar_url, bio,
		                   email_verified, is_remote, remote_user_id, remote_instance, remote_synced_at)
		VALUES ($1, $2, '', $3, $4, $5, true, true, $6, $7, NOW())
		ON CONFLICT (username) DO UPDATE
		  SET display_name = $3, avatar_url = $4, bio = $5, remote_synced_at = NOW()
		RETURNING id
	`, handle+"@"+instance,
		handle+"@"+instance,
		displayName, avatarURL, bio,
		handle, instance,
	).Scan(&id)
	return id
}

// fetchRemoteProfile GETs /federation/users/{handle} on the remote instance.
// Returns an empty map on any error (caller must handle gracefully).
func (s *Service) fetchRemoteProfile(handle, instance string) map[string]string {
	if !isValidInstanceHost(instance) {
		return map[string]string{}
	}
	reqURL := "https://" + instance + "/federation/users/" + url.PathEscape(handle)
	resp, err := fedHTTPClient.Get(reqURL)
	if err != nil || resp.StatusCode != 200 {
		return map[string]string{}
	}
	defer resp.Body.Close()

	var profile map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return map[string]string{}
	}
	return profile
}

// syncStaleRemoteUsers re-fetches profiles for remote users not synced in 24h.
func (s *Service) syncStaleRemoteUsers() {
	rows, err := s.db.Query(`
		SELECT remote_user_id, remote_instance
		FROM users
		WHERE is_remote = true
		  AND (remote_synced_at IS NULL OR remote_synced_at < NOW() - INTERVAL '24 hours')
		LIMIT 50
	`)
	if err != nil { return }
	defer rows.Close()

	type entry struct{ handle, instance string }
	var stale []entry
	for rows.Next() {
		var e entry
		rows.Scan(&e.handle, &e.instance)
		stale = append(stale, e)
	}
	rows.Close()

	for _, e := range stale {
		profile := s.fetchRemoteProfile(e.handle, e.instance)
		if len(profile) == 0 { continue }
		s.db.Exec(`
			UPDATE users SET display_name = $1, avatar_url = $2, bio = $3, remote_synced_at = NOW()
			WHERE remote_user_id = $4 AND remote_instance = $5
		`, profile["display_name"], profile["avatar_url"], profile["bio"], e.handle, e.instance)
	}
}

// ── Federated user profile ────────────────────────────────────────────────────

func (s *Service) GetUser(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")

	// Standard ActivityPub actor document — legacy flat-JSON response below is
	// unchanged for the custom protocol's own requests (no Accept header, or
	// a plain application/json Accept).
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/activity+json") || strings.Contains(accept, "application/ld+json") {
		s.writeActorObject(w, handle)
		return
	}

	var u struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Bio         string `json:"bio"`
	}
	err := s.db.QueryRow(`
		SELECT username, display_name, avatar_url, bio
		FROM users WHERE username = $1 AND is_remote = false AND profile_private = false
	`, handle).Scan(&u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio)
	if err != nil {
		writeError(w, 404, "user not found or profile is private")
		return
	}
	writeJSON(w, 200, u)
}

// ── Federated search ──────────────────────────────────────────────────────────

func (s *Service) Search(w http.ResponseWriter, r *http.Request) {
	if !s.federationEnabled() {
		writeError(w, 404, "federation not enabled")
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, 400, "q required")
		return
	}

	rows, err := s.db.Query(`
		SELECT username, display_name, avatar_url
		FROM users
		WHERE is_remote = false AND profile_private = false
		  AND (username ILIKE '%'||$1||'%' OR display_name ILIKE '%'||$1||'%')
		  AND deletion_scheduled_at IS NULL
		LIMIT 20
	`, q)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type User struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Instance    string `json:"instance"`
	}
	domain := domainFromURL(s.cfg.InstanceDomain)
	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.Username, &u.DisplayName, &u.AvatarURL)
		u.Instance = domain
		users = append(users, u)
	}
	if users == nil { users = []User{} }
	writeJSON(w, 200, map[string]any{"users": users})
}

// ── Cross-instance user lookup ────────────────────────────────────────────────

// LookupUser resolves a user@instance handle by fetching their profile from the
// remote instance and creating/updating the local stub. Returns the local profile.
// Query param: handle=username@instance.com
func (s *Service) LookupUser(w http.ResponseWriter, r *http.Request) {
	if !s.federationEnabled() {
		writeError(w, 404, "federation not enabled")
		return
	}

	raw := r.URL.Query().Get("handle")
	if raw == "" {
		writeError(w, 400, "handle required — format: username@instance.com")
		return
	}

	parts := strings.SplitN(raw, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, 400, "invalid handle — format: username@instance.com")
		return
	}
	username, instance := parts[0], parts[1]

	if !isValidInstanceHost(instance) {
		writeError(w, 400, "invalid instance domain")
		return
	}

	// Don't look up our own users this way
	localDomain := domainFromURL(s.cfg.InstanceDomain)
	if instance == localDomain {
		var u struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			AvatarURL   string `json:"avatar_url"`
			Bio         string `json:"bio"`
			ID          string `json:"id"`
			IsRemote    bool   `json:"is_remote"`
		}
		s.db.QueryRow(`SELECT id, username, display_name, avatar_url, bio FROM users WHERE username = $1 AND is_remote = false AND deletion_scheduled_at IS NULL`, username).
			Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio)
		if u.ID == "" {
			writeError(w, 404, "user not found")
			return
		}
		writeJSON(w, 200, map[string]any{"user": u, "local": true})
		return
	}

	// Check if already cached locally
	var localID string
	s.db.QueryRow(`SELECT id FROM users WHERE remote_user_id = $1 AND remote_instance = $2`, username, instance).Scan(&localID)

	// Fetch fresh profile from remote
	profile := s.fetchRemoteProfile(username, instance)
	if len(profile) == 0 && localID == "" {
		writeError(w, 404, "user not found on remote instance — check the handle and try again")
		return
	}

	// Create or update local stub
	localID = s.getOrCreateRemoteUser(username, instance)

	type Result struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Bio         string `json:"bio"`
		IsRemote    bool   `json:"is_remote"`
		Instance    string `json:"remote_instance"`
	}
	var u Result
	s.db.QueryRow(`SELECT id, username, display_name, avatar_url, bio, is_remote, remote_instance FROM users WHERE id = $1`, localID).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.IsRemote, &u.Instance)

	writeJSON(w, 200, map[string]any{"user": u, "local": false})
}

// ── Outbound helpers (called by other services) ───────────────────────────────

// SendToUserInstance enqueues an activity to be delivered to a specific remote instance.
// activity can be any JSON-serialisable value; the federation service will sign it.
func (s *Service) SendToUserInstance(remoteInstance, instanceURL string, activity any) {
	if !s.federationEnabled() { return }

	payload, err := json.Marshal(activity)
	if err != nil { return }

	// Ensure the activity has our instance_id and timestamp set
	var m map[string]any
	json.Unmarshal(payload, &m)
	if m["instance_id"] == nil { m["instance_id"] = domainFromURL(s.cfg.InstanceDomain) }
	if m["timestamp"] == nil { m["timestamp"] = time.Now().Unix() }
	payload, _ = json.Marshal(m)

	s.db.Exec(`
		INSERT INTO federation_queue (instance_url, payload, next_attempt)
		VALUES ($1, $2, NOW())
	`, instanceURL, string(payload))
}

// BroadcastToFriendInstances sends an activity to all remote instances where
// the given user has at least one accepted friend.
func (s *Service) BroadcastToFriendInstances(userID string, activity any) {
	if !s.federationEnabled() { return }

	payload, err := json.Marshal(activity)
	if err != nil { return }

	var m map[string]any
	json.Unmarshal(payload, &m)
	if m["instance_id"] == nil { m["instance_id"] = domainFromURL(s.cfg.InstanceDomain) }
	if m["timestamp"] == nil { m["timestamp"] = time.Now().Unix() }
	payload, _ = json.Marshal(m)

	// Find distinct remote instances of accepted friends
	rows, err := s.db.Query(`
		SELECT DISTINCT u.remote_instance
		FROM friendships f
		JOIN users u ON u.id = CASE
			WHEN f.requester_id = $1 THEN f.addressee_id
			ELSE f.requester_id
		END
		WHERE (f.requester_id = $1 OR f.addressee_id = $1)
		  AND f.status = 'accepted'
		  AND u.is_remote = true
		  AND u.remote_instance != ''
	`, userID)
	if err != nil { return }
	defer rows.Close()

	var instances []string
	for rows.Next() {
		var inst string
		rows.Scan(&inst)
		instances = append(instances, inst)
	}
	rows.Close()

	for _, inst := range instances {
		instanceURL := "https://" + inst
		s.db.Exec(`
			INSERT INTO federation_queue (instance_url, payload, next_attempt)
			VALUES ($1, $2, NOW())
		`, instanceURL, string(payload))
	}
}

// ── Outbound queue ────────────────────────────────────────────────────────────

// SendActivity enqueues an outbound activity for reliable delivery.
// The background worker will sign and deliver it, retrying on failure.
func (s *Service) SendActivity(instanceURL string, activity Activity) error {
	payload, err := json.Marshal(activity)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO federation_queue (instance_url, payload, next_attempt)
		VALUES ($1, $2, NOW())
	`, instanceURL, string(payload))
	return err
}

// drainQueue processes pending outbound activities, retrying with backoff.
func (s *Service) drainQueue() {
	_, privKey, err := s.getOrCreateKeyPair()
	if err != nil {
		log.Printf("federation: drainQueue: could not get key pair: %v", err)
		return
	}

	rows, err := s.db.Query(`
		SELECT id, instance_url, payload
		FROM federation_queue
		WHERE attempts < 10 AND next_attempt <= NOW()
		ORDER BY next_attempt ASC
		LIMIT 20
	`)
	if err != nil { return }
	defer rows.Close()

	type job struct {
		id          string
		instanceURL string
		payload     []byte
	}
	var jobs []job
	for rows.Next() {
		var j job
		rows.Scan(&j.id, &j.instanceURL, &j.payload)
		jobs = append(jobs, j)
	}
	rows.Close()

	for _, j := range jobs {
		var activity Activity
		if err := json.Unmarshal(j.payload, &activity); err != nil {
			s.db.Exec(`DELETE FROM federation_queue WHERE id = $1`, j.id)
			continue
		}

		// Sign payload
		sig := ed25519.Sign(privKey, j.payload)
		activity.Signature = base64.StdEncoding.EncodeToString(sig)
		signed, _ := json.Marshal(activity)

		sendErr := s.deliverActivity(j.instanceURL, signed)
		if sendErr == nil {
			s.db.Exec(`DELETE FROM federation_queue WHERE id = $1`, j.id)
		} else {
			// Exponential backoff: 2^attempts minutes, capped at 24h
			s.db.Exec(`
				UPDATE federation_queue
				SET attempts = attempts + 1,
				    last_error = $1,
				    next_attempt = NOW() + (LEAST(POWER(2, attempts), 1440) * INTERVAL '1 minute')
				WHERE id = $2
			`, sendErr.Error(), j.id)
		}
	}
}

func (s *Service) deliverActivity(instanceURL string, signed []byte) error {
	resp, err := fedHTTPClient.Post(instanceURL+"/federation/inbox", "application/json", bytes.NewReader(signed))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("remote returned %d", resp.StatusCode)
	}
	return nil
}

// ── Signature verification ────────────────────────────────────────────────────

func (s *Service) verifyActivity(raw []byte, a Activity) error {
	if a.Signature == "" { return fmt.Errorf("no signature") }
	if a.InstanceID == "" { return fmt.Errorf("no instance id") }

	sig, err := base64.StdEncoding.DecodeString(a.Signature)
	if err != nil { return fmt.Errorf("bad signature encoding") }

	var m map[string]any
	json.Unmarshal(raw, &m)
	delete(m, "signature")
	unsigned, _ := json.Marshal(m)

	pubKey, err := s.getRemotePublicKey(a.InstanceID)
	if err != nil { return fmt.Errorf("could not get remote key: %w", err) }

	if !ed25519.Verify(pubKey, unsigned, sig) {
		return fmt.Errorf("signature invalid")
	}
	return nil
}

func (s *Service) getRemotePublicKey(domain string) (ed25519.PublicKey, error) {
	var keyB64 string
	s.db.QueryRow(`SELECT public_key FROM federated_instances WHERE domain = $1 AND status != 'blocked'`, domain).Scan(&keyB64)
	if keyB64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(keyB64)
		if err != nil { return nil, err }
		return ed25519.PublicKey(decoded), nil
	}

	if !isValidInstanceHost(domain) {
		return nil, fmt.Errorf("invalid instance domain")
	}
	resp, err := fedHTTPClient.Get("https://" + domain + "/.well-known/agora-instance")
	if err != nil { return nil, fmt.Errorf("could not reach instance: %w", err) }
	defer resp.Body.Close()

	var info struct {
		PublicKey string `json:"public_key"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("invalid instance info")
	}

	decoded, err := base64.StdEncoding.DecodeString(info.PublicKey)
	if err != nil { return nil, fmt.Errorf("bad public key") }

	s.db.Exec(`
		INSERT INTO federated_instances (domain, name, public_key, instance_url, status)
		VALUES ($1, $2, $3, $4, 'active')
		ON CONFLICT (domain) DO UPDATE SET public_key = $3, name = $2, last_seen_at = NOW()
	`, domain, info.Name, info.PublicKey, "https://"+domain)

	return ed25519.PublicKey(decoded), nil
}

// ── Key pair management ───────────────────────────────────────────────────────

func (s *Service) getOrCreateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	var pubB64, privB64 string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'federation_public_key'`).Scan(&pubB64)
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'federation_private_key'`).Scan(&privB64)

	if pubB64 != "" && privB64 != "" {
		pub, err1 := base64.StdEncoding.DecodeString(pubB64)
		priv, err2 := base64.StdEncoding.DecodeString(privB64)
		if err1 == nil && err2 == nil {
			return ed25519.PublicKey(pub), ed25519.PrivateKey(priv), nil
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil { return nil, nil, err }

	s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('federation_public_key', $1) ON CONFLICT (key) DO UPDATE SET value = $1`,
		base64.StdEncoding.EncodeToString(pub))
	s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('federation_private_key', $1) ON CONFLICT (key) DO UPDATE SET value = $1`,
		base64.StdEncoding.EncodeToString(priv))

	log.Println("federation: generated new Ed25519 keypair")
	return pub, priv, nil
}

// ── Background sync ───────────────────────────────────────────────────────────

func (s *Service) StartBackgroundSync(ctx context.Context) {
	// Deliberately does NOT gate the whole loop on federationEnabled() at
	// startup — that was a bug: federation_enabled is an admin-toggleable
	// runtime setting, but a one-time check here meant that if it happened to
	// be off (or unset) at the exact moment the server process started, the
	// delivery-queue drain loop would never run again for that process's
	// lifetime, even after an admin turned federation back on — outbound
	// activities would sit queued forever until the next restart. Instead the
	// loop always runs, and each tick re-checks the current value.
	queueTicker  := time.NewTicker(30 * time.Second)  // drain outbound queue
	apQueueTicker := time.NewTicker(20 * time.Second) // drain standard-AP delivery queue
	syncTicker   := time.NewTicker(15 * time.Minute)  // refresh instance list
	profileTicker := time.NewTicker(6 * time.Hour)    // sync stale remote profiles

	defer queueTicker.Stop()
	defer apQueueTicker.Stop()
	defer syncTicker.Stop()
	defer profileTicker.Stop()

	// Run immediately on start
	if s.federationEnabled() {
		go s.drainQueue()
		go s.drainAPQueue()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-queueTicker.C:
			if s.federationEnabled() {
				go s.drainQueue()
			}
		case <-apQueueTicker.C:
			if s.federationEnabled() {
				go s.drainAPQueue()
			}
		case <-syncTicker.C:
			if s.federationEnabled() {
				go s.refreshInstances()
			}
		case <-profileTicker.C:
			if s.federationEnabled() {
				go s.syncStaleRemoteUsers()
			}
		}
	}
}

func (s *Service) refreshInstances() {
	rows, _ := s.db.Query(`SELECT domain FROM federated_instances WHERE status = 'active'`)
	if rows == nil { return }
	defer rows.Close()
	for rows.Next() {
		var domain string
		rows.Scan(&domain)
		go func(d string) {
			if !isValidInstanceHost(d) { return }
			resp, err := fedHTTPClient.Get("https://" + d + "/.well-known/agora-instance")
			if err != nil { return }
			resp.Body.Close()
			if resp.StatusCode == 200 {
				s.db.Exec(`UPDATE federated_instances SET last_seen_at = NOW() WHERE domain = $1`, d)
			}
		}(domain)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Service) federationEnabled() bool {
	var val string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'federation_enabled'`).Scan(&val)
	return val == "true"
}

func domainFromURL(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return strings.Split(u, "/")[0]
}

// ── SSRF protection ─────────────────────────────────────────────────────────────
//
// All outbound federation requests go through fedHTTPClient, whose dialer refuses
// to connect to non-public IP addresses. This prevents an attacker-supplied
// instance host (e.g. via /federation/lookup) from making the server reach
// internal services, cloud metadata endpoints (169.254.169.254), or loopback.

var fedHTTPClient = &http.Client{
	Timeout:   10 * time.Second,
	Transport: &http.Transport{DialContext: safeDialContext},
}

// isPublicIP reports whether ip is a globally routable address we're willing to
// connect to. Loopback, private, link-local, CGNAT, unspecified, and multicast
// ranges are all rejected.
func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10 — carrier-grade NAT (not covered by IsPrivate)
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return false
	}
	return true
}

// safeDialContext resolves the target host, verifies every resolved IP is public,
// then dials a validated IP directly (closing the DNS-rebinding window between
// the check and the connection).
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ipa := range ips {
		if !isPublicIP(ipa.IP) {
			return nil, fmt.Errorf("refusing to connect to non-public address %s", ipa.IP)
		}
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

// isValidInstanceHost performs cheap syntactic validation on a federation host
// before it's ever placed into a URL. It rejects empty values, over-long names,
// and anything containing characters that could alter the request target.
func isValidInstanceHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if strings.ContainsAny(h, "/\\?#@ \t\r\n") {
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
