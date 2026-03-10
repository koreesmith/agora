package friends

import (
	"net/http"
	"time"

	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/pkg/middleware"
	"github.com/agora-social/agora/pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

type Service struct {
	db       *sqlx.DB
	notifSvc *notifications.Service
}

func NewService(db *sqlx.DB, notifSvc *notifications.Service) *Service {
	return &Service{db: db, notifSvc: notifSvc}
}

type FriendGroup struct {
	ID        string    `db:"id" json:"id"`
	UserID    string    `db:"user_id" json:"user_id"`
	Name      string    `db:"name" json:"name"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

func RegisterRoutes(r chi.Router, svc *Service) {
	r.Post("/friends/request/{userID}", svc.SendRequest)
	r.Post("/friends/accept/{userID}", svc.AcceptRequest)
	r.Post("/friends/decline/{userID}", svc.DeclineRequest)
	r.Delete("/friends/{userID}", svc.Unfriend)
	r.Get("/friends", svc.ListFriends)
	r.Get("/friends/requests", svc.ListRequests)

	// Friend groups
	r.Get("/friend-groups", svc.ListGroups)
	r.Post("/friend-groups", svc.CreateGroup)
	r.Delete("/friend-groups/{groupID}", svc.DeleteGroup)
	r.Post("/friend-groups/{groupID}/members/{friendID}", svc.AddToGroup)
	r.Delete("/friend-groups/{groupID}/members/{friendID}", svc.RemoveFromGroup)
	r.Get("/friend-groups/{groupID}/members", svc.ListGroupMembers)
}

func (s *Service) SendRequest(w http.ResponseWriter, r *http.Request) {
	requesterID := middleware.GetUserID(r.Context())
	addresseeID := chi.URLParam(r, "userID")

	if requesterID == addresseeID {
		utils.Error(w, http.StatusBadRequest, "cannot friend yourself")
		return
	}

	// Check if addressee exists
	var exists bool
	s.db.Get(&exists, "SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)", addresseeID)
	if !exists {
		utils.Error(w, http.StatusNotFound, "user not found")
		return
	}

	// Check if already friends or pending
	var existing struct {
		ID     string `db:"id"`
		Status string `db:"status"`
	}
	err := s.db.Get(&existing, `
		SELECT id, status FROM friendships
		WHERE (requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1)
	`, requesterID, addresseeID)

	if err == nil {
		if existing.Status == "accepted" {
			utils.Error(w, http.StatusConflict, "already friends")
			return
		}
		if existing.Status == "pending" {
			utils.Error(w, http.StatusConflict, "friend request already pending")
			return
		}
		if existing.Status == "blocked" {
			utils.Error(w, http.StatusForbidden, "unable to send friend request")
			return
		}
	}

	var friendshipID string
	s.db.QueryRow(`
		INSERT INTO friendships (requester_id, addressee_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT (requester_id, addressee_id) DO UPDATE SET status = 'pending', updated_at = NOW()
		RETURNING id
	`, requesterID, addresseeID).Scan(&friendshipID)

	go s.notifSvc.Create(addresseeID, requesterID, "friend_request", "friendship", friendshipID, "sent you a friend request")

	utils.JSON(w, http.StatusOK, map[string]string{"message": "friend request sent"})
}

func (s *Service) AcceptRequest(w http.ResponseWriter, r *http.Request) {
	addresseeID := middleware.GetUserID(r.Context())
	requesterID := chi.URLParam(r, "userID")

	result, err := s.db.Exec(`
		UPDATE friendships SET status = 'accepted', updated_at = NOW()
		WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'
	`, requesterID, addresseeID)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "failed to accept request")
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		utils.Error(w, http.StatusNotFound, "friend request not found")
		return
	}

	go s.notifSvc.Create(requesterID, addresseeID, "friend_accepted", "user", addresseeID, "accepted your friend request")

	utils.JSON(w, http.StatusOK, map[string]string{"message": "friend request accepted"})
}

func (s *Service) DeclineRequest(w http.ResponseWriter, r *http.Request) {
	addresseeID := middleware.GetUserID(r.Context())
	requesterID := chi.URLParam(r, "userID")

	s.db.Exec(`
		UPDATE friendships SET status = 'declined', updated_at = NOW()
		WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'
	`, requesterID, addresseeID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "friend request declined"})
}

func (s *Service) Unfriend(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	otherID := chi.URLParam(r, "userID")

	s.db.Exec(`
		DELETE FROM friendships
		WHERE status = 'accepted'
		AND ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
	`, userID, otherID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "unfriended"})
}

func (s *Service) ListFriends(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	var friends []struct {
		ID          string    `db:"id" json:"id"`
		Username    string    `db:"username" json:"username"`
		DisplayName string    `db:"display_name" json:"display_name"`
		AvatarURL   string    `db:"avatar_url" json:"avatar_url"`
		FriendsSince time.Time `db:"friends_since" json:"friends_since"`
	}

	s.db.Select(&friends, `
		SELECT u.id, u.username, u.display_name, u.avatar_url, f.updated_at as friends_since
		FROM friendships f
		JOIN users u ON (CASE WHEN f.requester_id = $1 THEN f.addressee_id ELSE f.requester_id END) = u.id
		WHERE f.status = 'accepted' AND (f.requester_id = $1 OR f.addressee_id = $1)
		ORDER BY f.updated_at DESC
	`, userID)

	utils.JSON(w, http.StatusOK, friends)
}

func (s *Service) ListRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	var requests []struct {
		ID          string    `db:"id" json:"id"`
		Username    string    `db:"username" json:"username"`
		DisplayName string    `db:"display_name" json:"display_name"`
		AvatarURL   string    `db:"avatar_url" json:"avatar_url"`
		RequestedAt time.Time `db:"requested_at" json:"requested_at"`
	}

	s.db.Select(&requests, `
		SELECT u.id, u.username, u.display_name, u.avatar_url, f.created_at as requested_at
		FROM friendships f
		JOIN users u ON f.requester_id = u.id
		WHERE f.addressee_id = $1 AND f.status = 'pending'
		ORDER BY f.created_at DESC
	`, userID)

	utils.JSON(w, http.StatusOK, requests)
}

// Friend Groups

func (s *Service) ListGroups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var groups []FriendGroup
	s.db.Select(&groups, `SELECT id, user_id, name, created_at FROM friend_groups WHERE user_id = $1 ORDER BY name`, userID)
	utils.JSON(w, http.StatusOK, groups)
}

func (s *Service) CreateGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req struct{ Name string `json:"name"` }
	if err := utils.DecodeJSON(r, &req); err != nil || req.Name == "" {
		utils.Error(w, http.StatusBadRequest, "name required")
		return
	}

	var group FriendGroup
	err := s.db.QueryRow(`
		INSERT INTO friend_groups (user_id, name) VALUES ($1, $2)
		RETURNING id, user_id, name, created_at
	`, userID, req.Name).Scan(&group.ID, &group.UserID, &group.Name, &group.CreatedAt)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "failed to create group")
		return
	}

	utils.JSON(w, http.StatusCreated, group)
}

func (s *Service) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	groupID := chi.URLParam(r, "groupID")

	s.db.Exec(`DELETE FROM friend_groups WHERE id = $1 AND user_id = $2`, groupID, userID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "group deleted"})
}

func (s *Service) AddToGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	groupID := chi.URLParam(r, "groupID")
	friendID := chi.URLParam(r, "friendID")

	// Verify group ownership
	var ownerID string
	s.db.Get(&ownerID, "SELECT user_id FROM friend_groups WHERE id = $1", groupID)
	if ownerID != userID {
		utils.Error(w, http.StatusForbidden, "not your group")
		return
	}

	// Verify actually friends
	var isFriend bool
	s.db.Get(&isFriend, `
		SELECT EXISTS(SELECT 1 FROM friendships WHERE status = 'accepted'
		AND ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1)))
	`, userID, friendID)
	if !isFriend {
		utils.Error(w, http.StatusBadRequest, "can only add friends to groups")
		return
	}

	s.db.Exec(`INSERT INTO friend_group_members (group_id, friend_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, groupID, friendID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "added to group"})
}

func (s *Service) RemoveFromGroup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	groupID := chi.URLParam(r, "groupID")
	friendID := chi.URLParam(r, "friendID")

	var ownerID string
	s.db.Get(&ownerID, "SELECT user_id FROM friend_groups WHERE id = $1", groupID)
	if ownerID != userID {
		utils.Error(w, http.StatusForbidden, "not your group")
		return
	}

	s.db.Exec(`DELETE FROM friend_group_members WHERE group_id = $1 AND friend_id = $2`, groupID, friendID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "removed from group"})
}

func (s *Service) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	groupID := chi.URLParam(r, "groupID")

	var ownerID string
	s.db.Get(&ownerID, "SELECT user_id FROM friend_groups WHERE id = $1", groupID)
	if ownerID != userID {
		utils.Error(w, http.StatusForbidden, "not your group")
		return
	}

	var members []struct {
		ID          string `db:"id" json:"id"`
		Username    string `db:"username" json:"username"`
		DisplayName string `db:"display_name" json:"display_name"`
		AvatarURL   string `db:"avatar_url" json:"avatar_url"`
	}
	s.db.Select(&members, `
		SELECT u.id, u.username, u.display_name, u.avatar_url
		FROM friend_group_members fgm
		JOIN users u ON fgm.friend_id = u.id
		WHERE fgm.group_id = $1
		ORDER BY u.display_name
	`, groupID)

	utils.JSON(w, http.StatusOK, members)
}
