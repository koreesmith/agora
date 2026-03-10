package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/agora-social/agora/pkg/config"
	"github.com/agora-social/agora/pkg/middleware"
	"github.com/agora-social/agora/pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

type Service struct {
	db  *sqlx.DB
	cfg *config.Config
}

func NewService(db *sqlx.DB, cfg *config.Config) *Service {
	return &Service{db: db, cfg: cfg}
}

func RegisterRoutes(r chi.Router, svc *Service) {
	r.Get("/admin/settings", svc.GetSettings)
	r.Patch("/admin/settings", svc.UpdateSettings)
	r.Post("/admin/logo", svc.UploadLogo)

	r.Get("/admin/users", svc.ListUsers)
	r.Patch("/admin/users/{userID}/role", svc.SetRole)
	r.Delete("/admin/users/{userID}", svc.DeleteUser)

	r.Get("/admin/invites", svc.ListInvites)
	r.Post("/admin/invites", svc.CreateInvite)
	r.Delete("/admin/invites/{inviteID}", svc.RevokeInvite)

	r.Get("/admin/audit-log", svc.GetAuditLog)
	r.Get("/admin/stats", svc.GetStats)

	r.Get("/admin/federation/instances", svc.ListInstances)
	r.Post("/admin/federation/instances/{instanceID}/block", svc.BlockInstance)
	r.Post("/admin/federation/instances/{instanceID}/unblock", svc.UnblockInstance)
}

func (s *Service) GetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Queryx(`SELECT key, value FROM instance_settings ORDER BY key`)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		settings[k] = v
	}

	// Redact SMTP password
	if _, ok := settings["smtp_password"]; ok {
		settings["smtp_password"] = "••••••••"
	}

	utils.JSON(w, http.StatusOK, settings)
}

func (s *Service) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.GetUserID(r.Context())
	var updates map[string]string
	if err := utils.DecodeJSON(r, &updates); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Protected keys that cannot be updated via this endpoint
	protected := map[string]bool{}

	for k, v := range updates {
		if protected[k] {
			continue
		}
		// Don't overwrite smtp_password with redacted value
		if k == "smtp_password" && v == "••••••••" {
			continue
		}
		s.db.Exec(`
			INSERT INTO instance_settings (key, value, updated_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
		`, k, v)
	}

	s.db.Exec(`INSERT INTO audit_log (actor_id, action) VALUES ($1, 'update_settings')`, adminID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "settings updated"})
}

func (s *Service) UploadLogo(w http.ResponseWriter, r *http.Request) {
	// Handled by media service; just stores the URL in settings
	utils.JSON(w, http.StatusOK, map[string]string{"message": "use /api/media/upload?category=instance"})
}

func (s *Service) ListUsers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("q")
	var users []struct {
		ID              string    `db:"id" json:"id"`
		Username        string    `db:"username" json:"username"`
		Email           string    `db:"email" json:"email"`
		DisplayName     string    `db:"display_name" json:"display_name"`
		Role            string    `db:"role" json:"role"`
		IsSuspended     bool      `db:"is_suspended" json:"is_suspended"`
		EmailVerified   bool      `db:"email_verified" json:"email_verified"`
		IsRemote        bool      `db:"is_remote" json:"is_remote"`
		DeletionScheduled *time.Time `db:"deletion_scheduled_at" json:"deletion_scheduled_at,omitempty"`
		CreatedAt       time.Time `db:"created_at" json:"created_at"`
	}

	query := `
		SELECT id, username, email, display_name, role, is_suspended, email_verified, is_remote, deletion_scheduled_at, created_at
		FROM users
	`
	args := []any{}
	if search != "" {
		query += ` WHERE username ILIKE $1 OR email ILIKE $1 OR display_name ILIKE $1`
		args = append(args, "%"+search+"%")
	}
	query += ` ORDER BY created_at DESC LIMIT 100`

	s.db.Select(&users, query, args...)
	if users == nil {
		users = []struct {
			ID              string    `db:"id" json:"id"`
			Username        string    `db:"username" json:"username"`
			Email           string    `db:"email" json:"email"`
			DisplayName     string    `db:"display_name" json:"display_name"`
			Role            string    `db:"role" json:"role"`
			IsSuspended     bool      `db:"is_suspended" json:"is_suspended"`
			EmailVerified   bool      `db:"email_verified" json:"email_verified"`
			IsRemote        bool      `db:"is_remote" json:"is_remote"`
			DeletionScheduled *time.Time `db:"deletion_scheduled_at" json:"deletion_scheduled_at,omitempty"`
			CreatedAt       time.Time `db:"created_at" json:"created_at"`
		}{}
	}
	utils.JSON(w, http.StatusOK, users)
}

func (s *Service) SetRole(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.GetUserID(r.Context())
	targetUserID := chi.URLParam(r, "userID")

	var req struct{ Role string `json:"role"` }
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	allowed := map[string]bool{"user": true, "moderator": true, "admin": true}
	if !allowed[req.Role] {
		utils.Error(w, http.StatusBadRequest, "invalid role")
		return
	}

	s.db.Exec(`UPDATE users SET role = $1 WHERE id = $2`, req.Role, targetUserID)
	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id) VALUES ($1, 'set_role', 'user', $2)`, adminID, targetUserID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "role updated"})
}

func (s *Service) DeleteUser(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.GetUserID(r.Context())
	targetUserID := chi.URLParam(r, "userID")

	// Anonymize
	s.db.Exec(`
		UPDATE users SET
			email = 'admin_deleted_' || id || '@deleted',
			password_hash = '',
			display_name = 'Deleted User',
			bio = '', avatar_url = '', cover_url = '',
			is_suspended = true
		WHERE id = $1
	`, targetUserID)
	s.db.Exec(`UPDATE posts SET content = '[deleted]', media_urls = '{}', deleted_at = NOW() WHERE author_id = $1`, targetUserID)
	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id) VALUES ($1, 'delete_user', 'user', $2)`, adminID, targetUserID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "user deleted"})
}

func (s *Service) ListInvites(w http.ResponseWriter, r *http.Request) {
	var invites []struct {
		ID          string     `db:"id" json:"id"`
		Code        string     `db:"code" json:"code"`
		CreatedBy   string     `db:"created_by" json:"created_by"`
		UsedBy      *string    `db:"used_by" json:"used_by,omitempty"`
		UsedAt      *time.Time `db:"used_at" json:"used_at,omitempty"`
		ExpiresAt   *time.Time `db:"expires_at" json:"expires_at,omitempty"`
		CreatedAt   time.Time  `db:"created_at" json:"created_at"`
		CreatorName string     `db:"creator_name" json:"creator_name"`
	}
	s.db.Select(&invites, `
		SELECT i.*, u.username as creator_name
		FROM invite_codes i
		JOIN users u ON i.created_by = u.id
		ORDER BY i.created_at DESC
	`)
	if invites == nil {
		invites = []struct {
			ID          string     `db:"id" json:"id"`
			Code        string     `db:"code" json:"code"`
			CreatedBy   string     `db:"created_by" json:"created_by"`
			UsedBy      *string    `db:"used_by" json:"used_by,omitempty"`
			UsedAt      *time.Time `db:"used_at" json:"used_at,omitempty"`
			ExpiresAt   *time.Time `db:"expires_at" json:"expires_at,omitempty"`
			CreatedAt   time.Time  `db:"created_at" json:"created_at"`
			CreatorName string     `db:"creator_name" json:"creator_name"`
		}{}
	}
	utils.JSON(w, http.StatusOK, invites)
}

func (s *Service) CreateInvite(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.GetUserID(r.Context())

	var req struct {
		ExpiresAt *time.Time `json:"expires_at"`
	}
	utils.DecodeJSON(r, &req)

	code := generateInviteCode()
	var inviteID string
	s.db.QueryRow(`
		INSERT INTO invite_codes (code, created_by, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id
	`, code, adminID, req.ExpiresAt).Scan(&inviteID)

	utils.JSON(w, http.StatusCreated, map[string]string{"id": inviteID, "code": code})
}

func (s *Service) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	inviteID := chi.URLParam(r, "inviteID")
	s.db.Exec(`DELETE FROM invite_codes WHERE id = $1 AND used_by IS NULL`, inviteID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "invite revoked"})
}

func (s *Service) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	var entries []struct {
		ID          string    `db:"id" json:"id"`
		ActorID     *string   `db:"actor_id" json:"actor_id,omitempty"`
		Action      string    `db:"action" json:"action"`
		TargetType  string    `db:"target_type" json:"target_type"`
		TargetID    string    `db:"target_id" json:"target_id"`
		CreatedAt   time.Time `db:"created_at" json:"created_at"`
		ActorName   *string   `db:"actor_name" json:"actor_name,omitempty"`
	}
	s.db.Select(&entries, `
		SELECT al.id, al.actor_id, al.action, al.target_type, al.target_id, al.created_at,
			u.username as actor_name
		FROM audit_log al
		LEFT JOIN users u ON al.actor_id = u.id
		ORDER BY al.created_at DESC
		LIMIT 200
	`)
	if entries == nil {
		entries = []struct {
			ID          string    `db:"id" json:"id"`
			ActorID     *string   `db:"actor_id" json:"actor_id,omitempty"`
			Action      string    `db:"action" json:"action"`
			TargetType  string    `db:"target_type" json:"target_type"`
			TargetID    string    `db:"target_id" json:"target_id"`
			CreatedAt   time.Time `db:"created_at" json:"created_at"`
			ActorName   *string   `db:"actor_name" json:"actor_name,omitempty"`
		}{}
	}
	utils.JSON(w, http.StatusOK, entries)
}

func (s *Service) GetStats(w http.ResponseWriter, r *http.Request) {
	var stats struct {
		TotalUsers    int `db:"total_users" json:"total_users"`
		ActiveUsers   int `db:"active_users" json:"active_users"`
		TotalPosts    int `db:"total_posts" json:"total_posts"`
		TotalReports  int `db:"total_reports" json:"total_reports"`
		PendingReports int `db:"pending_reports" json:"pending_reports"`
	}
	s.db.Get(&stats, `
		SELECT
			(SELECT COUNT(*) FROM users WHERE is_remote = false) as total_users,
			(SELECT COUNT(*) FROM users WHERE is_remote = false AND is_suspended = false) as active_users,
			(SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL) as total_posts,
			(SELECT COUNT(*) FROM reports) as total_reports,
			(SELECT COUNT(*) FROM reports WHERE status = 'pending') as pending_reports
	`)
	utils.JSON(w, http.StatusOK, stats)
}

func (s *Service) ListInstances(w http.ResponseWriter, r *http.Request) {
	var instances []struct {
		ID          string    `db:"id" json:"id"`
		Domain      string    `db:"domain" json:"domain"`
		Name        string    `db:"name" json:"name"`
		InstanceURL string    `db:"instance_url" json:"instance_url"`
		Status      string    `db:"status" json:"status"`
		LastSeenAt  time.Time `db:"last_seen_at" json:"last_seen_at"`
		CreatedAt   time.Time `db:"created_at" json:"created_at"`
	}
	s.db.Select(&instances, `SELECT id, domain, name, instance_url, status, last_seen_at, created_at FROM federated_instances ORDER BY created_at DESC`)
	if instances == nil {
		instances = []struct {
			ID          string    `db:"id" json:"id"`
			Domain      string    `db:"domain" json:"domain"`
			Name        string    `db:"name" json:"name"`
			InstanceURL string    `db:"instance_url" json:"instance_url"`
			Status      string    `db:"status" json:"status"`
			LastSeenAt  time.Time `db:"last_seen_at" json:"last_seen_at"`
			CreatedAt   time.Time `db:"created_at" json:"created_at"`
		}{}
	}
	utils.JSON(w, http.StatusOK, instances)
}

func (s *Service) BlockInstance(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.GetUserID(r.Context())
	instanceID := chi.URLParam(r, "instanceID")
	s.db.Exec(`UPDATE federated_instances SET status = 'blocked' WHERE id = $1`, instanceID)
	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id) VALUES ($1, 'block_instance', 'instance', $2)`, adminID, instanceID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "instance blocked"})
}

func (s *Service) UnblockInstance(w http.ResponseWriter, r *http.Request) {
	adminID := middleware.GetUserID(r.Context())
	instanceID := chi.URLParam(r, "instanceID")
	s.db.Exec(`UPDATE federated_instances SET status = 'active' WHERE id = $1`, instanceID)
	s.db.Exec(`INSERT INTO audit_log (actor_id, action, target_type, target_id) VALUES ($1, 'unblock_instance', 'instance', $2)`, adminID, instanceID)
	utils.JSON(w, http.StatusOK, map[string]string{"message": "instance unblocked"})
}

func generateInviteCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
