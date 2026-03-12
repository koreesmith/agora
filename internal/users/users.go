package users

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/media"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db    *store.DB
	media *media.Service
}

func NewService(db *store.DB, media *media.Service) *Service {
	return &Service{db: db, media: media}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/users/{username}",          s.GetProfile)
	r.Patch("/users/me",                s.UpdateProfile)
	r.Post("/users/me/avatar",          s.UploadAvatar)
	r.Post("/users/me/cover",           s.UploadCover)
	r.Get("/users/me/export",           s.ExportData)
	r.Post("/users/me/request-deletion", s.RequestDeletion)
	r.Delete("/users/me/request-deletion", s.CancelDeletion)
	r.Post("/users/me/delete-immediately", s.DeleteImmediately)
}

// ── Handlers ─────────────────────────────────────────────────────────────────

func (s *Service) GetProfile(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	viewerID := auth.UserIDFromCtx(r.Context())

	var u struct {
		ID             string  `json:"id"`
		Username       string  `json:"username"`
		DisplayName    string  `json:"display_name"`
		Bio            string  `json:"bio"`
		AvatarURL      string  `json:"avatar_url"`
		CoverURL       string  `json:"cover_url"`
		Location       string  `json:"location"`
		Website        string  `json:"website"`
		ProfilePrivate bool    `json:"profile_private"`
		IsRemote       bool    `json:"is_remote"`
		RemoteInstance string  `json:"remote_instance,omitempty"`
		CreatedAt      string  `json:"created_at"`
		FriendStatus   string  `json:"friend_status"`
		FriendCount    int     `json:"friend_count"`
	}

	err := s.db.QueryRow(`
		SELECT id, username, display_name, bio, avatar_url, cover_url,
		       location, website, profile_private, is_remote, remote_instance,
		       created_at
		FROM users WHERE username = $1 AND deletion_scheduled_at IS NULL
	`, username).Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.AvatarURL, &u.CoverURL,
		&u.Location, &u.Website, &u.ProfilePrivate, &u.IsRemote, &u.RemoteInstance,
		&u.CreatedAt,
	)
	if err != nil {
		writeError(w, 404, "user not found")
		return
	}

	// Friend status (directional: pending_incoming = they requested us)
	if viewerID != "" && viewerID != u.ID {
		var status, requesterID string
		s.db.QueryRow(`
			SELECT status, requester_id FROM friendships
			WHERE (requester_id = $1 AND addressee_id = $2)
			   OR (requester_id = $2 AND addressee_id = $1)
		`, viewerID, u.ID).Scan(&status, &requesterID)
		if status == "pending" && requesterID == u.ID {
			u.FriendStatus = "pending_incoming"
		} else {
			u.FriendStatus = status
		}
	}
	if viewerID == u.ID {
		u.FriendStatus = "self"
	}

	// Friend count (public)
	s.db.QueryRow(`
		SELECT COUNT(*) FROM friendships
		WHERE (requester_id = $1 OR addressee_id = $1) AND status = 'accepted'
	`, u.ID).Scan(&u.FriendCount)

	// Enforce privacy
	if u.ProfilePrivate && viewerID != u.ID && u.FriendStatus != "accepted" {
		writeJSON(w, 200, map[string]any{
			"id":           u.ID,
			"username":     u.Username,
			"display_name": u.DisplayName,
			"avatar_url":   u.AvatarURL,
			"profile_private": true,
			"friend_status": u.FriendStatus,
		})
		return
	}

	writeJSON(w, 200, u)
}

func (s *Service) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		DisplayName    *string `json:"display_name"`
		Bio            *string `json:"bio"`
		Location       *string `json:"location"`
		Website        *string `json:"website"`
		ProfilePrivate *bool   `json:"profile_private"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	sets := []string{"updated_at = NOW()"}
	args := []any{}
	i := 1

	if req.DisplayName != nil {
		sets = append(sets, fmt.Sprintf("display_name = $%d", i)); args = append(args, *req.DisplayName); i++
	}
	if req.Bio != nil {
		sets = append(sets, fmt.Sprintf("bio = $%d", i)); args = append(args, *req.Bio); i++
	}
	if req.Location != nil {
		sets = append(sets, fmt.Sprintf("location = $%d", i)); args = append(args, *req.Location); i++
	}
	if req.Website != nil {
		sets = append(sets, fmt.Sprintf("website = $%d", i)); args = append(args, *req.Website); i++
	}
	if req.ProfilePrivate != nil {
		sets = append(sets, fmt.Sprintf("profile_private = $%d", i)); args = append(args, *req.ProfilePrivate); i++
	}

	args = append(args, userID)
	_, err := s.db.Exec(
		fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(sets, ", "), i),
		args...,
	)
	if err != nil {
		writeError(w, 500, "update failed")
		return
	}

	writeJSON(w, 200, map[string]string{"message": "profile updated"})
}

func (s *Service) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	url, err := s.media.SaveUpload(r, "avatar", userID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.db.Exec(`UPDATE users SET avatar_url = $1, updated_at = NOW() WHERE id = $2`, url, userID)
	writeJSON(w, 200, map[string]string{"avatar_url": url})
}

func (s *Service) UploadCover(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	url, err := s.media.SaveUpload(r, "cover", userID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.db.Exec(`UPDATE users SET cover_url = $1, updated_at = NOW() WHERE id = $2`, url, userID)
	writeJSON(w, 200, map[string]string{"cover_url": url})
}


// ── GDPR Export ───────────────────────────────────────────────────────────────

func (s *Service) ExportData(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	exportJSON := func(filename string, query string, args ...any) {
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return
		}
		defer rows.Close()

		cols, _ := rows.Columns()
		var results []map[string]any
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals { ptrs[i] = &vals[i] }
			rows.Scan(ptrs...)
			row := map[string]any{}
			for i, c := range cols {
				row[c] = vals[i]
			}
			results = append(results, row)
		}

		f, _ := zw.Create(filename)
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		enc.Encode(results)
	}

	exportJSON("profile.json", `SELECT username, email, display_name, bio, avatar_url, location, website, created_at FROM users WHERE id = $1`, userID)
	exportJSON("posts.json", `SELECT content, image_url, visibility, created_at FROM posts WHERE author_id = $1 AND deleted_at IS NULL ORDER BY created_at`, userID)
	exportJSON("comments.json", `SELECT p.content, c.content as comment, c.created_at FROM posts c JOIN posts p ON p.id = c.parent_id WHERE c.author_id = $1 AND c.parent_id IS NOT NULL ORDER BY c.created_at`, userID)
	exportJSON("friends.json", `
		SELECT u.username, u.display_name, f.created_at as friends_since
		FROM friendships f
		JOIN users u ON (CASE WHEN f.requester_id = $1 THEN f.addressee_id ELSE f.requester_id END) = u.id
		WHERE (f.requester_id = $1 OR f.addressee_id = $1) AND f.status = 'accepted'
	`, userID)
	exportJSON("likes.json", `SELECT post_id, created_at FROM likes WHERE user_id = $1 ORDER BY created_at`, userID)

	zw.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="agora-data-export.zip"`)
	io.Copy(w, &buf)
}

// ── GDPR Deletion ─────────────────────────────────────────────────────────────

func (s *Service) RequestDeletion(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	var graceDays int = 30
	var graceStr string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'deletion_grace_days'`).Scan(&graceStr)
	if graceStr != "" {
		fmt.Sscanf(graceStr, "%d", &graceDays)
	}

	scheduledAt := time.Now().AddDate(0, 0, graceDays)
	s.db.Exec(`
		UPDATE users SET deletion_requested_at = NOW(), deletion_scheduled_at = $1
		WHERE id = $2
	`, scheduledAt, userID)

	writeJSON(w, 200, map[string]any{
		"message":      fmt.Sprintf("account deletion scheduled — you have %d days to cancel", graceDays),
		"scheduled_at": scheduledAt,
	})
}

func (s *Service) CancelDeletion(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	s.db.Exec(`
		UPDATE users SET deletion_requested_at = NULL, deletion_scheduled_at = NULL
		WHERE id = $1
	`, userID)
	writeJSON(w, 200, map[string]string{"message": "deletion cancelled"})
}

func (s *Service) DeleteImmediately(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	go s.purgeUser(userID)
	writeJSON(w, 200, map[string]string{"message": "account deleted"})
}

func (s *Service) purgeUser(userID string) {
	// Delete uploaded files
	var avatarURL, coverURL string
	s.db.QueryRow(`SELECT avatar_url, cover_url FROM users WHERE id = $1`, userID).Scan(&avatarURL, &coverURL)
	for _, u := range []string{avatarURL, coverURL} {
		if u != "" && strings.HasPrefix(u, "/uploads/") {
			os.Remove(filepath.Join(s.media.UploadDir(), strings.TrimPrefix(u, "/uploads/")))
		}
	}
	// Cascade deletes handle all related data via FK constraints
	s.db.Exec(`DELETE FROM users WHERE id = $1`, userID)
}

func (s *Service) StartDeletionCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rows, err := s.db.Query(`SELECT id FROM users WHERE deletion_scheduled_at < NOW()`)
			if err != nil {
				continue
			}
			var ids []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				ids = append(ids, id)
			}
			rows.Close()
			for _, id := range ids {
				s.purgeUser(id)
			}
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = sql.ErrNoRows

// ── Discover ──────────────────────────────────────────────────────────────────

type DiscoverUser struct {
	ID             string   `json:"id"`
	Username       string   `json:"username"`
	DisplayName    string   `json:"display_name"`
	AvatarURL      string   `json:"avatar_url"`
	Bio            string   `json:"bio"`
	MutualCount    int      `json:"mutual_count"`
	MutualFriends  []string `json:"mutual_friends"` // display names of up to 3
}

func (s *Service) Discover(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}

	// Show all non-friends on the instance, sorted by mutual count desc
	rows, err := s.db.Query(`
		WITH my_friends AS (
			SELECT CASE WHEN requester_id = $1 THEN addressee_id ELSE requester_id END AS fid
			FROM friendships
			WHERE (requester_id = $1 OR addressee_id = $1) AND status = 'accepted'
		)
		SELECT u.id, u.username, u.display_name, u.avatar_url, COALESCE(u.bio,''),
		       COUNT(DISTINCT f.id) AS mutual_count
		FROM users u
		LEFT JOIN friendships f ON f.status = 'accepted'
			AND (
				(f.requester_id = u.id AND f.addressee_id IN (SELECT fid FROM my_friends))
				OR (f.addressee_id = u.id AND f.requester_id IN (SELECT fid FROM my_friends))
			)
		WHERE u.id != $1
		  AND u.deleted_at IS NULL
		  AND u.email_verified = true
		  AND u.is_remote = false
		  AND u.id NOT IN (SELECT fid FROM my_friends)
		GROUP BY u.id
		ORDER BY mutual_count DESC, u.display_name
		LIMIT 50
	`, userID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	var results []DiscoverUser
	for rows.Next() {
		var u DiscoverUser
		rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.MutualCount)
		results = append(results, u)
	}

	// For each candidate, fetch up to 3 mutual friend display names
	for i, u := range results {
		mrows, err := s.db.Query(`
			WITH my_friends AS (
				SELECT CASE WHEN requester_id = $1 THEN addressee_id ELSE requester_id END AS fid
				FROM friendships WHERE (requester_id = $1 OR addressee_id = $1) AND status = 'accepted'
			)
			SELECT COALESCE(NULLIF(u2.display_name,''), u2.username)
			FROM friendships f
			JOIN users u2 ON u2.id = CASE
				WHEN f.requester_id = $2 THEN f.addressee_id
				ELSE f.requester_id
			END
			WHERE f.status = 'accepted'
			  AND (f.requester_id = $2 OR f.addressee_id = $2)
			  AND CASE WHEN f.requester_id = $2 THEN f.addressee_id ELSE f.requester_id END
			      IN (SELECT fid FROM my_friends)
			LIMIT 3
		`, userID, u.ID)
		if err == nil {
			var names []string
			for mrows.Next() {
				var name string
				mrows.Scan(&name)
				names = append(names, name)
			}
			mrows.Close()
			results[i].MutualFriends = names
		}
		if results[i].MutualFriends == nil {
			results[i].MutualFriends = []string{}
		}
	}

	if results == nil {
		results = []DiscoverUser{}
	}
	writeJSON(w, 200, map[string]any{"users": results})
}
