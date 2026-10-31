package notifications

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/lib/pq"
)

// ── Push delivery (AGORA-420) ─────────────────────────────────────────────────
//
// Pushes go through Expo's push service, which hands them to APNs and FCM.
// What changed here, and why:
//   - Every device the user registered gets the push (push_tokens), not just
//     the last one to register.
//   - Each push carries the unread count as its badge. The app set the badge
//     itself while it was open, so a closed app's icon never had one.
//   - Marking notifications read sends a badge-only push, so reading them on
//     the web or another device clears the number on the phone.
//   - Expo's per-message tickets are read. A DeviceNotRegistered token
//     (uninstalled app, revoked permission) is dropped rather than retried on
//     every notification forever, and other failures are logged instead of
//     vanishing.
//   - One shared HTTP client instead of a new one per notification.

const expoPushURL = "https://exp.host/--/api/v2/push/send"

// expoBatchLimit is Expo's maximum number of messages per request.
const expoBatchLimit = 100

var pushHTTPClient = &http.Client{Timeout: 10 * time.Second}

type expoMessage struct {
	To       string            `json:"to"`
	Title    string            `json:"title,omitempty"`
	Body     string            `json:"body,omitempty"`
	Sound    string            `json:"sound,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
	Badge    *int              `json:"badge,omitempty"`
	Priority string            `json:"priority,omitempty"`
}

type expoTicket struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

func (s *Service) pushTokens(userID string) []string {
	var tokens []string
	rows, err := s.db.Query(`SELECT token FROM push_tokens WHERE user_id = $1`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

func (s *Service) unreadCount(userID string) int {
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = false`, userID).Scan(&count)
	return count
}

// badgeCount is the number on the app icon: unread notifications plus unread
// direct messages (AGORA-425), the same two counts the app shows on its
// Notifications and Messages tabs. Unread messages are counted the way the DM
// service counts them per conversation.
func (s *Service) badgeCount(userID string) int {
	var messages int
	s.db.QueryRow(`
		SELECT COUNT(*) FROM messages m
		JOIN conversation_participants cp ON cp.conversation_id = m.conversation_id AND cp.user_id = $1
		WHERE m.author_id != $1
		  AND m.deleted_at IS NULL
		  AND (cp.last_read_at IS NULL OR m.created_at > cp.last_read_at)
	`, userID).Scan(&messages)
	return s.unreadCount(userID) + messages
}

// PushDirectMessage tells a recipient's devices about a new direct message
// (AGORA-425). Messages used to reach only an open app, over the DM socket.
//
// The push names the sender but not what they wrote: a message is the most
// private thing on the instance, and a push travels through Expo and Apple or
// Google and sits on a lock screen. A first message from someone the recipient
// hasn't accepted yet says it's a request, matching the Requests split the
// Messages tab makes. Nothing is pushed between users who have blocked each
// other.
func (s *Service) PushDirectMessage(recipientID, senderID, convID string, isRequest bool) {
	var blocked bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`,
		recipientID, senderID).Scan(&blocked)
	if blocked {
		return
	}
	tokens := s.pushTokens(recipientID)
	if len(tokens) == 0 {
		return
	}

	senderName, senderUsername := "Someone", ""
	var display, username string
	s.db.QueryRow(`SELECT COALESCE(display_name,''), username FROM users WHERE id = $1`, senderID).Scan(&display, &username)
	if display != "" { senderName = display } else if username != "" { senderName = username }
	senderUsername = username

	title, body := "New message", senderName+" sent you a message"
	if isRequest {
		title, body = "New message request", senderName+" wants to send you a message"
	}
	data := map[string]string{
		"type":            "new_message",
		"conversation_id": convID,
		"actor_username":  senderUsername,
	}
	badge := s.badgeCount(recipientID)
	messages := make([]expoMessage, len(tokens))
	for i, t := range tokens {
		messages[i] = expoMessage{
			To: t, Title: title, Body: body, Sound: "default", Data: data,
			Badge: &badge, Priority: "high",
		}
	}
	s.sendPush(messages)
}

// SyncBadge is syncBadge for the DM service, which calls it when a
// conversation is read so the icon's count comes down on the user's other
// devices too.
func (s *Service) SyncBadge(userID string) { s.syncBadge(userID) }

// rootPostID resolves a comment (or a reply to one) to the post it's on, the
// way List does for the notifications page. Anything else comes back as is.
func (s *Service) rootPostID(postID string) string {
	if postID == "" {
		return ""
	}
	id := postID
	for i := 0; i < 2; i++ {
		var parentID *string
		if s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1`, id).Scan(&parentID) != nil || parentID == nil {
			break
		}
		id = *parentID
	}
	return id
}

// syncBadge sets the app icon's badge on every device to the current unread
// count, with no banner or sound. iOS applies a badge-only push silently; the
// type lets the app recognise it in the foreground too.
func (s *Service) syncBadge(userID string) {
	tokens := s.pushTokens(userID)
	if len(tokens) == 0 {
		return
	}
	badge := s.badgeCount(userID)
	messages := make([]expoMessage, len(tokens))
	for i, t := range tokens {
		messages[i] = expoMessage{To: t, Badge: &badge, Data: map[string]string{"type": "badge_sync"}}
	}
	s.sendPush(messages)
}

func (s *Service) sendPush(messages []expoMessage) {
	for start := 0; start < len(messages); start += expoBatchLimit {
		end := start + expoBatchLimit
		if end > len(messages) {
			end = len(messages)
		}
		s.sendPushBatch(messages[start:end])
	}
}

func (s *Service) sendPushBatch(messages []expoMessage) {
	payload, err := json.Marshal(messages)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, expoPushURL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := pushHTTPClient.Do(req)
	if err != nil {
		log.Printf("push: send failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var out struct {
		Data   []expoTicket `json:"data"`
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("push: Expo returned %d (%d request errors)", resp.StatusCode, len(out.Errors))
		for _, e := range out.Errors {
			log.Printf("push: %s: %s", e.Code, e.Message)
		}
		return
	}
	s.handleTickets(messages, out.Data)
}

// handleTickets reads Expo's per-message results, which come back in the same
// order the messages were sent.
func (s *Service) handleTickets(messages []expoMessage, tickets []expoTicket) {
	var dead []string
	for i, t := range tickets {
		if i >= len(messages) || t.Status != "error" {
			continue
		}
		if t.Details.Error == "DeviceNotRegistered" {
			dead = append(dead, messages[i].To)
			continue
		}
		log.Printf("push: Expo rejected a message (%s): %s", t.Details.Error, t.Message)
	}
	if len(dead) == 0 {
		return
	}
	s.db.Exec(`DELETE FROM push_tokens WHERE token = ANY($1)`, pq.Array(dead))
	s.db.Exec(`UPDATE users SET expo_push_token = '' WHERE expo_push_token = ANY($1)`, pq.Array(dead))
	log.Printf("push: dropped %d unregistered device token(s)", len(dead))
}
