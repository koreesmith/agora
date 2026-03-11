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
	FriendStatus     string  `json:"friend_status,omitempty"` // for friend_request type
	CreatedAt        string  `json:"created_at"`
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/notifications",              s.List)
	r.Get("/notifications/unread-count", s.UnreadCount)
	r.Post("/notifications/read-all",    s.MarkAllRead)
	r.Post("/notifications/{id}/read",   s.MarkRead)
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
	// For friend_request notifications, look up the current friendship status
	// so the frontend can show "accepted" even after a page reload
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

// ── Create helpers (called by other packages) ─────────────────────────────────

func (s *Service) Create(userID, actorID, notifType, postID, data string) {
	var pID *string
	if postID != "" { pID = &postID }
	var aID *string
	if actorID != "" { aID = &actorID }
	s.db.Exec(`INSERT INTO notifications (user_id, actor_id, type, post_id, data) VALUES ($1,$2,$3,$4,$5)`,
		userID, aID, notifType, pID, data)
	go s.maybeEmailNotif(userID, notifType)
}

func (s *Service) maybeEmailNotif(userID, notifType string) {
	if !s.email.enabled() { return }
	var email, displayName string
	if err := s.db.QueryRow(`SELECT email, display_name FROM users WHERE id = $1 AND email_verified = true`, userID).
		Scan(&email, &displayName); err != nil {
		return
	}
	subject, body := notifEmailContent(notifType)
	if subject == "" { return }
	s.email.Send(email, subject, fmt.Sprintf("Hi %s,\n\n%s\n\n— Agora", displayName, body))
}

func notifEmailContent(t string) (string, string) {
	switch t {
	case "friend_request":  return "New friend request on Agora", "Someone sent you a friend request."
	case "friend_accepted": return "Friend request accepted", "Your friend request was accepted."
	case "post_like":       return "Someone liked your post", "One of your posts received a like."
	case "post_comment":    return "New comment on your post", "Someone commented on your post."
	case "post_repost":     return "Your post was reposted", "Someone shared your post."
	}
	return "", ""
}

// ── Email helpers (called by auth) ────────────────────────────────────────────

func (s *Service) SendEmailVerification(_, email, displayName, token string) {
	if !s.email.enabled() { return }
	domain := s.email.instanceDomain()
	link := fmt.Sprintf("%s/verify-email?token=%s", domain, token)
	s.email.Send(email, "Verify your Agora email address",
		fmt.Sprintf("Hi %s,\n\nVerify your email:\n\n%s\n\nExpires in 24 hours.\n\n— Agora", displayName, link))
}

func (s *Service) SendPasswordReset(_, email, displayName, token string) {
	if !s.email.enabled() { return }
	domain := s.email.instanceDomain()
	link := fmt.Sprintf("%s/reset-password?token=%s", domain, token)
	s.email.Send(email, "Reset your Agora password",
		fmt.Sprintf("Hi %s,\n\nReset your password:\n\n%s\n\nExpires in 2 hours.\n\n— Agora", displayName, link))
}

func (s *Service) SendModerationAction(userID, action, reason string) {
	if !s.email.enabled() { return }
	var email, displayName string
	if err := s.db.QueryRow(`SELECT email, display_name FROM users WHERE id = $1`, userID).
		Scan(&email, &displayName); err != nil {
		return
	}
	s.email.Send(email, "Moderation action on your Agora account",
		fmt.Sprintf("Hi %s,\n\nAction: %s\nReason: %s\n\n— Agora", displayName, action, reason))
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
	if val != "true" {
		log.Printf("email: smtp_enabled=%q, skipping", val)
	}
	return val == "true"
}

func (e *EmailService) instanceDomain() string {
	var val string
	e.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_domain'`).Scan(&val)
	if val == "" { return e.cfg.InstanceDomain }
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
	if portStr == "" {
		portStr = "587"
	}

	domain := e.instanceDomain()
	instanceName := e.instanceName()

	// Friendly From with display name, e.g. "Agora <noreply@example.com>"
	fromHeader := fmt.Sprintf("%s <%s>", instanceName, from)

	// Unique Message-ID to prevent duplicate/spam classification
	msgID := fmt.Sprintf("<%s.%s@%s>", randomID(), randomID(), domain)

	// RFC 2822 date
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

func (e *EmailService) instanceName() string {
	var val string
	e.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_name'`).Scan(&val)
	if val == "" {
		return "Agora"
	}
	return val
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
