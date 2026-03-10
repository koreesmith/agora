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
	"net/http"
	"time"

	"github.com/agora-social/agora/internal/feed"
	"github.com/agora-social/agora/internal/users"
	"github.com/agora-social/agora/pkg/config"
	"github.com/agora-social/agora/pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

type Service struct {
	db       *sqlx.DB
	cfg      *config.Config
	feedSvc  *feed.Service
	userSvc  *users.Service
	privKey  ed25519.PrivateKey
	pubKey   ed25519.PublicKey
}

func NewService(db *sqlx.DB, cfg *config.Config, feedSvc *feed.Service, userSvc *users.Service) *Service {
	pub, priv, err := ensureKeyPair(db)
	if err != nil {
		log.Printf("Warning: could not load/create federation key pair: %v", err)
	}
	return &Service{db: db, cfg: cfg, feedSvc: feedSvc, userSvc: userSvc, privKey: priv, pubKey: pub}
}

// Federation protocol types
type InstanceInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	PublicKey   string `json:"public_key"`
	Version     string `json:"version"`
	UserCount   int    `json:"user_count"`
}

type FederatedActivity struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"` // post, friend_request, friend_accept, delete
	Actor      string          `json:"actor"` // user@instance.url
	Object     json.RawMessage `json:"object"`
	Instance   string          `json:"instance"`
	Signature  string          `json:"signature"`
	Timestamp  time.Time       `json:"timestamp"`
}

func RegisterRoutes(r chi.Router, svc *Service) {
	// Well-known endpoints
	r.Get("/.well-known/agora", svc.InstanceInfo)

	// Federation API
	r.Route("/federation", func(r chi.Router) {
		r.Post("/inbox", svc.Inbox)
		r.Get("/users/{username}", svc.GetFederatedUser)
		r.Get("/search", svc.FederatedSearch)
	})
}

func (s *Service) isEnabled() bool {
	var enabled string
	s.db.Get(&enabled, `SELECT value FROM instance_settings WHERE key = 'federation_enabled'`)
	return enabled == "true"
}

func (s *Service) InstanceInfo(w http.ResponseWriter, r *http.Request) {
	if !s.isEnabled() {
		utils.Error(w, http.StatusForbidden, "federation not enabled on this instance")
		return
	}

	var settings map[string]string
	rows, _ := s.db.Queryx(`SELECT key, value FROM instance_settings WHERE key IN ('instance_name', 'instance_description')`)
	settings = make(map[string]string)
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		settings[k] = v
	}

	var userCount int
	s.db.Get(&userCount, "SELECT COUNT(*) FROM users WHERE is_remote = false AND is_suspended = false")

	info := InstanceInfo{
		Name:        settings["instance_name"],
		Description: settings["instance_description"],
		URL:         s.cfg.InstanceURL,
		PublicKey:   base64.StdEncoding.EncodeToString(s.pubKey),
		Version:     "1.0",
		UserCount:   userCount,
	}

	utils.JSON(w, http.StatusOK, info)
}

func (s *Service) Inbox(w http.ResponseWriter, r *http.Request) {
	if !s.isEnabled() {
		utils.Error(w, http.StatusForbidden, "federation disabled")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		utils.Error(w, http.StatusBadRequest, "could not read body")
		return
	}

	var activity FederatedActivity
	if err := json.Unmarshal(body, &activity); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid activity")
		return
	}

	// Verify the sending instance is not blocked
	var instanceStatus string
	s.db.Get(&instanceStatus, `SELECT status FROM federated_instances WHERE domain = $1`, activity.Instance)
	if instanceStatus == "blocked" {
		utils.Error(w, http.StatusForbidden, "instance blocked")
		return
	}

	// Fetch instance public key if we haven't seen it
	pubKey, err := s.getOrFetchInstanceKey(activity.Instance)
	if err != nil {
		utils.Error(w, http.StatusUnauthorized, "could not verify sender")
		return
	}

	// Verify signature
	sigBytes, err := base64.StdEncoding.DecodeString(activity.Signature)
	if err != nil || len(sigBytes) == 0 {
		utils.Error(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// Reconstruct the signed payload (body minus signature field)
	activity.Signature = ""
	unsigned, _ := json.Marshal(activity)
	if !ed25519.Verify(pubKey, unsigned, sigBytes) {
		utils.Error(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	// Process activity
	switch activity.Type {
	case "post":
		s.handleFederatedPost(activity)
	case "delete":
		s.handleFederatedDelete(activity)
	case "friend_request":
		s.handleFederatedFriendRequest(activity)
	case "friend_accept":
		s.handleFederatedFriendAccept(activity)
	}

	// Update last_seen
	s.db.Exec(`UPDATE federated_instances SET last_seen_at = NOW() WHERE domain = $1`, activity.Instance)

	utils.JSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Service) GetFederatedUser(w http.ResponseWriter, r *http.Request) {
	if !s.isEnabled() {
		utils.Error(w, http.StatusForbidden, "federation disabled")
		return
	}

	username := chi.URLParam(r, "username")
	var user struct {
		ID          string `db:"id" json:"id"`
		Username    string `db:"username" json:"username"`
		DisplayName string `db:"display_name" json:"display_name"`
		AvatarURL   string `db:"avatar_url" json:"avatar_url"`
		Bio         string `db:"bio" json:"bio"`
	}

	err := s.db.Get(&user, `
		SELECT id, username, display_name, avatar_url, bio
		FROM users WHERE username = $1 AND is_remote = false AND is_suspended = false
	`, username)
	if err != nil {
		utils.Error(w, http.StatusNotFound, "user not found")
		return
	}

	utils.JSON(w, http.StatusOK, map[string]any{
		"id":           fmt.Sprintf("%s/federation/users/%s", s.cfg.InstanceURL, user.Username),
		"username":     user.Username,
		"display_name": user.DisplayName,
		"avatar_url":   user.AvatarURL,
		"bio":          user.Bio,
		"instance":     s.cfg.InstanceURL,
	})
}

func (s *Service) FederatedSearch(w http.ResponseWriter, r *http.Request) {
	if !s.isEnabled() {
		utils.Error(w, http.StatusForbidden, "federation disabled")
		return
	}

	q := r.URL.Query().Get("q")
	scope := r.URL.Query().Get("scope") // "local" or "federation"

	// Local search
	var localUsers []struct {
		ID          string `db:"id" json:"id"`
		Username    string `db:"username" json:"username"`
		DisplayName string `db:"display_name" json:"display_name"`
		AvatarURL   string `db:"avatar_url" json:"avatar_url"`
		Instance    string `json:"instance"`
	}
	s.db.Select(&localUsers, `
		SELECT id, username, display_name, avatar_url
		FROM users WHERE is_suspended = false
		AND (username ILIKE $1 OR display_name ILIKE $1)
		LIMIT 10
	`, "%"+q+"%")

	for i := range localUsers {
		localUsers[i].Instance = s.cfg.InstanceURL
	}

	result := map[string]any{"local": localUsers}

	if scope == "federation" {
		// Search all known active instances
		var instances []struct {
			InstanceURL string `db:"instance_url"`
		}
		s.db.Select(&instances, `SELECT instance_url FROM federated_instances WHERE status = 'active'`)

		var remoteUsers []any
		for _, inst := range instances {
			remote, err := s.searchRemoteInstance(inst.InstanceURL, q)
			if err != nil {
				log.Printf("Federation search error for %s: %v", inst.InstanceURL, err)
				continue
			}
			remoteUsers = append(remoteUsers, remote...)
		}
		result["remote"] = remoteUsers
	}

	utils.JSON(w, http.StatusOK, result)
}

func (s *Service) searchRemoteInstance(instanceURL, q string) ([]any, error) {
	url := fmt.Sprintf("%s/federation/search?q=%s&scope=local", instanceURL, q)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if local, ok := result["local"].([]any); ok {
		return local, nil
	}
	return nil, nil
}

func (s *Service) handleFederatedPost(activity FederatedActivity) {
	var postData struct {
		ID        string   `json:"id"`
		Content   string   `json:"content"`
		MediaURLs []string `json:"media_urls"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(activity.Object, &postData); err != nil {
		return
	}

	// Get or create remote user
	authorID := s.getOrCreateRemoteUser(activity.Actor, activity.Instance)
	if authorID == "" {
		return
	}

	s.db.Exec(`
		INSERT INTO posts (author_id, content, visibility, federated_id, home_instance, is_remote, media_urls, created_at)
		VALUES ($1, $2, 'public', $3, $4, true, $5, $6)
		ON CONFLICT DO NOTHING
	`, authorID, postData.Content, postData.ID, activity.Instance, postData.MediaURLs, postData.CreatedAt)
}

func (s *Service) handleFederatedDelete(activity FederatedActivity) {
	var obj struct{ ID string `json:"id"` }
	json.Unmarshal(activity.Object, &obj)
	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE federated_id = $1`, obj.ID)
}

func (s *Service) handleFederatedFriendRequest(activity FederatedActivity) {
	var obj struct{ TargetUsername string `json:"target_username"` }
	json.Unmarshal(activity.Object, &obj)

	var targetID string
	s.db.Get(&targetID, "SELECT id FROM users WHERE username = $1 AND is_remote = false", obj.TargetUsername)
	if targetID == "" {
		return
	}

	requesterID := s.getOrCreateRemoteUser(activity.Actor, activity.Instance)
	if requesterID == "" {
		return
	}

	s.db.Exec(`
		INSERT INTO friendships (requester_id, addressee_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT DO NOTHING
	`, requesterID, targetID)
}

func (s *Service) handleFederatedFriendAccept(activity FederatedActivity) {
	var obj struct{ RequesterFederatedID string `json:"requester_federated_id"` }
	json.Unmarshal(activity.Object, &obj)

	var requesterID string
	s.db.Get(&requesterID, "SELECT id FROM users WHERE federated_id = $1", obj.RequesterFederatedID)

	acceptorID := s.getOrCreateRemoteUser(activity.Actor, activity.Instance)

	if requesterID != "" && acceptorID != "" {
		s.db.Exec(`
			UPDATE friendships SET status = 'accepted', updated_at = NOW()
			WHERE requester_id = $1 AND addressee_id = $2
		`, requesterID, acceptorID)
	}
}

func (s *Service) getOrCreateRemoteUser(actorHandle, instance string) string {
	// actorHandle is "username@instance.url"
	var username string
	for _, part := range []string{actorHandle} {
		if len(part) > 0 {
			username = part
		}
	}
	// Parse username@instance
	if at := len(actorHandle) - len(instance) - 1; at > 0 {
		username = actorHandle[:at]
	}

	var userID string
	err := s.db.Get(&userID, `SELECT id FROM users WHERE federated_id = $1`, actorHandle)
	if err == nil {
		return userID
	}

	// Fetch user info from remote instance
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/federation/users/%s", instance, username))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var remoteUser struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Bio         string `json:"bio"`
	}
	json.NewDecoder(resp.Body).Decode(&remoteUser)

	s.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, avatar_url, bio, is_remote, federated_id, home_instance, email_verified)
		VALUES ($1, $2, '', $3, $4, $5, true, $6, $7, true)
		ON CONFLICT (email) DO UPDATE SET display_name = EXCLUDED.display_name
		RETURNING id
	`,
		fmt.Sprintf("%s@%s", remoteUser.Username, instance),
		fmt.Sprintf("%s@federated", actorHandle),
		remoteUser.DisplayName,
		remoteUser.AvatarURL,
		remoteUser.Bio,
		actorHandle,
		instance,
	).Scan(&userID)

	return userID
}

// SendActivity sends a signed activity to a remote instance
func (s *Service) SendActivity(instanceURL string, activity FederatedActivity) error {
	if s.privKey == nil {
		return fmt.Errorf("no private key configured")
	}

	activity.Signature = ""
	unsigned, _ := json.Marshal(activity)
	sig := ed25519.Sign(s.privKey, unsigned)
	activity.Signature = base64.StdEncoding.EncodeToString(sig)

	body, _ := json.Marshal(activity)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(instanceURL+"/federation/inbox", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (s *Service) getOrFetchInstanceKey(domain string) (ed25519.PublicKey, error) {
	var pubKeyB64 string
	err := s.db.Get(&pubKeyB64, `SELECT public_key FROM federated_instances WHERE domain = $1`, domain)
	if err == nil && pubKeyB64 != "" {
		keyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
		if err != nil {
			return nil, err
		}
		return ed25519.PublicKey(keyBytes), nil
	}

	// Fetch from instance
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://%s/.well-known/agora", domain))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info InstanceInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	keyBytes, err := base64.StdEncoding.DecodeString(info.PublicKey)
	if err != nil {
		return nil, err
	}

	// Store/update instance
	s.db.Exec(`
		INSERT INTO federated_instances (domain, name, public_key, instance_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (domain) DO UPDATE SET public_key = $3, name = $2, last_seen_at = NOW()
	`, domain, info.Name, info.PublicKey, info.URL)

	return ed25519.PublicKey(keyBytes), nil
}

func (s *Service) StartBackgroundSync(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.isEnabled() {
				s.refreshInstances()
			}
		}
	}
}

func (s *Service) refreshInstances() {
	var domains []string
	s.db.Select(&domains, `SELECT domain FROM federated_instances WHERE status = 'active'`)
	for _, domain := range domains {
		s.getOrFetchInstanceKey(domain)
	}
}

func ensureKeyPair(db *sqlx.DB) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	var privKeyB64 string
	err := db.Get(&privKeyB64, `SELECT value FROM instance_settings WHERE key = 'federation_private_key'`)
	if err == nil && privKeyB64 != "" {
		privBytes, err := base64.StdEncoding.DecodeString(privKeyB64)
		if err != nil {
			return nil, nil, err
		}
		priv := ed25519.PrivateKey(privBytes)
		return priv.Public().(ed25519.PublicKey), priv, nil
	}

	// Generate new key pair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	privB64 := base64.StdEncoding.EncodeToString(priv)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('federation_private_key', $1) ON CONFLICT (key) DO UPDATE SET value = $1`, privB64)

	log.Println("Generated new federation key pair")
	return pub, priv, nil
}
