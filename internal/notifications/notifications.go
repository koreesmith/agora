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
		return fmt.Sprintf("%s sent you a friend request on %s", actorName, instanceName),
			fmt.Sprintf("%s sent you a friend request on %s.\n\nAccept or decline: %s/friends", actorName, instanceName, baseURL)
	case "friend_accepted":
		return fmt.Sprintf("%s accepted your friend request", actorName),
			fmt.Sprintf("%s accepted your friend request on %s.\n\nView their profile: %s/friends", actorName, instanceName, baseURL)
	case "post_like":
		return fmt.Sprintf("%s liked your post on %s", actorName, instanceName),
			fmt.Sprintf("%s liked one of your posts on %s.\n\nSee what's happening: %s", actorName, instanceName, baseURL)
	case "post_reaction", "comment_reaction":
		return fmt.Sprintf("%s reacted to your post on %s", actorName, instanceName),
			fmt.Sprintf("%s reacted to one of your posts on %s.\n\nSee what's happening: %s", actorName, instanceName, baseURL)
	case "post_comment":
		return fmt.Sprintf("%s commented on your post", actorName),
			fmt.Sprintf("%s left a comment on your post on %s.\n\nSee the discussion: %s", actorName, instanceName, baseURL)
	case "post_repost":
		return fmt.Sprintf("%s shared your post on %s", actorName, instanceName),
			fmt.Sprintf("%s shared one of your posts on %s.\n\nSee what's happening: %s", actorName, instanceName, baseURL)
	case "post_mention":
		return fmt.Sprintf("%s mentioned you in a post", actorName),
			fmt.Sprintf("%s mentioned you in a post on %s.\n\nSee the post: %s", actorName, instanceName, baseURL)
	case "user_post":
		return fmt.Sprintf("%s just posted on %s", actorName, instanceName),
			fmt.Sprintf("%s just posted something new on %s.\n\nSee the post: %s", actorName, instanceName, baseURL)
	}
	return "", ""
}

func buildBody(displayName, instanceName, _, baseURL, content string) string {
	return fmt.Sprintf(`Hi %s,

%s

To manage your email notification preferences, visit:
%s/settings

— The %s team
`, displayName, content, baseURL, instanceName)
}

// ── Transactional emails (verification, password reset) ───────────────────────
// These always send regardless of email_notifications_enabled.

func (s *Service) SendEmailVerification(_, email, displayName, token string) {
	if !s.email.enabled() { return }
	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()
	baseURL := s.email.instanceBaseURL()
	link := fmt.Sprintf("%s/verify-email?token=%s", baseURL, token)

	subject := fmt.Sprintf("Verify your email address for %s", instanceName)

	plain := fmt.Sprintf(`Hi %s,

Welcome to %s! You're almost ready — just verify your email address to activate your account.

Verify your email here:
%s

This link expires in 24 hours.

If you didn't create an account on %s (%s), you can safely ignore this email.
Your email address will not be used for anything without your confirmation.

— The %s team
%s
`, displayName, instanceName, link, instanceName, domain, instanceName, domain)

	html := buildVerificationHTML(instanceName, domain, baseURL, displayName, link)
	s.email.SendHTML(email, subject, plain, html)
}

func (s *Service) SendPasswordReset(_, email, displayName, token string) {
	if !s.email.enabled() { return }
	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()
	baseURL := s.email.instanceBaseURL()
	link := fmt.Sprintf("%s/reset-password?token=%s", baseURL, token)

	subject := fmt.Sprintf("Reset your %s password", instanceName)

	plain := fmt.Sprintf(`Hi %s,

We received a request to reset the password for your account on %s (%s).

Reset your password here:
%s

This link expires in 2 hours. If you didn't request a password reset, you can safely ignore this email — your password has not been changed.

— The %s team
%s
`, displayName, instanceName, domain, link, instanceName, domain)

	html := buildPasswordResetHTML(instanceName, domain, baseURL, displayName, link)
	s.email.SendHTML(email, subject, plain, html)
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

A moderation action has been taken on your %s account.

Action: %s
Reason: %s

If you have questions, please contact the instance administrators at %s.

— The %s team
%s
`, displayName, instanceName, action, reason, domain, instanceName, domain)
	s.email.Send(email, fmt.Sprintf("Account notice from %s", instanceName), body)
}

// ── HTML email templates ──────────────────────────────────────────────────────

func emailBaseStyle() string {
	return `body{margin:0;padding:0;background:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif}
.wrap{max-width:560px;margin:32px auto;background:#ffffff;border-radius:12px;overflow:hidden;border:1px solid #e4e4e7}
.header{background:#7c3aed;padding:28px 32px;text-align:center}
.header h1{margin:0;color:#ffffff;font-size:22px;font-weight:700;letter-spacing:-0.3px}
.header p{margin:6px 0 0;color:#ddd6fe;font-size:13px}
.body{padding:32px}
.body p{margin:0 0 16px;color:#3f3f46;font-size:15px;line-height:1.6}
.cta{text-align:center;margin:28px 0}
.cta a{display:inline-block;background:#7c3aed;color:#ffffff !important;text-decoration:none;font-size:15px;font-weight:600;padding:14px 32px;border-radius:8px;letter-spacing:0.1px}
.link-fallback{margin:20px 0;padding:16px;background:#f4f4f5;border-radius:8px;word-break:break-all}
.link-fallback p{margin:0 0 6px;font-size:12px;color:#71717a;text-transform:uppercase;letter-spacing:0.5px;font-weight:600}
.link-fallback a{color:#7c3aed;font-size:13px;text-decoration:none}
.note{font-size:13px !important;color:#71717a !important}
.footer{padding:20px 32px;border-top:1px solid #e4e4e7;text-align:center}
.footer p{margin:0;font-size:12px;color:#a1a1aa;line-height:1.6}`
}

func buildVerificationHTML(instanceName, domain, _, displayName, link string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Verify your email — %s</title>
<style>%s</style></head>
<body>
<div class="wrap">
  <div class="header">
    <h1>%s</h1>
    <p>%s</p>
  </div>
  <div class="body">
    <p>Hi <strong>%s</strong>,</p>
    <p>Welcome to <strong>%s</strong>! You're one step away from joining the community. Please verify your email address to activate your account.</p>
    <div class="cta"><a href="%s">Verify my email address</a></div>
    <div class="link-fallback">
      <p>Or copy this link into your browser</p>
      <a href="%s">%s</a>
    </div>
    <p class="note">This link expires in <strong>24 hours</strong>. If you didn't sign up for %s, you can safely ignore this email.</p>
  </div>
  <div class="footer">
    <p>This email was sent by <strong>%s</strong> (%s).<br>
    You're receiving this because someone used this address to create an account.</p>
  </div>
</div>
</body></html>`,
		instanceName, emailBaseStyle(),
		instanceName, domain,
		displayName, instanceName,
		link,
		link, link,
		instanceName,
		instanceName, domain,
	)
}

func buildPasswordResetHTML(instanceName, domain, _, displayName, link string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Reset your password — %s</title>
<style>%s</style></head>
<body>
<div class="wrap">
  <div class="header">
    <h1>%s</h1>
    <p>%s</p>
  </div>
  <div class="body">
    <p>Hi <strong>%s</strong>,</p>
    <p>We received a request to reset the password for your <strong>%s</strong> account.</p>
    <div class="cta"><a href="%s">Reset my password</a></div>
    <div class="link-fallback">
      <p>Or copy this link into your browser</p>
      <a href="%s">%s</a>
    </div>
    <p class="note">This link expires in <strong>2 hours</strong>. If you didn't request a password reset, you can safely ignore this email — your password has not been changed.</p>
  </div>
  <div class="footer">
    <p>This email was sent by <strong>%s</strong> (%s).<br>
    To keep your account secure, never share this link with anyone.</p>
  </div>
</div>
</body></html>`,
		instanceName, emailBaseStyle(),
		instanceName, domain,
		displayName, instanceName,
		link,
		link, link,
		instanceName, domain,
	)
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
	val = strings.TrimPrefix(val, "https://")
	val = strings.TrimPrefix(val, "http://")
	val = strings.TrimSuffix(val, "/")
	return val
}

func (e *EmailService) instanceBaseURL() string {
	var val string
	e.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_domain'`).Scan(&val)
	if val == "" { val = e.cfg.InstanceDomain }
	val = strings.TrimSuffix(val, "/")
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

// Send sends a plain-text email.
func (e *EmailService) Send(to, subject, plainBody string) error {
	return e.SendHTML(to, subject, plainBody, "")
}

// SendHTML sends a multipart/alternative email with plain-text and optional HTML parts.
func (e *EmailService) SendHTML(to, subject, plainBody, htmlBody string) error {
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
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 +0000")

	// Encode subject as UTF-8 Q-encoded word to handle any special characters
	encodedSubject := fmt.Sprintf("=?UTF-8?Q?%s?=", qEncode(subject))

	var msg string
	if htmlBody != "" {
		boundary := "----=_Part_" + randomID()
		headers := []string{
			"From: " + fromHeader,
			"To: " + to,
			"Subject: " + encodedSubject,
			"Date: " + date,
			"Message-ID: " + msgID,
			"MIME-Version: 1.0",
			"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"",
			// Deliverability headers
			"X-Mailer: Agora/" + domain,
			"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
			"List-Unsubscribe: <" + e.instanceBaseURL() + "/settings>",
		}
		parts := []string{
			strings.Join(headers, "\r\n"),
			"",
			"--" + boundary,
			"Content-Type: text/plain; charset=UTF-8",
			"Content-Transfer-Encoding: quoted-printable",
			"",
			toQP(plainBody),
			"",
			"--" + boundary,
			"Content-Type: text/html; charset=UTF-8",
			"Content-Transfer-Encoding: quoted-printable",
			"",
			toQP(htmlBody),
			"",
			"--" + boundary + "--",
		}
		msg = strings.Join(parts, "\r\n")
	} else {
		headers := []string{
			"From: " + fromHeader,
			"To: " + to,
			"Subject: " + encodedSubject,
			"Date: " + date,
			"Message-ID: " + msgID,
			"MIME-Version: 1.0",
			"Content-Type: text/plain; charset=UTF-8",
			"Content-Transfer-Encoding: quoted-printable",
			"X-Mailer: Agora/" + domain,
			"List-Unsubscribe: <" + e.instanceBaseURL() + "/settings>",
		}
		msg = strings.Join(headers, "\r\n") + "\r\n\r\n" + toQP(plainBody)
	}

	addr := host + ":" + portStr
	var smtpAuth smtp.Auth
	if user != "" {
		smtpAuth = smtp.PlainAuth("", user, pass, host)
	}
	err := smtp.SendMail(addr, smtpAuth, from, []string{to}, []byte(msg))
	if err != nil {
		log.Printf("email: send failed to=%s err=%v", to, err)
	} else {
		log.Printf("email: sent successfully to=%s subject=%q", to, subject)
	}
	return err
}

// toQP encodes a string as quoted-printable for email transport.
func toQP(s string) string {
	var b strings.Builder
	lineLen := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' {
			b.WriteByte('\n')
			lineLen = 0
			continue
		}
		if c == '\r' {
			continue // strip bare CR, will be re-added as CRLF after \n
		}
		// Characters that must be encoded
		if c == '=' || c > 126 || (c < 32 && c != '\t') {
			enc := fmt.Sprintf("=%02X", c)
			if lineLen+3 > 75 {
				b.WriteString("=\r\n")
				lineLen = 0
			}
			b.WriteString(enc)
			lineLen += 3
		} else {
			if lineLen+1 > 75 {
				b.WriteString("=\r\n")
				lineLen = 0
			}
			b.WriteByte(c)
			lineLen++
		}
	}
	return b.String()
}

// qEncode encodes a string for use in an email header Q-encoding.
func qEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		} else if c == ' ' {
			b.WriteByte('_')
		} else {
			fmt.Fprintf(&b, "=%02X", c)
		}
	}
	return b.String()
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
