package dm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/store"
)

// ── Service ───────────────────────────────────────────────────────────────────

type Service struct {
	db  *store.DB
	hub *Hub
}

func New(db *store.DB) *Service {
	s := &Service{db: db, hub: newHub()}
	go s.hub.run()
	return s
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/conversations",                 s.ListConversations)
	r.Post("/conversations",                s.StartConversation)
	r.Get("/conversations/friend-search",   s.FriendSearch)
	r.Get("/conversations/{id}",            s.GetConversation)
	r.Get("/conversations/{id}/messages",   s.GetMessages)
	r.Post("/conversations/{id}/messages",  s.SendMessage)
	r.Patch("/messages/{id}",               s.EditMessage)
	r.Delete("/messages/{id}",              s.DeleteMessage)
	r.Post("/messages/{id}/react",          s.ReactMessage)
	r.Delete("/messages/{id}/react",        s.UnreactMessage)
	r.Post("/conversations/{id}/read",      s.MarkRead)
	r.Post("/conversations/{id}/accept",    s.AcceptRequest)
	r.Delete("/conversations/{id}",         s.LeaveConversation)
	r.Get("/ws",                            s.WebSocket)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── Types ─────────────────────────────────────────────────────────────────────

type Participant struct {
	UserID       string  `json:"user_id"`
	Username     string  `json:"username"`
	DisplayName  string  `json:"display_name"`
	AvatarURL    string  `json:"avatar_url"`
	LastReadAt   *string `json:"last_read_at,omitempty"`
	ReadReceipts bool    `json:"read_receipts"`
}

type Conversation struct {
	ID           string        `json:"id"`
	Participants []Participant `json:"participants"`
	LastMessage  *Message      `json:"last_message,omitempty"`
	UnreadCount  int           `json:"unread_count"`
	IsAccepted   bool          `json:"is_accepted"`
	UpdatedAt    string        `json:"updated_at"`
}

type Message struct {
	ID             string            `json:"id"`
	ConversationID string            `json:"conversation_id"`
	AuthorID       string            `json:"author_id"`
	AuthorUsername string            `json:"author_username"`
	AuthorName     string            `json:"author_display_name"`
	AuthorAvatar   string            `json:"author_avatar_url"`
	Content        string            `json:"content"`
	ImageURL       string            `json:"image_url"`
	Reactions      []MessageReaction `json:"reactions,omitempty"`
	EditedAt       *string           `json:"edited_at,omitempty"`
	DeletedAt      *string           `json:"deleted_at,omitempty"`
	CreatedAt      string            `json:"created_at"`
}

type MessageReaction struct {
	Reaction string `json:"reaction"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Service) isParticipant(convID, userID string) bool {
	var exists bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2)`,
		convID, userID).Scan(&exists)
	return exists
}

func (s *Service) isFriend(a, b string) bool {
	var exists bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM friendships WHERE ((requester_id=$1 AND addressee_id=$2) OR (requester_id=$2 AND addressee_id=$1)) AND status='accepted')`,
		a, b).Scan(&exists)
	return exists
}

func (s *Service) loadParticipants(convID, viewerID string) []Participant {
	rows, err := s.db.Query(`
		SELECT cp.user_id, u.username, u.display_name, u.avatar_url, cp.last_read_at, cp.read_receipts
		FROM conversation_participants cp
		JOIN users u ON u.id = cp.user_id
		WHERE cp.conversation_id = $1
	`, convID)
	if err != nil { return nil }
	defer rows.Close()
	var ps []Participant
	for rows.Next() {
		var p Participant
		rows.Scan(&p.UserID, &p.Username, &p.DisplayName, &p.AvatarURL, &p.LastReadAt, &p.ReadReceipts)
		if p.UserID != viewerID && !p.ReadReceipts {
			p.LastReadAt = nil
		}
		ps = append(ps, p)
	}
	return ps
}

func (s *Service) loadLastMessage(convID string) *Message {
	var m Message
	err := s.db.QueryRow(`
		SELECT m.id, m.conversation_id, m.author_id, u.username, u.display_name, u.avatar_url,
		       m.content, m.image_url, m.edited_at, m.deleted_at, m.created_at
		FROM messages m
		JOIN users u ON u.id = m.author_id
		WHERE m.conversation_id = $1 AND m.deleted_at IS NULL
		ORDER BY m.created_at DESC LIMIT 1
	`, convID).Scan(&m.ID, &m.ConversationID, &m.AuthorID, &m.AuthorUsername, &m.AuthorName,
		&m.AuthorAvatar, &m.Content, &m.ImageURL, &m.EditedAt, &m.DeletedAt, &m.CreatedAt)
	if err != nil { return nil }
	return &m
}

func (s *Service) unreadCount(convID, userID string) int {
	var count int
	s.db.QueryRow(`
		SELECT COUNT(*) FROM messages m
		JOIN conversation_participants cp ON cp.conversation_id = m.conversation_id AND cp.user_id = $2
		WHERE m.conversation_id = $1
		  AND m.author_id != $2
		  AND m.deleted_at IS NULL
		  AND (cp.last_read_at IS NULL OR m.created_at > cp.last_read_at)
	`, convID, userID).Scan(&count)
	return count
}

func (s *Service) loadMessage(msgID string) *Message {
	var m Message
	s.db.QueryRow(`
		SELECT m.id, m.conversation_id, m.author_id, u.username, u.display_name, u.avatar_url,
		       m.content, m.image_url, m.edited_at, m.deleted_at, m.created_at
		FROM messages m JOIN users u ON u.id = m.author_id
		WHERE m.id = $1
	`, msgID).Scan(&m.ID, &m.ConversationID, &m.AuthorID, &m.AuthorUsername, &m.AuthorName,
		&m.AuthorAvatar, &m.Content, &m.ImageURL, &m.EditedAt, &m.DeletedAt, &m.CreatedAt)
	return &m
}

func (s *Service) broadcastToConv(convID, excludeUserID string, event WSEvent) {
	rows, _ := s.db.Query(`SELECT user_id FROM conversation_participants WHERE conversation_id=$1 AND user_id!=$2`, convID, excludeUserID)
	if rows == nil { return }
	defer rows.Close()
	for rows.Next() {
		var uid string
		rows.Scan(&uid)
		s.hub.broadcast(uid, event)
	}
}

func (s *Service) enrichMessageReactions(msgs []Message) {
	if len(msgs) == 0 { return }
	ids  := make([]any, len(msgs))
	idx  := map[string]int{}
	phs  := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i]  = m.ID
		idx[m.ID] = i
		phs[i]  = fmt.Sprintf("$%d", i+1)
	}
	inClause := "("
	for i, p := range phs { if i > 0 { inClause += "," }; inClause += p }
	inClause += ")"

	rows, err := s.db.Query(`
		SELECT mr.message_id, mr.reaction, mr.user_id, u.username
		FROM message_reactions mr JOIN users u ON u.id = mr.user_id
		WHERE mr.message_id IN `+inClause, ids...)
	if err != nil { return }
	defer rows.Close()
	for rows.Next() {
		var msgID string
		var rx MessageReaction
		rows.Scan(&msgID, &rx.Reaction, &rx.UserID, &rx.Username)
		if i, ok := idx[msgID]; ok {
			msgs[i].Reactions = append(msgs[i].Reactions, rx)
		}
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *Service) FriendSearch(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	q := "%" + strings.ToLower(r.URL.Query().Get("q")) + "%"

	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url
		FROM users u
		JOIN friendships f ON (
			(f.requester_id = $1 AND f.addressee_id = u.id) OR
			(f.addressee_id = $1 AND f.requester_id = u.id)
		)
		WHERE f.status = 'accepted'
		  AND u.deletion_scheduled_at IS NULL
		  AND (LOWER(u.username) LIKE $2 OR LOWER(u.display_name) LIKE $2)
		ORDER BY u.display_name, u.username
		LIMIT 10
	`, userID, q)
	if err != nil { writeError(w, 500, "db error"); return }
	defer rows.Close()

	type Friend struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	var friends []Friend
	for rows.Next() {
		var f Friend
		rows.Scan(&f.ID, &f.Username, &f.DisplayName, &f.AvatarURL)
		friends = append(friends, f)
	}
	if friends == nil { friends = []Friend{} }
	writeJSON(w, 200, map[string]any{"friends": friends})
}

func (s *Service) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	rows, err := s.db.Query(`
		SELECT c.id, c.updated_at, cp.is_accepted
		FROM conversations c
		JOIN conversation_participants cp ON cp.conversation_id = c.id AND cp.user_id = $1
		ORDER BY c.updated_at DESC
	`, userID)
	if err != nil { writeError(w, 500, "db error"); return }
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		rows.Scan(&c.ID, &c.UpdatedAt, &c.IsAccepted)
		c.Participants = s.loadParticipants(c.ID, userID)
		c.LastMessage  = s.loadLastMessage(c.ID)
		c.UnreadCount  = s.unreadCount(c.ID, userID)
		convs = append(convs, c)
	}
	if convs == nil { convs = []Conversation{} }
	writeJSON(w, 200, map[string]any{"conversations": convs})
}

func (s *Service) StartConversation(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		RecipientUsername string `json:"username"`
		Message           string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RecipientUsername == "" {
		writeError(w, 400, "username required"); return
	}

	var recipientID, dmPrivacy string
	s.db.QueryRow(`SELECT id, COALESCE(dm_privacy, 'everyone') FROM users WHERE LOWER(username)=LOWER($1) AND deletion_scheduled_at IS NULL`,
		req.RecipientUsername).Scan(&recipientID, &dmPrivacy)
	if recipientID == ""     { writeError(w, 404, "user not found"); return }
	if recipientID == userID { writeError(w, 400, "cannot message yourself"); return }

	isFriend := s.isFriend(userID, recipientID)
	if dmPrivacy == "nobody"                    { writeError(w, 403, "this user is not accepting messages"); return }
	if dmPrivacy == "friends" && !isFriend      { writeError(w, 403, "this user only accepts messages from friends"); return }

	// Return existing conversation if one already exists
	var existingID string
	s.db.QueryRow(`
		SELECT c.id FROM conversations c
		JOIN conversation_participants p1 ON p1.conversation_id=c.id AND p1.user_id=$1
		JOIN conversation_participants p2 ON p2.conversation_id=c.id AND p2.user_id=$2
	`, userID, recipientID).Scan(&existingID)
	if existingID != "" {
		writeJSON(w, 200, map[string]string{"id": existingID})
		return
	}

	var convID string
	s.db.QueryRow(`INSERT INTO conversations DEFAULT VALUES RETURNING id`).Scan(&convID)
	s.db.Exec(`INSERT INTO conversation_participants (conversation_id, user_id, is_accepted) VALUES ($1,$2,true)`, convID, userID)
	s.db.Exec(`INSERT INTO conversation_participants (conversation_id, user_id, is_accepted) VALUES ($1,$2,$3)`, convID, recipientID, isFriend)

	if req.Message != "" {
		var msgID string
		s.db.QueryRow(`INSERT INTO messages (conversation_id, author_id, content) VALUES ($1,$2,$3) RETURNING id`,
			convID, userID, req.Message).Scan(&msgID)
		s.db.Exec(`UPDATE conversations SET updated_at=NOW() WHERE id=$1`, convID)
		m := s.loadMessage(msgID)
		s.hub.broadcast(recipientID, WSEvent{Type: "new_message", ConvID: convID, Data: m})
	}

	writeJSON(w, 201, map[string]string{"id": convID})
}

func (s *Service) GetConversation(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	convID := chi.URLParam(r, "id")
	if !s.isParticipant(convID, userID) { writeError(w, 404, "not found"); return }

	var c Conversation
	s.db.QueryRow(`SELECT c.id, c.updated_at, cp.is_accepted FROM conversations c JOIN conversation_participants cp ON cp.conversation_id=c.id AND cp.user_id=$2 WHERE c.id=$1`,
		convID, userID).Scan(&c.ID, &c.UpdatedAt, &c.IsAccepted)
	c.Participants = s.loadParticipants(c.ID, userID)
	c.UnreadCount  = s.unreadCount(c.ID, userID)
	writeJSON(w, 200, c)
}

func (s *Service) GetMessages(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	convID := chi.URLParam(r, "id")
	if !s.isParticipant(convID, userID) { writeError(w, 404, "not found"); return }

	before := r.URL.Query().Get("before")
	var (
		msgs []Message
		rows interface {
			Next() bool
			Scan(...any) error
			Close() error
		}
		err error
	)
	if before != "" {
		rows, err = s.db.Query(`
			SELECT m.id, m.conversation_id, m.author_id, u.username, u.display_name, u.avatar_url,
			       m.content, m.image_url, m.edited_at, m.deleted_at, m.created_at
			FROM messages m JOIN users u ON u.id=m.author_id
			WHERE m.conversation_id=$1 AND m.created_at<(SELECT created_at FROM messages WHERE id=$2)
			ORDER BY m.created_at DESC LIMIT 50
		`, convID, before)
	} else {
		rows, err = s.db.Query(`
			SELECT m.id, m.conversation_id, m.author_id, u.username, u.display_name, u.avatar_url,
			       m.content, m.image_url, m.edited_at, m.deleted_at, m.created_at
			FROM messages m JOIN users u ON u.id=m.author_id
			WHERE m.conversation_id=$1
			ORDER BY m.created_at DESC LIMIT 50
		`, convID)
	}
	if err != nil { writeError(w, 500, "db error"); return }
	defer rows.Close()
	for rows.Next() {
		var m Message
		rows.Scan(&m.ID, &m.ConversationID, &m.AuthorID, &m.AuthorUsername, &m.AuthorName,
			&m.AuthorAvatar, &m.Content, &m.ImageURL, &m.EditedAt, &m.DeletedAt, &m.CreatedAt)
		msgs = append(msgs, m)
	}
	if msgs == nil { msgs = []Message{} }
	// Reverse to oldest-first
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 { msgs[i], msgs[j] = msgs[j], msgs[i] }
	s.enrichMessageReactions(msgs)
	writeJSON(w, 200, map[string]any{"messages": msgs})
}

func (s *Service) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	convID := chi.URLParam(r, "id")
	if !s.isParticipant(convID, userID) { writeError(w, 404, "not found"); return }

	var accepted bool
	s.db.QueryRow(`SELECT is_accepted FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`, convID, userID).Scan(&accepted)
	if !accepted { writeError(w, 403, "conversation not yet accepted"); return }

	var req struct {
		Content  string `json:"content"`
		ImageURL string `json:"image_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { writeError(w, 400, "invalid json"); return }
	if req.Content == "" && req.ImageURL == "" { writeError(w, 400, "message cannot be empty"); return }

	var msgID string
	s.db.QueryRow(`INSERT INTO messages (conversation_id, author_id, content, image_url) VALUES ($1,$2,$3,$4) RETURNING id`,
		convID, userID, req.Content, req.ImageURL).Scan(&msgID)
	s.db.Exec(`UPDATE conversations SET updated_at=NOW() WHERE id=$1`, convID)

	m := s.loadMessage(msgID)
	s.broadcastToConv(convID, userID, WSEvent{Type: "new_message", ConvID: convID, Data: m})
	writeJSON(w, 201, m)
}

func (s *Service) EditMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	msgID  := chi.URLParam(r, "id")

	var authorID, convID string
	s.db.QueryRow(`SELECT author_id, conversation_id FROM messages WHERE id=$1 AND deleted_at IS NULL`, msgID).Scan(&authorID, &convID)
	if authorID == ""      { writeError(w, 404, "not found"); return }
	if authorID != userID  { writeError(w, 403, "forbidden"); return }

	var req struct { Content string `json:"content"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" { writeError(w, 400, "content required"); return }

	s.db.Exec(`UPDATE messages SET content=$1, edited_at=NOW() WHERE id=$2`, req.Content, msgID)
	m := s.loadMessage(msgID)
	s.broadcastToConv(convID, userID, WSEvent{Type: "message_edited", ConvID: convID, Data: m})
	writeJSON(w, 200, m)
}

func (s *Service) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	msgID  := chi.URLParam(r, "id")

	var authorID, convID string
	s.db.QueryRow(`SELECT author_id, conversation_id FROM messages WHERE id=$1 AND deleted_at IS NULL`, msgID).Scan(&authorID, &convID)
	if authorID == ""     { writeError(w, 404, "not found"); return }
	if authorID != userID { writeError(w, 403, "forbidden"); return }

	s.db.Exec(`UPDATE messages SET deleted_at=NOW(), content='', image_url='' WHERE id=$1`, msgID)
	s.broadcastToConv(convID, userID, WSEvent{Type: "message_deleted", ConvID: convID, Data: map[string]string{"id": msgID}})
	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

func (s *Service) ReactMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	msgID  := chi.URLParam(r, "id")

	var convID string
	s.db.QueryRow(`SELECT conversation_id FROM messages WHERE id=$1 AND deleted_at IS NULL`, msgID).Scan(&convID)
	if convID == ""                           { writeError(w, 404, "not found"); return }
	if !s.isParticipant(convID, userID)       { writeError(w, 403, "forbidden"); return }

	var req struct { Reaction string `json:"reaction"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Reaction == "" { writeError(w, 400, "reaction required"); return }

	s.db.Exec(`INSERT INTO message_reactions (message_id, user_id, reaction) VALUES ($1,$2,$3) ON CONFLICT (message_id, user_id) DO UPDATE SET reaction=$3`,
		msgID, userID, req.Reaction)
	s.broadcastToConv(convID, userID, WSEvent{Type: "message_reaction", ConvID: convID,
		Data: map[string]string{"message_id": msgID, "user_id": userID, "reaction": req.Reaction}})
	writeJSON(w, 200, map[string]string{"message": "reacted"})
}

func (s *Service) UnreactMessage(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	msgID  := chi.URLParam(r, "id")

	var convID string
	s.db.QueryRow(`SELECT conversation_id FROM messages WHERE id=$1`, msgID).Scan(&convID)
	if convID == "" { writeError(w, 404, "not found"); return }

	s.db.Exec(`DELETE FROM message_reactions WHERE message_id=$1 AND user_id=$2`, msgID, userID)
	s.broadcastToConv(convID, userID, WSEvent{Type: "message_reaction", ConvID: convID,
		Data: map[string]string{"message_id": msgID, "user_id": userID, "reaction": ""}})
	writeJSON(w, 200, map[string]string{"message": "unreacted"})
}

func (s *Service) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	convID := chi.URLParam(r, "id")
	if !s.isParticipant(convID, userID) { writeError(w, 404, "not found"); return }

	s.db.Exec(`UPDATE conversation_participants SET last_read_at=NOW() WHERE conversation_id=$1 AND user_id=$2`, convID, userID)
	s.broadcastToConv(convID, userID, WSEvent{Type: "read_receipt", ConvID: convID,
		Data: map[string]string{"user_id": userID, "at": time.Now().UTC().Format(time.RFC3339)}})
	writeJSON(w, 200, map[string]string{"message": "marked read"})
}

func (s *Service) AcceptRequest(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	convID := chi.URLParam(r, "id")

	var exists bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2 AND is_accepted=false)`,
		convID, userID).Scan(&exists)
	if !exists { writeError(w, 404, "not found"); return }

	s.db.Exec(`UPDATE conversation_participants SET is_accepted=true WHERE conversation_id=$1 AND user_id=$2`, convID, userID)
	writeJSON(w, 200, map[string]string{"message": "accepted"})
}

func (s *Service) LeaveConversation(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	convID := chi.URLParam(r, "id")
	if !s.isParticipant(convID, userID) { writeError(w, 404, "not found"); return }

	s.db.Exec(`DELETE FROM conversation_participants WHERE conversation_id=$1 AND user_id=$2`, convID, userID)
	writeJSON(w, 200, map[string]string{"message": "left"})
}

// ── WebSocket Hub ─────────────────────────────────────────────────────────────

type WSEvent struct {
	Type   string `json:"type"`
	ConvID string `json:"conversation_id"`
	Data   any    `json:"data"`
}

type Client struct {
	userID string
	conn   *websocket.Conn
	send   chan WSEvent
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string][]*Client // userID -> clients
	reg     chan *Client
	unreg   chan *Client
}

func newHub() *Hub {
	return &Hub{
		clients: make(map[string][]*Client),
		reg:     make(chan *Client, 16),
		unreg:   make(chan *Client, 16),
	}
}

func (h *Hub) run() {
	for {
		select {
		case c := <-h.reg:
			h.mu.Lock()
			h.clients[c.userID] = append(h.clients[c.userID], c)
			h.mu.Unlock()
		case c := <-h.unreg:
			h.mu.Lock()
			list := h.clients[c.userID]
			for i, cl := range list {
				if cl == c {
					h.clients[c.userID] = append(list[:i], list[i+1:]...)
					break
				}
			}
			close(c.send)
			h.mu.Unlock()
		}
	}
}

func (h *Hub) broadcast(userID string, event WSEvent) {
	h.mu.RLock()
	clients := h.clients[userID]
	h.mu.RUnlock()
	for _, c := range clients {
		select {
		case c.send <- event:
		default:
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (s *Service) WebSocket(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" { writeError(w, 401, "unauthorized"); return }

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }

	c := &Client{userID: userID, conn: conn, send: make(chan WSEvent, 32)}
	s.hub.reg <- c

	// Writer goroutine
	go func() {
		defer conn.Close()
		for event := range c.send {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(event); err != nil { break }
		}
	}()

	// Reader goroutine — handles ping/pong and mark_read from client
	defer func() { s.hub.unreg <- c }()
	conn.SetReadLimit(512)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Ping ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil { return }
		}
	}()

	for {
		var msg struct {
			Type   string `json:"type"`
			ConvID string `json:"conversation_id"`
		}
		if err := conn.ReadJSON(&msg); err != nil { break }
		if msg.Type == "mark_read" && msg.ConvID != "" {
			s.db.Exec(`UPDATE conversation_participants SET last_read_at=NOW() WHERE conversation_id=$1 AND user_id=$2`, msg.ConvID, userID)
			s.broadcastToConv(msg.ConvID, userID, WSEvent{Type: "read_receipt", ConvID: msg.ConvID,
				Data: map[string]string{"user_id": userID, "at": time.Now().UTC().Format(time.RFC3339)}})
		}
	}
}
