package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db       *store.DB
	cfg      *config.Config
	notifSvc notifSender
	media    mediaStore
}

type notifSender interface {
	SendEmailVerification(userID, email, displayName, token string)
	SendWaitlistConfirmation(userID, email, displayName string)
	SendWaitlistApproved(email, displayName, acceptURL string)
}

type mediaStore interface {
	UploadDir() string
}

func NewService(db *store.DB, cfg *config.Config, notifSvc notifSender, media mediaStore) *Service {
	return &Service{db: db, cfg: cfg, notifSvc: notifSvc, media: media}
}

func RegisterRoutes(r chi.Router, s *Service) {
	// Settings
	r.Get("/admin/settings",  s.GetSettings)
	r.Patch("/admin/settings", s.UpdateSettings)

	// Stats
	r.Get("/admin/stats", s.GetStats)

	// User management
	r.Get("/admin/users",                    s.ListUsers)
	r.Patch("/admin/users/{userID}/role",    s.SetRole)
	r.Delete("/admin/users/{userID}",        s.DeleteUser)
	r.Post("/admin/users/{userID}/resend-verification", s.ResendVerification)

	// Invite codes
	r.Get("/admin/invites",        s.ListInvites)
	r.Post("/admin/invites",       s.CreateInvite)
	r.Delete("/admin/invites/{id}", s.RevokeInvite)

	// Audit log
	r.Get("/admin/audit-log", s.GetAuditLog)

	// Federation
	r.Get("/admin/federation/instances",               s.ListInstances)
	r.Post("/admin/federation/instances",              s.AddInstance)
	r.Post("/admin/federation/instances/{id}/block",   s.BlockInstance)
	r.Post("/admin/federation/instances/{id}/unblock", s.UnblockInstance)

	// Instance rules
	r.Get("/admin/rules",             s.ListRules)
	r.Post("/admin/rules",            s.CreateRule)
	r.Patch("/admin/rules/{id}",      s.UpdateRule)
	r.Delete("/admin/rules/{id}",     s.DeleteRule)
	r.Patch("/admin/rules/{id}/move", s.MoveRule)

	// Waitlist
	r.Get("/admin/waitlist",              s.ListWaitlist)
	r.Post("/admin/waitlist/{id}/approve", s.ApproveWaitlist)
	r.Delete("/admin/waitlist/{id}",      s.RejectWaitlist)

	// Media cleanup
	r.Get("/admin/media/orphans",    s.ScanOrphans)
	r.Delete("/admin/media/orphans", s.DeleteOrphans)
}

// ── Settings ──────────────────────────────────────────────────────────────────

// adminEditableSettings is an explicit allowlist of instance_settings keys
// the admin panel can read and write (AGORA-143). Anything not listed here —
// notably federation_public_key/federation_private_key, the instance-wide
// signing keypair — is never serialized, regardless of what's in the table.
// Shared by GetSettings and UpdateSettings so the two can't drift apart.
var adminEditableSettings = map[string]bool{
	"instance_name": true, "instance_description": true, "registration_mode": true,
	"federation_enabled": true, "activitypub_enabled": true, "deletion_grace_days": true, "logo_url": true,
	"smtp_host": true, "smtp_port": true, "smtp_user": true, "smtp_password": true,
	"smtp_from": true, "smtp_enabled": true, "user_invites_enabled": true,
}

func (s *Service) GetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT key, value FROM instance_settings ORDER BY key`)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	settings := map[string]string{}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		if !adminEditableSettings[k] {
			continue
		}
		// Redact SMTP password in response
		if k == "smtp_password" && v != "" {
			v = "••••••••"
		}
		settings[k] = v
	}
	writeJSON(w, 200, settings)
}

func (s *Service) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	actorID := auth.UserIDFromCtx(r.Context())
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	for k, v := range updates {
		if !adminEditableSettings[k] {
			continue
		}
		// Don't overwrite password if placeholder sent
		if k == "smtp_password" && v == "••••••••" {
			continue
		}
		s.db.Exec(`
			INSERT INTO instance_settings (key, value, updated_at) VALUES ($1, $2, NOW())
			ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
		`, k, v)
	}

	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, details) VALUES ($1, 'update_settings', 'settings', '')`, actorID)

	writeJSON(w, 200, map[string]string{"message": "settings updated"})
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func (s *Service) GetStats(w http.ResponseWriter, r *http.Request) {
	var totalUsers, postsToday, activeUsers7d, pendingReports int

	s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_remote = false`).Scan(&totalUsers)
	s.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE created_at > NOW() - INTERVAL '1 day' AND deleted_at IS NULL`).Scan(&postsToday)
	s.db.QueryRow(`
		SELECT COUNT(DISTINCT author_id) FROM posts
		WHERE created_at > NOW() - INTERVAL '7 days' AND deleted_at IS NULL
	`).Scan(&activeUsers7d)
	s.db.QueryRow(`SELECT COUNT(*) FROM reports WHERE status = 'pending'`).Scan(&pendingReports)

	writeJSON(w, 200, map[string]int{
		"total_users":      totalUsers,
		"posts_today":      postsToday,
		"active_users_7d":  activeUsers7d,
		"pending_reports":  pendingReports,
	})
}

// ── User management ───────────────────────────────────────────────────────────

func (s *Service) ListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	var rows interface {
		Next() bool
		Scan(...any) error
		Close() error
	}
	var err error

	if q != "" {
		rows, err = s.db.Query(`
			SELECT id, username, email, display_name, role, is_suspended, email_verified, created_at
			FROM users
			WHERE is_remote = false
			  AND (username ILIKE '%'||$1||'%' OR email ILIKE '%'||$1||'%' OR display_name ILIKE '%'||$1||'%')
			ORDER BY created_at DESC LIMIT 100
		`, q)
	} else {
		rows, err = s.db.Query(`
			SELECT id, username, email, display_name, role, is_suspended, email_verified, created_at
			FROM users WHERE is_remote = false
			ORDER BY created_at DESC LIMIT 100
		`)
	}
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type User struct {
		ID            string `json:"id"`
		Username      string `json:"username"`
		Email         string `json:"email"`
		DisplayName   string `json:"display_name"`
		Role          string `json:"role"`
		IsSuspended   bool   `json:"is_suspended"`
		EmailVerified bool   `json:"email_verified"`
		CreatedAt     string `json:"created_at"`
	}
	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.Role, &u.IsSuspended, &u.EmailVerified, &u.CreatedAt)
		users = append(users, u)
	}
	if users == nil { users = []User{} }
	writeJSON(w, 200, map[string]any{"users": users})
}

func (s *Service) SetRole(w http.ResponseWriter, r *http.Request) {
	actorID := auth.UserIDFromCtx(r.Context())
	userID := chi.URLParam(r, "userID")
	var req struct{ Role string `json:"role"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if req.Role != "user" && req.Role != "moderator" && req.Role != "admin" {
		writeError(w, 400, "invalid role")
		return
	}
	s.db.Exec(`UPDATE users SET role = $1 WHERE id = $2`, req.Role, userID)
	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id, details) VALUES ($1, 'set_role', 'user', $2, $3)`,
		actorID, userID, req.Role)
	writeJSON(w, 200, map[string]string{"message": "role updated"})
}

func (s *Service) DeleteUser(w http.ResponseWriter, r *http.Request) {
	actorID := auth.UserIDFromCtx(r.Context())
	userID := chi.URLParam(r, "userID")

	var username string
	s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
	if username == "admin" {
		writeError(w, 403, "cannot delete the admin account")
		return
	}

	s.db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id, details) VALUES ($1, 'delete_user', 'user', $2, $3)`,
		actorID, userID, username)

	writeJSON(w, 200, map[string]string{"message": "user deleted"})
}

// ── Invites ───────────────────────────────────────────────────────────────────

func (s *Service) ListInvites(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT i.id, i.code, i.created_at, i.expires_at, i.used_at,
		       creator.username,
		       used.username
		FROM invite_codes i
		JOIN users creator ON creator.id = i.created_by
		LEFT JOIN users used ON used.id = i.used_by
		ORDER BY i.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type Invite struct {
		ID              string  `json:"id"`
		Code            string  `json:"code"`
		CreatedAt       string  `json:"created_at"`
		ExpiresAt       *string `json:"expires_at"`
		UsedAt          *string `json:"used_at"`
		CreatedByUsername string `json:"created_by_username"`
		UsedByUsername  *string `json:"used_by_username"`
	}
	var invites []Invite
	for rows.Next() {
		var inv Invite
		rows.Scan(&inv.ID, &inv.Code, &inv.CreatedAt, &inv.ExpiresAt, &inv.UsedAt,
			&inv.CreatedByUsername, &inv.UsedByUsername)
		invites = append(invites, inv)
	}
	if invites == nil { invites = []Invite{} }
	writeJSON(w, 200, map[string]any{"invites": invites})
}

func (s *Service) CreateInvite(w http.ResponseWriter, r *http.Request) {
	creatorID := auth.UserIDFromCtx(r.Context())
	var req struct{ ExpiresAt *string `json:"expires_at"` }
	json.NewDecoder(r.Body).Decode(&req)

	b := make([]byte, 16)
	rand.Read(b)
	code := hex.EncodeToString(b)

	var id string
	err := s.db.QueryRow(`
		INSERT INTO invite_codes (code, created_by, expires_at)
		VALUES ($1, $2, $3) RETURNING id
	`, code, creatorID, req.ExpiresAt).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not create invite")
		return
	}
	writeJSON(w, 201, map[string]string{"id": id, "code": code})
}

func (s *Service) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM invite_codes WHERE id = $1 AND used_by IS NULL`, id)
	writeJSON(w, 200, map[string]string{"message": "invite revoked"})
}

// ── Audit log ─────────────────────────────────────────────────────────────────

func (s *Service) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT a.id, a.action, a.target_type, a.target_id, a.details, a.created_at,
		       u.username
		FROM audit_log a
		LEFT JOIN users u ON u.id = a.actor_id
		ORDER BY a.created_at DESC
		LIMIT 200
	`)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type Entry struct {
		ID         string  `json:"id"`
		Action     string  `json:"action"`
		TargetType string  `json:"target_type"`
		TargetID   string  `json:"target_id"`
		Details    string  `json:"details"`
		CreatedAt  string  `json:"created_at"`
		Actor      *string `json:"actor_username"`
	}
	var entries []Entry
	for rows.Next() {
		var e Entry
		rows.Scan(&e.ID, &e.Action, &e.TargetType, &e.TargetID, &e.Details, &e.CreatedAt, &e.Actor)
		entries = append(entries, e)
	}
	if entries == nil { entries = []Entry{} }
	writeJSON(w, 200, map[string]any{"entries": entries})
}

// ── Federation ────────────────────────────────────────────────────────────────

func (s *Service) AddInstance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Domain) == "" {
		writeError(w, 400, "domain required")
		return
	}
	domain := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(req.Domain), "https://"), "http://")
	domain = strings.Split(domain, "/")[0]

	// Fetch instance info to verify it's a real Agora instance
	instanceURL := "https://" + domain
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(instanceURL + "/.well-known/agora-instance")
	if err != nil || resp.StatusCode != 200 {
		writeError(w, 422, "could not reach instance — make sure it is an Agora instance with federation enabled")
		return
	}
	defer resp.Body.Close()

	var info struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		writeError(w, 422, "invalid response from instance")
		return
	}

	s.db.Exec(`
		INSERT INTO federated_instances (domain, name, public_key, instance_url, status)
		VALUES ($1, $2, $3, $4, 'active')
		ON CONFLICT (domain) DO UPDATE
		  SET name = $2, public_key = $3, instance_url = $4, status = 'active', last_seen_at = NOW()
	`, domain, info.Name, info.PublicKey, instanceURL)

	actorID := auth.UserIDFromCtx(r.Context())
	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id, details) VALUES ($1, 'add_instance', 'instance', $2, $3)`,
		actorID, domain, instanceURL)

	writeJSON(w, 201, map[string]string{"message": "instance added", "domain": domain, "name": info.Name})
}

func (s *Service) ListInstances(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT id, domain, name, instance_url, status, last_seen_at, created_at
		FROM federated_instances
		ORDER BY last_seen_at DESC
	`)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type Instance struct {
		ID          string `json:"id"`
		Domain      string `json:"domain"`
		Name        string `json:"name"`
		InstanceURL string `json:"instance_url"`
		Status      string `json:"status"`
		LastSeenAt  string `json:"last_seen_at"`
		CreatedAt   string `json:"created_at"`
	}
	var instances []Instance
	for rows.Next() {
		var inst Instance
		rows.Scan(&inst.ID, &inst.Domain, &inst.Name, &inst.InstanceURL,
			&inst.Status, &inst.LastSeenAt, &inst.CreatedAt)
		instances = append(instances, inst)
	}
	if instances == nil { instances = []Instance{} }
	writeJSON(w, 200, map[string]any{"instances": instances})
}

func (s *Service) BlockInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actorID := auth.UserIDFromCtx(r.Context())
	s.db.Exec(`UPDATE federated_instances SET status = 'blocked' WHERE id = $1`, id)
	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id) VALUES ($1, 'block_instance', 'instance', $2)`, actorID, id)
	writeJSON(w, 200, map[string]string{"message": "instance blocked"})
}

func (s *Service) UnblockInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Exec(`UPDATE federated_instances SET status = 'active' WHERE id = $1`, id)
	writeJSON(w, 200, map[string]string{"message": "instance unblocked"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = strings.Contains

func (s *Service) ResendVerification(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")

	var email, displayName string
	var verified bool
	err := s.db.QueryRow(`SELECT email, display_name, email_verified FROM users WHERE id = $1 AND is_remote = false`, userID).
		Scan(&email, &displayName, &verified)
	if err != nil {
		writeError(w, 404, "user not found"); return
	}
	if verified {
		writeError(w, 400, "email already verified"); return
	}

	token, err := randomHex(32)
	if err != nil {
		writeError(w, 500, "server error"); return
	}

	_, err = s.db.Exec(`
		UPDATE users SET email_verify_token = $1, email_verify_expires = NOW() + INTERVAL '24 hours'
		WHERE id = $2
	`, token, userID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}

	go s.notifSvc.SendEmailVerification(userID, email, displayName, token)

	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id, details)
		VALUES ($1, 'resend_verification', 'user', $2, $3)`,
		auth.UserIDFromCtx(r.Context()), userID, email)

	writeJSON(w, 200, map[string]string{"message": "verification email sent"})
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ── Instance Rules ────────────────────────────────────────────────────────────

type Rule struct {
	ID        string `json:"id"`
	Position  int    `json:"position"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

func (s *Service) ListRules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, position, text, created_at FROM instance_rules ORDER BY position ASC, created_at ASC`)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()
	var rules []Rule
	for rows.Next() {
		var rule Rule
		rows.Scan(&rule.ID, &rule.Position, &rule.Text, &rule.CreatedAt)
		rules = append(rules, rule)
	}
	if rules == nil { rules = []Rule{} }
	writeJSON(w, 200, map[string]any{"rules": rules})
}

func (s *Service) CreateRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, 400, "text required"); return
	}
	var maxPos int
	s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) FROM instance_rules`).Scan(&maxPos)
	var id string
	err := s.db.QueryRow(`INSERT INTO instance_rules (position, text) VALUES ($1, $2) RETURNING id`,
		maxPos+1, strings.TrimSpace(req.Text)).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not create rule"); return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Service) UpdateRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, 400, "text required"); return
	}
	s.db.Exec(`UPDATE instance_rules SET text = $1 WHERE id = $2`, strings.TrimSpace(req.Text), id)
	writeJSON(w, 200, map[string]string{"message": "updated"})
}

func (s *Service) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM instance_rules WHERE id = $1`, id)
	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

func (s *Service) MoveRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Direction string `json:"direction"` // "up" or "down"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}
	if req.Direction != "up" && req.Direction != "down" {
		writeError(w, 400, "direction must be 'up' or 'down'"); return
	}

	var pos int
	s.db.QueryRow(`SELECT position FROM instance_rules WHERE id = $1`, id).Scan(&pos)
	if pos == 0 {
		writeError(w, 404, "rule not found"); return
	}

	var swapID string
	var swapPos int
	if req.Direction == "up" {
		s.db.QueryRow(`SELECT id, position FROM instance_rules WHERE position < $1 ORDER BY position DESC LIMIT 1`, pos).Scan(&swapID, &swapPos)
	} else {
		s.db.QueryRow(`SELECT id, position FROM instance_rules WHERE position > $1 ORDER BY position ASC LIMIT 1`, pos).Scan(&swapID, &swapPos)
	}
	if swapID == "" {
		writeJSON(w, 200, map[string]string{"message": "already at boundary"}); return
	}
	s.db.Exec(`UPDATE instance_rules SET position = $1 WHERE id = $2`, swapPos, id)
	s.db.Exec(`UPDATE instance_rules SET position = $1 WHERE id = $2`, pos, swapID)
	writeJSON(w, 200, map[string]string{"message": "moved"})
}

// ── Waitlist ──────────────────────────────────────────────────────────────────

func (s *Service) ListWaitlist(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT id, username, email, display_name, created_at, email_verified
		FROM users
		WHERE waitlist_status = 'pending' AND deletion_scheduled_at IS NULL
		ORDER BY created_at ASC
	`)
	if err != nil { writeError(w, 500, "db error"); return }
	defer rows.Close()

	type WaitlistUser struct {
		ID            string `json:"id"`
		Username      string `json:"username"`
		Email         string `json:"email"`
		DisplayName   string `json:"display_name"`
		CreatedAt     string `json:"created_at"`
		EmailVerified bool   `json:"email_verified"`
	}

	var users []WaitlistUser
	for rows.Next() {
		var u WaitlistUser
		rows.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.CreatedAt, &u.EmailVerified)
		users = append(users, u)
	}
	if users == nil { users = []WaitlistUser{} }
	writeJSON(w, 200, map[string]any{"users": users})
}

func (s *Service) ApproveWaitlist(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	var email, displayName, waitlistToken string
	err := s.db.QueryRow(`
		SELECT email, display_name, waitlist_token FROM users
		WHERE id = $1 AND waitlist_status = 'pending'
	`, userID).Scan(&email, &displayName, &waitlistToken)
	if err != nil { writeError(w, 404, "user not found or not on waitlist"); return }

	// Mark approved
	_, err = s.db.Exec(`
		UPDATE users SET waitlist_status = 'approved', email_verified = true WHERE id = $1
	`, userID)
	if err != nil { writeError(w, 500, "db error"); return }

	// Build accept URL — ensure no double scheme
	domain := s.cfg.InstanceDomain
	if !strings.HasPrefix(domain, "http://") && !strings.HasPrefix(domain, "https://") {
		domain = "https://" + domain
	}
	domain = strings.TrimRight(domain, "/")
	acceptURL := domain + "/api/auth/waitlist/accept?token=" + waitlistToken
	go s.notifSvc.SendWaitlistApproved(email, displayName, acceptURL)

	writeJSON(w, 200, map[string]string{"message": "approved"})
}

func (s *Service) RejectWaitlist(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	res, err := s.db.Exec(`
		DELETE FROM users WHERE id = $1 AND waitlist_status = 'pending'
	`, userID)
	if err != nil { writeError(w, 500, "db error"); return }
	n, _ := res.RowsAffected()
	if n == 0 { writeError(w, 404, "user not found or not on waitlist"); return }
	writeJSON(w, 200, map[string]string{"message": "rejected"})
}
