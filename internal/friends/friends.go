package friends

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

// fedSender is the subset of federation.Service used here (avoids import cycle).
type fedSender interface {
	BroadcastToFriendInstances(userID string, activity any)
	SendToUserInstance(remoteInstance, instanceURL string, activity any)
}

type Service struct {
	db    *store.DB
	notif *notifications.Service
	fed   fedSender
}

func NewService(db *store.DB, notif *notifications.Service) *Service {
	return &Service{db: db, notif: notif}
}

func (s *Service) SetFed(f fedSender) { s.fed = f }

func RegisterRoutes(r chi.Router, s *Service) {
	// Friendships
	r.Get("/friends",                     s.ListFriends)
	r.Get("/friends/requests",            s.ListRequests)
	r.Post("/friends/request/{userID}",   s.SendRequest)
	r.Post("/friends/accept/{userID}",    s.Accept)
	r.Post("/friends/decline/{userID}",   s.Decline)
	r.Delete("/friends/{userID}",         s.Unfriend)

	// Friend groups
	r.Get("/friend-groups",                           s.ListGroups)
	r.Post("/friend-groups",                          s.CreateGroup)
	r.Delete("/friend-groups/{groupID}",              s.DeleteGroup)
	r.Get("/friend-groups/{groupID}/members",         s.ListGroupMembers)
	r.Post("/friend-groups/{groupID}/members/{friendID}", s.AddToGroup)
	r.Delete("/friend-groups/{groupID}/members/{friendID}", s.RemoveFromGroup)
}

// ── Friendships ───────────────────────────────────────────────────────────────

func (s *Service) ListFriends(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url,
		       u.bio, u.is_remote, u.remote_instance, f.created_at
		FROM friendships f
		JOIN users u ON (
			CASE WHEN f.requester_id = $1 THEN f.addressee_id ELSE f.requester_id END
		) = u.id
		WHERE (f.requester_id = $1 OR f.addressee_id = $1)
		  AND f.status = 'accepted'
		ORDER BY u.display_name
	`, userID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type Friend struct {
		ID             string `json:"id"`
		Username       string `json:"username"`
		DisplayName    string `json:"display_name"`
		AvatarURL      string `json:"avatar_url"`
		Bio            string `json:"bio"`
		IsRemote       bool   `json:"is_remote"`
		RemoteInstance string `json:"remote_instance,omitempty"`
		FriendsSince   string `json:"friends_since"`
	}

	var friends []Friend
	for rows.Next() {
		var f Friend
		rows.Scan(&f.ID, &f.Username, &f.DisplayName, &f.AvatarURL,
			&f.Bio, &f.IsRemote, &f.RemoteInstance, &f.FriendsSince)
		friends = append(friends, f)
	}
	if friends == nil { friends = []Friend{} }
	writeJSON(w, 200, map[string]any{"friends": friends})
}

func (s *Service) ListRequests(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	type Req struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		RequestedAt string `json:"requested_at"`
	}

	// Incoming
	inRows, _ := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url, f.created_at
		FROM friendships f JOIN users u ON u.id = f.requester_id
		WHERE f.addressee_id = $1 AND f.status = 'pending'
		ORDER BY f.created_at DESC
	`, userID)
	defer inRows.Close()
	var incoming []Req
	for inRows.Next() {
		var req Req
		inRows.Scan(&req.ID, &req.Username, &req.DisplayName, &req.AvatarURL, &req.RequestedAt)
		incoming = append(incoming, req)
	}

	// Outgoing
	outRows, _ := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url, f.created_at
		FROM friendships f JOIN users u ON u.id = f.addressee_id
		WHERE f.requester_id = $1 AND f.status = 'pending'
		ORDER BY f.created_at DESC
	`, userID)
	defer outRows.Close()
	var outgoing []Req
	for outRows.Next() {
		var req Req
		outRows.Scan(&req.ID, &req.Username, &req.DisplayName, &req.AvatarURL, &req.RequestedAt)
		outgoing = append(outgoing, req)
	}

	if incoming == nil { incoming = []Req{} }
	if outgoing == nil { outgoing = []Req{} }
	writeJSON(w, 200, map[string]any{"incoming": incoming, "outgoing": outgoing})
}

func (s *Service) SendRequest(w http.ResponseWriter, r *http.Request) {
	requesterID := auth.UserIDFromCtx(r.Context())
	addresseeID := chi.URLParam(r, "userID")

	if requesterID == addresseeID {
		writeError(w, 400, "cannot friend yourself")
		return
	}

	// Check addressee exists
	var exists bool
	var addresseeAPActorURL string
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1), COALESCE((SELECT ap_actor_url FROM users WHERE id = $1), '')`, addresseeID).
		Scan(&exists, &addresseeAPActorURL)
	if !exists {
		writeError(w, 404, "user not found")
		return
	}
	// AGORA-167: a genuine ActivityPub actor has no concept of friending —
	// only following (federation.FollowFediverseAccount) — so a friend
	// request here would insert a pending row that can never be accepted.
	if addresseeAPActorURL != "" {
		writeError(w, 400, "fediverse accounts can't be added as friends — follow them instead")
		return
	}

	// Block check — treat as if user doesn't exist
	var isBlocked bool
	s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`,
		requesterID, addresseeID).Scan(&isBlocked)
	if isBlocked {
		writeError(w, 404, "user not found")
		return
	}

	// Check not already friends / pending
	var status string
	s.db.QueryRow(`
		SELECT status FROM friendships
		WHERE (requester_id = $1 AND addressee_id = $2)
		   OR (requester_id = $2 AND addressee_id = $1)
	`, requesterID, addresseeID).Scan(&status)
	if status == "accepted" {
		writeError(w, 409, "already friends")
		return
	}
	if status == "pending" {
		writeError(w, 409, "request already pending")
		return
	}

	_, err := s.db.Exec(`
		INSERT INTO friendships (requester_id, addressee_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT (requester_id, addressee_id) DO UPDATE SET status = 'pending', updated_at = NOW()
	`, requesterID, addresseeID)
	if err != nil {
		writeError(w, 500, "could not send request")
		return
	}

	go s.notif.Create(addresseeID, requesterID, "friend_request", "", "")

	// If addressee is on a remote instance, send the request over federation
	if s.fed != nil {
		var isRemote bool
		var remoteInstance, remoteUserID, requesterUsername string
		s.db.QueryRow(`SELECT is_remote, remote_instance, remote_user_id FROM users WHERE id = $1`, addresseeID).
			Scan(&isRemote, &remoteInstance, &remoteUserID)
		s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, requesterID).Scan(&requesterUsername)
		if isRemote && remoteInstance != "" {
			go s.fed.SendToUserInstance(remoteInstance, "https://"+remoteInstance, map[string]any{
				"type":       "friend_request",
				"actor":      requesterUsername,
				"instance_id": domainFromCfg(s.db),
				"timestamp":  time.Now().Unix(),
				"object": map[string]string{
					"from_handle": requesterUsername,
					"to_handle":   remoteUserID,
				},
			})
		}
	}

	writeJSON(w, 200, map[string]string{"message": "friend request sent"})
}

func (s *Service) Accept(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	requesterID := chi.URLParam(r, "userID")

	res, err := s.db.Exec(`
		UPDATE friendships SET status = 'accepted', updated_at = NOW()
		WHERE requester_id = $1 AND addressee_id = $2 AND status = 'pending'
	`, requesterID, userID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, 404, "no pending request found")
		return
	}

	go s.notif.Create(requesterID, userID, "friend_accepted", "", "")

	// If requester is remote, send accept back to their instance
	if s.fed != nil {
		var isRemote bool
		var remoteInstance, remoteUserID, accepterUsername string
		s.db.QueryRow(`SELECT is_remote, remote_instance, remote_user_id FROM users WHERE id = $1`, requesterID).
			Scan(&isRemote, &remoteInstance, &remoteUserID)
		s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&accepterUsername)
		if isRemote && remoteInstance != "" {
			go s.fed.SendToUserInstance(remoteInstance, "https://"+remoteInstance, map[string]any{
				"type":        "friend_accept",
				"actor":       accepterUsername,
				"instance_id": domainFromCfg(s.db),
				"timestamp":   time.Now().Unix(),
				"object": map[string]string{
					"from_handle": accepterUsername,
					"to_handle":   remoteUserID,
				},
			})
		}
	}

	writeJSON(w, 200, map[string]string{"message": "friend request accepted"})
}

func (s *Service) Decline(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	otherID := chi.URLParam(r, "userID")

	s.db.Exec(`
		UPDATE friendships SET status = 'declined', updated_at = NOW()
		WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
		  AND status = 'pending'
	`, otherID, userID)
	writeJSON(w, 200, map[string]string{"message": "request declined"})
}

func (s *Service) Unfriend(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	otherID := chi.URLParam(r, "userID")

	s.db.Exec(`
		DELETE FROM friendships
		WHERE (requester_id = $1 AND addressee_id = $2)
		   OR (requester_id = $2 AND addressee_id = $1)
	`, userID, otherID)
	writeJSON(w, 200, map[string]string{"message": "unfriended"})
}

// ── Friend Groups ─────────────────────────────────────────────────────────────

func (s *Service) ListGroups(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	rows, _ := s.db.Query(`
		SELECT g.id, g.name, g.created_at,
		       COUNT(m.friend_id) as member_count
		FROM friend_groups g
		LEFT JOIN friend_group_members m ON m.group_id = g.id
		WHERE g.user_id = $1
		GROUP BY g.id, g.name, g.created_at
		ORDER BY g.name
	`, userID)
	defer rows.Close()

	type Group struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		MemberCount int    `json:"member_count"`
		CreatedAt   string `json:"created_at"`
	}
	var groups []Group
	for rows.Next() {
		var g Group
		rows.Scan(&g.ID, &g.Name, &g.CreatedAt, &g.MemberCount)
		groups = append(groups, g)
	}
	if groups == nil { groups = []Group{} }
	writeJSON(w, 200, map[string]any{"groups": groups})
}

func (s *Service) CreateGroup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, 400, "name required")
		return
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO friend_groups (user_id, name) VALUES ($1, $2) RETURNING id
	`, userID, req.Name).Scan(&id)
	if err != nil {
		writeError(w, 409, "group name already exists")
		return
	}
	writeJSON(w, 201, map[string]string{"id": id, "name": req.Name})
}

func (s *Service) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	groupID := chi.URLParam(r, "groupID")
	s.db.Exec(`DELETE FROM friend_groups WHERE id = $1 AND user_id = $2`, groupID, userID)
	writeJSON(w, 200, map[string]string{"message": "group deleted"})
}

func (s *Service) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	groupID := chi.URLParam(r, "groupID")

	// Verify ownership
	var ownerID string
	s.db.QueryRow(`SELECT user_id FROM friend_groups WHERE id = $1`, groupID).Scan(&ownerID)
	if ownerID != userID {
		writeError(w, 403, "forbidden")
		return
	}

	rows, _ := s.db.Query(`
		SELECT u.id, u.username, u.display_name, u.avatar_url, u.is_remote, u.remote_instance
		FROM friend_group_members m
		JOIN users u ON u.id = m.friend_id
		WHERE m.group_id = $1
		ORDER BY u.display_name
	`, groupID)
	defer rows.Close()

	type Member struct {
		ID             string `json:"id"`
		Username       string `json:"username"`
		DisplayName    string `json:"display_name"`
		AvatarURL      string `json:"avatar_url"`
		IsRemote       bool   `json:"is_remote"`
		RemoteInstance string `json:"remote_instance"`
	}
	var members []Member
	for rows.Next() {
		var m Member
		rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.AvatarURL, &m.IsRemote, &m.RemoteInstance)
		members = append(members, m)
	}
	if members == nil { members = []Member{} }
	writeJSON(w, 200, map[string]any{"members": members})
}

func (s *Service) AddToGroup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	groupID := chi.URLParam(r, "groupID")
	friendID := chi.URLParam(r, "friendID")

	// Verify group ownership
	var ownerID string
	s.db.QueryRow(`SELECT user_id FROM friend_groups WHERE id = $1`, groupID).Scan(&ownerID)
	if ownerID != userID {
		writeError(w, 403, "forbidden")
		return
	}

	// AGORA-182: list membership isn't friendship-only anymore — a followed
	// fediverse (or, once AGORA-195 lands, Bluesky) account can join a list
	// too, read-side only. There's no mutual "accept" for a one-way follow,
	// so the bar is just "the caller follows them and it's been accepted",
	// checked via the cached remote-actor row ListFollowing already joins
	// against (ap_following.followed_actor_url = users.ap_actor_url).
	var ok bool
	s.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM friendships
			WHERE status = 'accepted'
			  AND ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
		) OR EXISTS(
			SELECT 1 FROM ap_following af
			JOIN users u ON u.ap_actor_url = af.followed_actor_url
			WHERE af.follower_user_id = $1 AND af.accepted = true AND u.id = $2
		)
	`, userID, friendID).Scan(&ok)
	if !ok {
		writeError(w, 400, "can only add friends or followed fediverse accounts to lists")
		return
	}

	s.db.Exec(`INSERT INTO friend_group_members (group_id, friend_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, groupID, friendID)
	writeJSON(w, 200, map[string]string{"message": "added to group"})
}

func (s *Service) RemoveFromGroup(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	groupID := chi.URLParam(r, "groupID")
	friendID := chi.URLParam(r, "friendID")

	var ownerID string
	s.db.QueryRow(`SELECT user_id FROM friend_groups WHERE id = $1`, groupID).Scan(&ownerID)
	if ownerID != userID {
		writeError(w, 403, "forbidden")
		return
	}

	s.db.Exec(`DELETE FROM friend_group_members WHERE group_id = $1 AND friend_id = $2`, groupID, friendID)
	writeJSON(w, 200, map[string]string{"message": "removed from group"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func domainFromCfg(db *store.DB) string {
	var domain string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'instance_domain'`).Scan(&domain)
	return domain
}
