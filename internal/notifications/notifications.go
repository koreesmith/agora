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
		// For comment_reaction and comment_reply, post_id may point to a comment
		// rather than the root post. Resolve upward so the client navigates correctly.
		if n.PostID != nil && (n.Type == "comment_reaction" || n.Type == "comment_reply" || n.Type == "comment_like") {
			var parentID *string
			s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1`, *n.PostID).Scan(&parentID)
			if parentID != nil {
				// It's a comment — walk up one more level in case it's a depth-2 reply
				rootID := *parentID
				var grandParentID *string
				s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1`, rootID).Scan(&grandParentID)
				if grandParentID != nil {
					rootID = *grandParentID
				}
				notifs[i].PostID = &rootID
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

// ── User Invites (AGORA-75) ───────────────────────────────────────────────────

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
	go s.maybeEmailNotif(userID, actorID, notifType, postID)
	go s.maybePushNotif(userID, actorID, notifType, postID, data)
}

func (s *Service) maybePushNotif(userID, actorID, notifType, postID, extraData string) {
	var pushToken string
	s.db.QueryRow(`SELECT COALESCE(expo_push_token,'') FROM users WHERE id = $1`, userID).Scan(&pushToken)
	if pushToken == "" { return }

	actorName := "Someone"
	actorUsername := ""
	if actorID != "" {
		var display, username string
		s.db.QueryRow(`SELECT COALESCE(display_name,''), username FROM users WHERE id = $1`, actorID).Scan(&display, &username)
		if display != "" { actorName = display } else if username != "" { actorName = username }
		actorUsername = username
	}

	title, body := pushNotifContent(notifType, actorName)
	if title == "" { return }

	data := map[string]string{
		"type":           notifType,
		"post_id":        postID,
		"actor_username": actorUsername,
		"data":           extraData,
	}

	payload := map[string]any{
		"to":    pushToken,
		"title": title,
		"body":  body,
		"sound": "default",
		"data":  data,
	}
	jsonBytes, _ := json.Marshal([]any{payload})
	req, err := http.NewRequest("POST", "https://exp.host/--/api/v2/push/send", strings.NewReader(string(jsonBytes)))
	if err != nil { return }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil { log.Printf("push notif error: %v", err); return }
	defer resp.Body.Close()
}

func pushNotifContent(t, actorName string) (title, body string) {
	switch t {
	case "friend_request":
		return "New friend request", actorName + " sent you a friend request"
	case "friend_accepted":
		return "Friend request accepted", actorName + " accepted your friend request"
	case "post_like":
		return "New like", actorName + " liked your post"
	case "post_reaction":
		return "New reaction", actorName + " reacted to your post"
	case "post_comment":
		return "New comment", actorName + " commented on your post"
	case "comment_reply":
		return "New reply", actorName + " replied to your comment"
	case "post_mention":
		return "You were mentioned", actorName + " mentioned you in a post"
	case "post_repost":
		return "New repost", actorName + " reposted your post"
	case "wall_post":
		return "New wall post", actorName + " posted on your wall"
	case "wall_post_pending":
		return "Wall post request", actorName + " wants to post on your wall"
	case "user_post":
		return "New post", actorName + " just posted something new"
	case "group_join_request":
		return "Join request", actorName + " wants to join your group"
	case "group_join_approved":
		return "Request approved", "Your group join request was approved"
	case "new_report":
		return "⚠️ New Report", "A new report has been submitted — tap to review"
	}
	return "", ""
}

func (s *Service) maybeEmailNotif(userID, actorID, notifType, postID string) {
	if !s.email.enabled() { return }

	var toEmail, displayName, unsubToken string
	var emailNotifsEnabled bool
	if err := s.db.QueryRow(`
		SELECT email, display_name, email_notifications_enabled, COALESCE(unsubscribe_token,'')
		FROM users WHERE id = $1 AND email_verified = true
	`, userID).Scan(&toEmail, &displayName, &emailNotifsEnabled, &unsubToken); err != nil {
		return
	}
	if !emailNotifsEnabled { return }

	actorName := "Someone"
	actorUsername := ""
	if actorID != "" {
		var aDisplay, aUsername string
		s.db.QueryRow(`SELECT display_name, username FROM users WHERE id = $1`, actorID).
			Scan(&aDisplay, &aUsername)
		if aDisplay != "" {
			actorName = aDisplay
		} else if aUsername != "" {
			actorName = aUsername
		}
		actorUsername = aUsername
	}

	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()
	baseURL := s.email.instanceBaseURL()

	subject, body := notifEmailContent(notifType, actorName, actorUsername, postID, instanceName, baseURL)
	if subject == "" { return }

	s.email.SendHTML(toEmail, subject, buildBody(displayName, instanceName, domain, baseURL, body), "", unsubToken)
}

func notifEmailContent(t, actorName, actorUsername, postID, instanceName, baseURL string) (subject, body string) {
	postURL := baseURL
	if postID != "" {
		postURL = fmt.Sprintf("%s/post/%s", baseURL, postID)
	}
	profileURL := baseURL
	if actorUsername != "" {
		profileURL = fmt.Sprintf("%s/profile/%s", baseURL, actorUsername)
	}

	switch t {
	case "friend_request":
		return fmt.Sprintf("%s sent you a friend request on %s", actorName, instanceName),
			fmt.Sprintf("%s sent you a friend request on %s.\n\nAccept or decline: %s/friends", actorName, instanceName, baseURL)
	case "friend_accepted":
		return fmt.Sprintf("%s accepted your friend request", actorName),
			fmt.Sprintf("%s accepted your friend request on %s.\n\nView their profile: %s", actorName, instanceName, profileURL)
	case "post_like":
		return fmt.Sprintf("%s liked your post on %s", actorName, instanceName),
			fmt.Sprintf("%s liked one of your posts on %s.\n\nSee the post: %s", actorName, instanceName, postURL)
	case "post_reaction", "comment_reaction":
		return fmt.Sprintf("%s reacted to your post on %s", actorName, instanceName),
			fmt.Sprintf("%s reacted to one of your posts on %s.\n\nSee the post: %s", actorName, instanceName, postURL)
	case "post_comment":
		return fmt.Sprintf("%s commented on your post", actorName),
			fmt.Sprintf("%s left a comment on your post on %s.\n\nSee the discussion: %s", actorName, instanceName, postURL)
	case "comment_reply":
		return fmt.Sprintf("%s replied to your comment", actorName),
			fmt.Sprintf("%s replied to your comment on %s.\n\nSee the reply: %s", actorName, instanceName, postURL)
	case "post_repost":
		return fmt.Sprintf("%s shared your post on %s", actorName, instanceName),
			fmt.Sprintf("%s shared one of your posts on %s.\n\nSee the post: %s", actorName, instanceName, postURL)
	case "post_mention":
		return fmt.Sprintf("%s mentioned you in a post", actorName),
			fmt.Sprintf("%s mentioned you in a post on %s.\n\nSee the post: %s", actorName, instanceName, postURL)
	case "user_post":
		return fmt.Sprintf("%s just posted on %s", actorName, instanceName),
			fmt.Sprintf("%s just posted something new on %s.\n\nSee the post: %s", actorName, instanceName, postURL)
	case "wall_post":
		return fmt.Sprintf("%s posted on your wall", actorName),
			fmt.Sprintf("%s wrote something on your wall on %s.\n\nSee it: %s", actorName, instanceName, postURL)
	case "wall_post_pending":
		return fmt.Sprintf("%s wants to post on your wall", actorName),
			fmt.Sprintf("%s wants to post on your wall on %s but it needs your approval.\n\nReview it: %s", actorName, instanceName, postURL)
	case "wall_post_approved":
		return "Your wall post was approved",
			fmt.Sprintf("Your post on someone's wall on %s was approved.\n\nSee it: %s", instanceName, postURL)
	case "new_report":
		return fmt.Sprintf("⚠️ New report on %s", instanceName),
			fmt.Sprintf("A new report has been submitted on %s and needs your review.\n\nReview it: %s/admin", instanceName, baseURL)
	}
	return "", ""
}

func buildBody(displayName, instanceName, _, baseURL, content string) string {
	return fmt.Sprintf(`Hi %s,

%s

---
You're receiving this because you have email notifications enabled on %s.
To unsubscribe, visit: %s/settings

The %s team
`, displayName, content, instanceName, baseURL, instanceName)
}

// ── One-click unsubscribe ─────────────────────────────────────────────────────

// OneClickUnsubscribe handles POST /api/notifications/unsubscribe
// Called by email clients that support RFC 8058 one-click unsubscribe.
func (s *Service) OneClickUnsubscribe(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		if err := r.ParseForm(); err == nil {
			token = r.FormValue("token")
		}
	}
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	s.db.Exec(`UPDATE users SET email_notifications_enabled = false WHERE unsubscribe_token = $1`, token)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Unsubscribed successfully."))
}

// UnsubscribePage handles GET /api/notifications/unsubscribe
// Shown when a user clicks the unsubscribe link in their email client.
func (s *Service) UnsubscribePage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	baseURL := s.email.instanceBaseURL()
	instanceName := s.email.instanceName()

	if token == "" {
		http.Redirect(w, r, baseURL+"/settings", http.StatusSeeOther)
		return
	}

	var userID, displayName string
	s.db.QueryRow(`SELECT id, display_name FROM users WHERE unsubscribe_token = $1`, token).Scan(&userID, &displayName)
	if userID == "" {
		http.Redirect(w, r, baseURL+"/settings", http.StatusSeeOther)
		return
	}

	// Unsubscribe immediately on GET click too
	s.db.Exec(`UPDATE users SET email_notifications_enabled = false WHERE id = $1`, userID)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Unsubscribed — %s</title>
<style>body{margin:0;padding:40px 20px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#f4f4f5;text-align:center}
.box{max-width:480px;margin:0 auto;background:#fff;border-radius:12px;padding:40px;border:1px solid #e4e4e7}
h1{color:#3f3f46;font-size:22px;margin:0 0 12px}p{color:#71717a;font-size:15px;line-height:1.6;margin:0 0 20px}
a{color:#7c3aed;text-decoration:none;font-weight:600}</style></head>
<body><div class="box">
<h1>You've been unsubscribed</h1>
<p>Hi %s, you'll no longer receive email notifications from %s.</p>
<p>You can re-enable notifications at any time in your <a href="%s/settings">account settings</a>.</p>
</div></body></html>`, instanceName, displayName, instanceName, baseURL)
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

Welcome to %s! You're almost ready - just verify your email address to activate your account.

Verify your email here:
%s

This link expires in 24 hours.

If you didn't create an account on %s (%s), you can safely ignore this email.
Your email address will not be used for anything without your confirmation.

The %s team
%s
`, displayName, instanceName, link, instanceName, domain, instanceName, domain)

	html := buildVerificationHTML(instanceName, domain, baseURL, displayName, link)
	s.email.SendHTML(email, subject, plain, html, "") // no unsubscribe — transactional
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

This link expires in 2 hours. If you didn't request a password reset, you can safely ignore this email - your password has not been changed.

The %s team
%s
`, displayName, instanceName, domain, link, instanceName, domain)

	html := buildPasswordResetHTML(instanceName, domain, baseURL, displayName, link)
	s.email.SendHTML(email, subject, plain, html, "") // no unsubscribe — transactional
}

func (s *Service) SendUserInvite(toEmail, inviterName, inviterUsername string) {
	if !s.email.enabled() { return }
	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()
	baseURL := s.email.instanceBaseURL()
	registerURL := fmt.Sprintf("%s/register", baseURL)

	subject := fmt.Sprintf("%s invited you to join %s", inviterName, instanceName)

	plain := fmt.Sprintf(`Hi there,

%s (@%s) has invited you to join %s — a private, federated social network.

Sign up here:
%s

%s is a place to connect with the people you actually know, without ads, tracking, or algorithmic manipulation.

If you didn't expect this invitation, you can safely ignore this email.

The %s team
%s
`, inviterName, inviterUsername, instanceName, registerURL, instanceName, instanceName, domain)

	html := fmt.Sprintf(`<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:560px;margin:40px auto;padding:0 20px;color:#243b53">
<div style="text-align:center;margin-bottom:32px">
  <div style="width:56px;height:56px;background:#486581;border-radius:14px;display:inline-flex;align-items:center;justify-content:center">
    <svg width="32" height="32" viewBox="0 0 96 96" fill="none">
      <path d="M48 12L18 78" stroke="white" stroke-width="8" stroke-linecap="round"/>
      <path d="M48 12L78 78" stroke="white" stroke-width="8" stroke-linecap="round"/>
      <line x1="31" y1="52" x2="65" y2="52" stroke="white" stroke-width="7" stroke-linecap="round"/>
      <circle cx="24" cy="52" r="5" fill="#9fb3c8"/>
      <circle cx="72" cy="52" r="5" fill="#9fb3c8"/>
    </svg>
  </div>
</div>
<h1 style="font-size:24px;font-weight:700;color:#102a43;text-align:center;margin-bottom:8px">You're invited to join %s!</h1>
<p style="font-size:16px;color:#627d98;text-align:center;margin-bottom:32px">Your friend wants to connect with you.</p>
<div style="background:#f0f4f8;border-radius:12px;padding:24px;margin-bottom:24px">
  <p style="margin:0 0 8px;color:#334e68;font-size:15px">
    <strong>%s</strong> <span style="color:#829ab1">(@%s)</span> has invited you to join <strong>%s</strong>.
  </p>
  <p style="color:#486581;font-size:14px;margin:12px 0 0">
    %s is a private, federated social network — no ads, no tracking, no algorithmic manipulation. Just people you actually know.
  </p>
</div>
<div style="text-align:center;margin-bottom:32px">
  <a href="%s" style="display:inline-block;background:#486581;color:white;font-weight:700;font-size:16px;padding:14px 32px;border-radius:12px;text-decoration:none">Create your account →</a>
</div>
<p style="color:#829ab1;font-size:13px;text-align:center">If the button doesn't work, copy and paste this link:<br><a href="%s" style="color:#486581">%s</a></p>
<hr style="border:none;border-top:1px solid #e8edf2;margin:32px 0">
<p style="color:#b0bec5;font-size:11px;text-align:center">
  You received this because %s (@%s) entered your email address on %s (%s).<br>
  If you didn't expect this, you can safely ignore it — no account will be created without your action.<br>
  <a href="%s/privacy" style="color:#9fb3c8">Privacy Policy</a>
</p>
</body></html>`,
		instanceName,
		inviterName, inviterUsername, instanceName,
		instanceName,
		registerURL,
		registerURL, registerURL,
		inviterName, inviterUsername, instanceName, domain,
		baseURL,
	)

	// No unsubscribe token — this goes to non-users who have no account
	s.email.SendHTML(toEmail, subject, plain, html, "")
}

func (s *Service) SendWaitlistConfirmation(_, email, displayName string) {
	if !s.email.enabled() { return }
	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()

	subject := fmt.Sprintf("You're on the %s waitlist!", instanceName)
	plain := fmt.Sprintf(`Hi %s,

Thanks for signing up for %s! You're on the waitlist.

We're reviewing new accounts and will send you an invite link once you're approved. We'll try to get to you as quickly as possible.

In the meantime, make sure to verify your email address using the separate verification email we sent you.

The %s team
%s
`, displayName, instanceName, instanceName, domain)

	html := fmt.Sprintf(`<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:560px;margin:40px auto;padding:0 20px;color:#243b53">
<div style="text-align:center;margin-bottom:32px">
  <div style="width:56px;height:56px;background:#486581;border-radius:14px;display:inline-flex;align-items:center;justify-content:center">
    <svg width="32" height="32" viewBox="0 0 96 96" fill="none"><path d="M48 12L18 78" stroke="white" stroke-width="8" stroke-linecap="round"/><path d="M48 12L78 78" stroke="white" stroke-width="8" stroke-linecap="round"/><line x1="31" y1="52" x2="65" y2="52" stroke="white" stroke-width="7" stroke-linecap="round"/><circle cx="24" cy="52" r="5" fill="#9fb3c8"/><circle cx="72" cy="52" r="5" fill="#9fb3c8"/></svg>
  </div>
</div>
<h1 style="font-size:24px;font-weight:700;color:#102a43;text-align:center;margin-bottom:8px">You're on the waitlist!</h1>
<p style="font-size:16px;color:#627d98;text-align:center;margin-bottom:32px">We'll review your account and send you an invite soon.</p>
<div style="background:#f0f4f8;border-radius:12px;padding:24px;margin-bottom:24px">
  <p style="margin:0;color:#334e68;font-size:15px">Hi <strong>%s</strong>,</p>
  <p style="color:#486581;font-size:14px;margin:12px 0 0">Thanks for signing up for <strong>%s</strong>. Your account is on the waitlist — we'll notify you with an invite link as soon as you're approved.</p>
</div>
<p style="color:#829ab1;font-size:13px;text-align:center">Please also verify your email address using the separate verification email we sent you.</p>
<p style="color:#829ab1;font-size:12px;text-align:center;margin-top:32px">%s &middot; %s</p>
</body></html>`, displayName, instanceName, instanceName, domain)

	s.email.SendHTML(email, subject, plain, html, "")
}

func (s *Service) SendWaitlistApproved(email, displayName, acceptURL string) {
	if !s.email.enabled() { return }
	instanceName := s.email.instanceName()
	domain := s.email.instanceDomain()

	subject := fmt.Sprintf("You're approved — welcome to %s!", instanceName)
	plain := fmt.Sprintf(`Hi %s,

Great news! Your account on %s has been approved.

Click the link below to complete your signup and log in:
%s

This link will log you in automatically. If you have any trouble, you can also visit %s and sign in with the username and password you chose when you signed up.

Welcome aboard!

The %s team
%s
`, displayName, instanceName, acceptURL, domain, instanceName, domain)

	html := fmt.Sprintf(`<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:560px;margin:40px auto;padding:0 20px;color:#243b53">
<div style="text-align:center;margin-bottom:32px">
  <div style="width:56px;height:56px;background:#486581;border-radius:14px;display:inline-flex;align-items:center;justify-content:center">
    <svg width="32" height="32" viewBox="0 0 96 96" fill="none"><path d="M48 12L18 78" stroke="white" stroke-width="8" stroke-linecap="round"/><path d="M48 12L78 78" stroke="white" stroke-width="8" stroke-linecap="round"/><line x1="31" y1="52" x2="65" y2="52" stroke="white" stroke-width="7" stroke-linecap="round"/><circle cx="24" cy="52" r="5" fill="#9fb3c8"/><circle cx="72" cy="52" r="5" fill="#9fb3c8"/></svg>
  </div>
</div>
<h1 style="font-size:24px;font-weight:700;color:#102a43;text-align:center;margin-bottom:8px">You're approved! 🎉</h1>
<p style="font-size:16px;color:#627d98;text-align:center;margin-bottom:32px">Welcome to %s, %s.</p>
<div style="text-align:center;margin-bottom:32px">
  <a href="%s" style="display:inline-block;background:#486581;color:white;font-weight:700;font-size:16px;padding:14px 32px;border-radius:12px;text-decoration:none">Complete signup &amp; sign in →</a>
</div>
<p style="color:#829ab1;font-size:13px;text-align:center">If the button doesn't work, copy and paste this link:<br><a href="%s" style="color:#486581">%s</a></p>
<p style="color:#829ab1;font-size:12px;text-align:center;margin-top:32px">%s &middot; %s</p>
</body></html>`, instanceName, displayName, acceptURL, acceptURL, acceptURL, instanceName, domain)

	s.email.SendHTML(email, subject, plain, html, "")
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

The %s team
%s
`, displayName, instanceName, action, reason, domain, instanceName, domain)
	s.email.Send(email, fmt.Sprintf("Account notice from %s", instanceName), body, "")
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
func (e *EmailService) Send(to, subject, plainBody, unsubToken string) error {
	return e.SendHTML(to, subject, plainBody, "", unsubToken)
}

// SendHTML sends a multipart/alternative email with plain-text and optional HTML parts.
func (e *EmailService) SendHTML(to, subject, plainBody, htmlBody, unsubToken string) error {
	host, portStr, user, pass, from := e.smtpConfig()
	log.Printf("email: sending to=%s subject=%q via %s", to, subject, host)
	if host == "" {
		log.Println("email: smtp_host not configured, skipping")
		return fmt.Errorf("smtp not configured")
	}
	if portStr == "" { portStr = "587" }

	domain := e.instanceDomain()
	instanceName := e.instanceName()
	baseURL := e.instanceBaseURL()
	fromHeader := fmt.Sprintf("%s <%s>", instanceName, from)
	msgID := fmt.Sprintf("<%s.%s@%s>", randomID(), randomID(), domain)
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 +0000")

	encodedSubject := fmt.Sprintf("=?UTF-8?Q?%s?=", qEncode(subject))

	// Build unsubscribe URLs — one-click requires both a POST URL and a mailto fallback
	unsubURL := baseURL + "/api/notifications/unsubscribe?token=" + unsubToken
	unsubMailto := "mailto:unsubscribe@" + domain + "?subject=unsubscribe"
	var unsubHeaders []string
	if unsubToken != "" {
		unsubHeaders = []string{
			"List-Unsubscribe: <" + unsubURL + ">, <" + unsubMailto + ">",
			"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
		}
	}

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
			"X-Mailer: Agora/" + domain,
		}
		headers = append(headers, unsubHeaders...)
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
		}
		headers = append(headers, unsubHeaders...)
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
