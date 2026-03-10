package federation

import (
	"strings"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db  *store.DB
	cfg *config.Config
}

// feedSvc and userSvc are accepted for future use (cross-package circular-dep avoidance)
func NewService(db *store.DB, cfg *config.Config, _, _ any) *Service {
	return &Service{db: db, cfg: cfg}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/.well-known/agora-instance", s.InstanceInfo)
	r.Post("/federation/inbox",           s.Inbox)
	r.Get("/federation/users/{handle}",  s.GetUser)
	r.Get("/federation/search",           s.Search)
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

	writeJSON(w, 200, map[string]any{
		"domain":      domainFromURL(s.cfg.InstanceDomain),
		"name":        name,
		"description": description,
		"public_key":  base64.StdEncoding.EncodeToString(pubKey),
		"api_version": "1",
		"user_count":  userCount,
		"software":    "agora",
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

	var activity Activity
	if err := json.Unmarshal(body, &activity); err != nil {
		writeError(w, 400, "invalid activity")
		return
	}

	// Verify signature
	if err := s.verifyActivity(body, activity); err != nil {
		log.Printf("federation: signature verification failed from %s: %v", activity.InstanceID, err)
		writeError(w, 401, "invalid signature")
		return
	}

	// Check not blocked
	var status string
	s.db.QueryRow(`SELECT status FROM federated_instances WHERE domain = $1`, activity.InstanceID).Scan(&status)
	if status == "blocked" {
		writeError(w, 403, "instance is blocked")
		return
	}

	// Update last seen
	s.db.Exec(`
		UPDATE federated_instances SET last_seen_at = NOW() WHERE domain = $1
	`, activity.InstanceID)

	// Log activity
	payload, _ := json.Marshal(activity)
	s.db.Exec(`
		INSERT INTO audit_log (action, target_type, target_id, details)
		VALUES ('federation_inbox', 'activity', $1, $2)
	`, activity.Type, string(payload))

	// Dispatch
	switch activity.Type {
	case "post":
		s.handleInboundPost(activity)
	case "delete_post":
		s.handleInboundDelete(activity)
	case "friend_request":
		s.handleInboundFriendRequest(activity)
	case "friend_accept":
		s.handleInboundFriendAccept(activity)
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
	if err := json.Unmarshal(a.Object, &obj); err != nil {
		return
	}
	if obj.Visibility != "public" {
		return // only federate public posts
	}

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
	if err := json.Unmarshal(a.Object, &obj); err != nil {
		return
	}

	remoteUserID := s.getOrCreateRemoteUser(obj.FromHandle, a.InstanceID)
	var localUserID string
	s.db.QueryRow(`SELECT id FROM users WHERE username = $1 AND is_remote = false`, obj.ToHandle).Scan(&localUserID)
	if localUserID == "" {
		return
	}

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
	if err := json.Unmarshal(a.Object, &obj); err != nil {
		return
	}

	remoteUserID := s.getOrCreateRemoteUser(obj.FromHandle, a.InstanceID)
	var localUserID string
	s.db.QueryRow(`SELECT id FROM users WHERE username = $1 AND is_remote = false`, obj.ToHandle).Scan(&localUserID)
	if localUserID == "" || remoteUserID == "" {
		return
	}

	s.db.Exec(`
		UPDATE friendships SET status = 'accepted', updated_at = NOW()
		WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'
	`, localUserID, remoteUserID)
}

// ── Remote user lookup ────────────────────────────────────────────────────────

func (s *Service) getOrCreateRemoteUser(handle, instance string) string {
	var id string
	s.db.QueryRow(`
		SELECT id FROM users WHERE remote_user_id = $1 AND remote_instance = $2
	`, handle, instance).Scan(&id)
	if id != "" {
		return id
	}

	// Create a stub remote user
	s.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, email_verified,
		                   is_remote, remote_user_id, remote_instance)
		VALUES ($1, $2, '', $3, true, true, $4, $5)
		ON CONFLICT (username) DO UPDATE SET remote_instance = $5
		RETURNING id
	`, handle+"@"+instance,
		handle+"@"+instance,
		handle+"@"+instance,
		handle,
		instance,
	).Scan(&id)
	return id
}

// ── Federated user profile ────────────────────────────────────────────────────

func (s *Service) GetUser(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
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

	// Search local users
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

// ── Outbound activity sender ──────────────────────────────────────────────────

func (s *Service) SendActivity(instanceURL string, activity Activity) error {
	_, privKey, err := s.getOrCreateKeyPair()
	if err != nil {
		return err
	}

	// Sign the payload
	payload, _ := json.Marshal(activity)
	sig := ed25519.Sign(privKey, payload)
	activity.Signature = base64.StdEncoding.EncodeToString(sig)

	signed, _ := json.Marshal(activity)
	resp, err := http.Post(instanceURL+"/federation/inbox", "application/json", bytes.NewReader(signed))
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
	if a.Signature == "" {
		return fmt.Errorf("no signature")
	}
	if a.InstanceID == "" {
		return fmt.Errorf("no instance id")
	}

	sig, err := base64.StdEncoding.DecodeString(a.Signature)
	if err != nil {
		return fmt.Errorf("bad signature encoding")
	}

	// Strip signature field before verifying
	var m map[string]any
	json.Unmarshal(raw, &m)
	delete(m, "signature")
	unsigned, _ := json.Marshal(m)

	pubKey, err := s.getRemotePublicKey(a.InstanceID)
	if err != nil {
		return fmt.Errorf("could not get remote key: %w", err)
	}

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
		if err != nil {
			return nil, err
		}
		return ed25519.PublicKey(decoded), nil
	}

	// Fetch from remote
	resp, err := http.Get("https://" + domain + "/.well-known/agora-instance")
	if err != nil {
		return nil, fmt.Errorf("could not reach instance: %w", err)
	}
	defer resp.Body.Close()

	var info struct {
		PublicKey string `json:"public_key"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("invalid instance info")
	}

	decoded, err := base64.StdEncoding.DecodeString(info.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("bad public key")
	}

	// Store for future use
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

	// Generate new keypair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	s.db.Exec(`
		INSERT INTO instance_settings (key, value) VALUES ('federation_public_key', $1)
		ON CONFLICT (key) DO UPDATE SET value = $1
	`, base64.StdEncoding.EncodeToString(pub))
	s.db.Exec(`
		INSERT INTO instance_settings (key, value) VALUES ('federation_private_key', $1)
		ON CONFLICT (key) DO UPDATE SET value = $1
	`, base64.StdEncoding.EncodeToString(priv))

	log.Println("federation: generated new Ed25519 keypair")
	return pub, priv, nil
}

// ── Background sync ───────────────────────────────────────────────────────────

func (s *Service) StartBackgroundSync(ctx context.Context) {
	if !s.federationEnabled() {
		return
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshInstances()
		}
	}
}

func (s *Service) refreshInstances() {
	rows, _ := s.db.Query(`SELECT domain FROM federated_instances WHERE status = 'active'`)
	if rows == nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var domain string
		rows.Scan(&domain)
		// Ping to keep last_seen fresh
		go func(d string) {
			resp, err := http.Get("https://" + d + "/.well-known/agora-instance")
			if err != nil {
				return
			}
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
