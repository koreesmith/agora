package notifications

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/agora-social/agora/pkg/middleware"
	"github.com/agora-social/agora/pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	db       *sqlx.DB
	redis    *redis.Client
	emailSvc *EmailService
}

func NewService(db *sqlx.DB, redis *redis.Client, emailSvc *EmailService) *Service {
	return &Service{db: db, redis: redis, emailSvc: emailSvc}
}

type Notification struct {
	ID         string    `db:"id" json:"id"`
	UserID     string    `db:"user_id" json:"user_id"`
	ActorID    *string   `db:"actor_id" json:"actor_id,omitempty"`
	Type       string    `db:"type" json:"type"`
	EntityType string    `db:"entity_type" json:"entity_type"`
	EntityID   *string   `db:"entity_id" json:"entity_id,omitempty"`
	Content    string    `db:"content" json:"content"`
	Read       bool      `db:"read" json:"read"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	// Joined actor info
	ActorUsername    *string `db:"actor_username" json:"actor_username,omitempty"`
	ActorDisplayName *string `db:"actor_display_name" json:"actor_display_name,omitempty"`
	ActorAvatarURL   *string `db:"actor_avatar_url" json:"actor_avatar_url,omitempty"`
}

func RegisterRoutes(r chi.Router, svc *Service) {
	r.Get("/notifications", svc.List)
	r.Post("/notifications/read-all", svc.MarkAllRead)
	r.Post("/notifications/{notifID}/read", svc.MarkRead)
	r.Get("/notifications/unread-count", svc.UnreadCount)
}

func (s *Service) Create(userID, actorID, notifType, entityType, entityID, content string) {
	_, err := s.db.Exec(`
		INSERT INTO notifications (user_id, actor_id, type, entity_type, entity_id, content)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, actorID, notifType, entityType, entityID, content)
	if err != nil {
		log.Printf("Failed to create notification: %v", err)
		return
	}

	// Publish to Redis for real-time (future SSE/WebSocket support)
	s.redis.Publish(context.Background(), fmt.Sprintf("notif:%s", userID), notifType)
}

func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		limit, _ = strconv.Atoi(l)
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		offset, _ = strconv.Atoi(o)
	}

	var notifs []Notification
	s.db.Select(&notifs, `
		SELECT
			n.id, n.user_id, n.actor_id, n.type, n.entity_type, n.entity_id, n.content, n.read, n.created_at,
			u.username as actor_username,
			u.display_name as actor_display_name,
			u.avatar_url as actor_avatar_url
		FROM notifications n
		LEFT JOIN users u ON n.actor_id = u.id
		WHERE n.user_id = $1
		ORDER BY n.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)

	if notifs == nil {
		notifs = []Notification{}
	}

	utils.JSON(w, http.StatusOK, notifs)
}

func (s *Service) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	s.db.Exec(`UPDATE notifications SET read = true WHERE user_id = $1`, userID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "all notifications marked as read"})
}

func (s *Service) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	notifID := chi.URLParam(r, "notifID")
	s.db.Exec(`UPDATE notifications SET read = true WHERE id = $1 AND user_id = $2`, notifID, userID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "notification marked as read"})
}

func (s *Service) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var count int
	s.db.Get(&count, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = false`, userID)
	utils.JSON(w, http.StatusOK, map[string]int{"count": count})
}
