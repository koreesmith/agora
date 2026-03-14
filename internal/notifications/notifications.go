package notifications

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// ── Service ───────────────────────────────────────────────────────────────────

type Service struct {
	db    *store.DB
	email *EmailService
}

func NewService(db *store.DB, email *EmailService) *Service {
	return &Service{db: db, email: email}
}

type Notification struct {
	ID               string  `json:"id"`
	Type             string  `json:"type"`
	ActorID          *string `json:"actor_id"`
	ActorUsername    *string `json:"actor_username"`
	ActorDisplayName *string `json:"actor_display_name"`
	ActorAvatarURL   *string `json:"actor_avatar_url"`
	PostID           *string `json:"post_id"`
	Data             string  `json:"data"`
	Read             bool    `json:"read"`
	FriendStatus     string  `json:"friend_status,omitempty"`
	CreatedAt        string  `json:"created_at"`
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/notifications",                    s.List)
	r.Get("/notifications/unread-count",       s.UnreadCount)
	r.Post("/notifications/read-all",          s.MarkAllRead)
	r.Post("/notifications/{id}/read",         s.MarkRead)
	r.Get("/notifications/email-preferences",  s.GetEmailPrefs)
	r.Put("/notifications/email-preferences",  s.UpdateEmailPrefs)
}

func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	userID := ctxkeys.GetUserID(r.Context())
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	rows, err := s.db.Query(`
		SELECT n.id, n.type,
		       n.actor_id, u.username, u.display_name, u.avatar_url,
		       n.post_id, n.data, n.read, n.created_at
		FROM notifications n
		LEFT JOIN users u ON u.id = n.actor_id
		WHERE n.user_id = $1
		ORDER BY n.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()
	var notifs []Notification
	for rows.Next() {
		var n Notification
		rows.Scan(&n.ID, &n.Type, &n.ActorID, &n.ActorUsername, &n.ActorDisplayName,
			&n.ActorAvatarURL, &n.PostID, &n.Data, &n.Read, &n.CreatedAt)
		notifs = append(notifs, n)
	}
	for i, n := range notifs {
		if n.Type == "friend_request" && n.ActorID != nil {
			var status, requesterID string
			s.db.QueryRow(`
				SELECT status, requester_id FROM friendships
				WHERE (requester_id = $1 AND addressee_id = $2)
				   OR (requester_id = $2 AND addressee_id = $1)
			`, *n.ActorID, userID).Scan(&status, &requesterID)
			if status == "accepted" {
				notifs[i].FriendStatus = "accepted"
			} else if status == "pending" && requesterID == *n.ActorID {
				notifs[i].FriendStatus = "pending_incoming"
			} else if status == "pending" {
				notifs[i].FriendStatus = "pending_outgoing"
			}
		}
	}
	if notifs == nil {
		notifs = []Notification{}
	}
	writeJSON(w, 200, map[string]any{"notifications": notifs})
}

func (s *Service) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID := ctxkeys.GetUserID(r.Context())
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = false`, userID).Scan(&count)
	writeJSON(w, 200, map[string]int{"count": count})
}

func (s *Service) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID := ctxkeys.GetUserID(r.Context())
	s.db.Exec(`UPDATE notifications SET read = true WHERE user_id = $1`, userID)
	writeJSON(w, 200, map[string]string{"message": "ok"})
}

func (s *Service) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID := ctxkeys.GetUserID(r.Context())
	id := chi.URLParam(r, "id")
	s.db.Exec(`UPDATE notifications SET read = true WHERE id = $1 AND user_id = $2`, id, userID)
	writeJSON(w, 200, map[string]string{"message": "ok"})
}

func (s *Service) GetEmailPrefs(w http.ResponseWriter, r *http.Request) {
	userID := ctxkeys.GetUserID(r.Context())
	var enabled bool
	s.db.QueryRow(`SELECT email_notifications_enabled FROM users WHERE id = $1`, userID).Scan(&enabled)
	writeJSON(w, 200, map[string]bool{"email_notifications_enabled": enabled})
}

func (s *Service) UpdateEmailPrefs(w http.ResponseWriter, r *http.Request) {
	userID := ctxkeys.GetUserID(r.Context())
	var req struct {
		Enabled bool `json:"email_notifications_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request"); return
	}
	s.db.Exec(`UPDATE users SET email_notifications_enabled = $1, updated_at = NOW() WHERE id = $2`, req.Enabled, userID)
	writeJSON(w, 200, map[string]bool{"email_notifications_enabled": req.Enabled})
}

// ── Create (called by other packages) ────────────────────────────────────────

func (s *Service) Create(userID, actorID, notifType, postID, data string) {
	var pID *string
	if postID != "" { pID = &postID }
	var aID *string
	if actorID != "" { aID = &actorID }
	s.db.Exec(`INSERT INTO notifications (user_id, actor_id, type, post_id, data) VALUES ($1,$2,$3,$4,$5)`,
		userID, aID, notifType, pID, data)
	go s.maybeEmailNotif(userID, actorID, notifType)
}

func (s *Service) maybeEmailNotif(userID, actorID, notifType string) {
	if !s.email.enabled() { return }

	// Check user's email notification preference (never blocks verification emails)
	var toEmail, displayName string
	var emailNotifsEnabled bool
	if err := s.db.QueryRow(`
		SELECT email, display_name, email_notifications_enabled
		FROM users WHERE id = $1 AND email_verified = true
	`, userID).Scan(&toEmail, &displayName, &emailNotifsEnabled); err != nil {
		return
	}
	if !emailNotifsEnabled { return }

	// Look up actor name
	actorName := "Someone"
	if actorID != "" {
		var aDisplay, aUsername string
		s.db.QueryRow(`SELECT display_name, username FROM users WHERE id = $1`, actorID).
			Scan(&aDisplay, &aUsername)
		if aDisplay != "" {
			actorName = aDisplay
		} else if aUsername != "" {
			actorName = aUsername
		}
	}

	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()
	baseURL := s.email.instanceBaseURL()

	subject, body := notifEmailContent(notifType, actorName, instanceName, baseURL)
	if subject == "" { return }

	s.email.Send(toEmail, subject, buildBody(displayName, instanceName, domain, baseURL, body))
}

func notifEmailContent(t, actorName, instanceName, baseURL string) (subject, body string) {
	switch t {
	case "friend_request":
		return fmt.Sprintf("New friend request on %s", instanceName),
			fmt.Sprintf("%s sent you a friend request!\n\nHead to %s/friends to accept or decline.", actorName, baseURL)
	case "friend_accepted":
		return fmt.Sprintf("%s accepted your friend request!", actorName),
			fmt.Sprintf("Great news — %s accepted your friend request on %s.\n\nView their profile: %s/profile/%s",
				actorName, instanceName, baseURL, strings.ToLower(strings.ReplaceAll(actorName, " ", "-")))
	case "post_like":
		return fmt.Sprintf("%s liked your post", actorName),
			fmt.Sprintf("%s liked one of your posts on %s.", actorName, instanceName)
	case "post_comment":
		return fmt.Sprintf("%s commented on your post", actorName),
			fmt.Sprintf("%s left a comment on your post on %s.\n\nHead to %s to see what they said.", actorName, instanceName, baseURL)
	case "post_repost":
		return fmt.Sprintf("%s shared your post", actorName),
			fmt.Sprintf("%s shared one of your posts on %s.", actorName, instanceName)
	case "post_mention":
		return fmt.Sprintf("%s mentioned you in a post", actorName),
			fmt.Sprintf("%s mentioned you in a post on %s.\n\nHead to %s to see what they said.", actorName, instanceName, baseURL)
	}
	return "", ""
}

func buildBody(displayName, instanceName, _, baseURL, content string) string {
	return fmt.Sprintf(`Hi %s,

%s

──────────────────────────────
This notification was sent by %s (%s).
To turn off email notifications, go to Settings → Notifications.
`, displayName, content, instanceName, baseURL)
}

// ── Transactional emails (verification, password reset) ───────────────────────
// These always send regardless of email_notifications_enabled.

func (s *Service) SendEmailVerification(_, email, displayName, token string) {
	if !s.email.enabled() { return }
	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()
	baseURL := s.email.instanceBaseURL()
	link := fmt.Sprintf("%s/verify-email?token=%s", baseURL, token)
	body := fmt.Sprintf(`Hi %s,

Welcome to %s! Please verify your email address by clicking the link below:

%s

This link expires in 24 hours. If you didn't create an account on %s, you can safely ignore this email.

──────────────────────────────
%s (%s)
`, displayName, instanceName, link, instanceName, instanceName, domain)
	s.email.Send(email, fmt.Sprintf("Verify your email address — %s", instanceName), body)
}

func (s *Service) SendPasswordReset(_, email, displayName, token string) {
	if !s.email.enabled() { return }
	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()
	baseURL := s.email.instanceBaseURL()
	link := fmt.Sprintf("%s/reset-password?token=%s", baseURL, token)
	body := fmt.Sprintf(`Hi %s,

You requested a password reset for your account on %s.

Reset your password here:
%s

This link expires in 2 hours. If you didn't request this, you can safely ignore this email — your password has not been changed.

──────────────────────────────
%s (%s)
`, displayName, instanceName, link, instanceName, domain)
	s.email.Send(email, fmt.Sprintf("Reset your password — %s", instanceName), body)
}

func (s *Service) SendModerationAction(userID, action, reason string) {
	if !s.email.enabled() { return }
	var email, displayName string
	if err := s.db.QueryRow(`SELECT email, display_name FROM users WHERE id = $1`, userID).
		Scan(&email, &displayName); err != nil {
		return
	}
	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()
	body := fmt.Sprintf(`Hi %s,

A moderation action has been taken on your account on %s.

Action: %s
Reason: %s

If you have questions, please contact the instance administrators.

──────────────────────────────
%s (%s)
`, displayName, instanceName, action, reason, instanceName, domain)
	s.email.Send(email, fmt.Sprintf("Moderation action on your %s account", instanceName), body)
}

// ── Email Service ─────────────────────────────────────────────────────────────

type EmailService struct {
	db  *store.DB
	cfg *config.Config
}

func NewEmailService(db *store.DB, cfg *config.Config) *EmailService {
	return &EmailService{db: db, cfg: cfg}
}

func (e *EmailService) enabled() bool {
	var val string
	e.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'smtp_enabled'`).Scan(&val)
	return val == "true"
}

func (e *EmailService) instanceDomain() string {
	var val string
	e.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_domain'`).Scan(&val)
	if val == "" { val = e.cfg.InstanceDomain }
	// Strip protocol — return bare domain only (e.g. "ameth.social")
	val = strings.TrimPrefix(val, "https://")
	val = strings.TrimPrefix(val, "http://")
	val = strings.TrimSuffix(val, "/")
	return val
}

// instanceBaseURL returns the full base URL for use in links, inferring https
// unless the stored value explicitly uses http (for local/dev environments).
func (e *EmailService) instanceBaseURL() string {
	var val string
	e.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_domain'`).Scan(&val)
	if val == "" { val = e.cfg.InstanceDomain }
	val = strings.TrimSuffix(val, "/")
	// If already has a protocol, use it as-is
	if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
		return val
	}
	return "https://" + val
}

func (e *EmailService) instanceName() string {
	var val string
	e.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_name'`).Scan(&val)
	if val == "" { return "Agora" }
	return val
}

func (e *EmailService) smtpConfig() (host, port, user, pass, from string) {
	rows, err := e.db.Query(`SELECT key, value FROM instance_settings WHERE key IN ('smtp_host','smtp_port','smtp_user','smtp_password','smtp_from')`)
	if err != nil { return }
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		m[k] = v
	}
	return m["smtp_host"], m["smtp_port"], m["smtp_user"], m["smtp_password"], m["smtp_from"]
}

func (e *EmailService) Send(to, subject, body string) error {
	host, portStr, user, pass, from := e.smtpConfig()
	log.Printf("email: sending to=%s subject=%q via %s", to, subject, host)
	if host == "" {
		log.Println("email: smtp_host not configured, skipping")
		return fmt.Errorf("smtp not configured")
	}
	if portStr == "" { portStr = "587" }

	domain := e.instanceDomain()
	instanceName := e.instanceName()
	fromHeader := fmt.Sprintf("%s <%s>", instanceName, from)
	msgID := fmt.Sprintf("<%s.%s@%s>", randomID(), randomID(), domain)
	date := time.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700")

	headers := []string{
		"From: " + fromHeader,
		"To: " + to,
		"Reply-To: " + fromHeader,
		"Subject: " + subject,
		"Date: " + date,
		"Message-ID: " + msgID,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"X-Mailer: Agora Social",
		"Precedence: bulk",
		"Auto-Submitted: auto-generated",
	}

	msg := strings.Join(headers, "\r\n") + "\r\n\r\n" + body

	addr := host + ":" + portStr
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	err := smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
	if err != nil {
		log.Printf("email: send failed to=%s err=%v", to, err)
	} else {
		log.Printf("email: sent successfully to=%s", to)
	}
	return err
}

func randomID() string {
	b := make([]byte, 8)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(36))
		const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
		b[i] = chars[n.Int64()]
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
