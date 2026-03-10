package moderation

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

type Report struct {
	ID                string     `db:"id" json:"id"`
	ReporterID        string     `db:"reporter_id" json:"reporter_id"`
	ReportedUserID    *string    `db:"reported_user_id" json:"reported_user_id,omitempty"`
	ReportedPostID    *string    `db:"reported_post_id" json:"reported_post_id,omitempty"`
	ReportedCommentID *string    `db:"reported_comment_id" json:"reported_comment_id,omitempty"`
	Reason            string     `db:"reason" json:"reason"`
	Details           string     `db:"details" json:"details"`
	Status            string     `db:"status" json:"status"`
	ReviewedBy        *string    `db:"reviewed_by" json:"reviewed_by,omitempty"`
	ReviewedAt        *time.Time `db:"reviewed_at" json:"reviewed_at,omitempty"`
	ReviewNotes       string     `db:"review_notes" json:"review_notes"`
	CreatedAt         time.Time  `db:"created_at" json:"created_at"`
}

func RegisterRoutes(r chi.Router, svc *Service) {
	r.Post("/reports", svc.CreateReport)

	// Moderator/admin routes
	r.Group(func(r chi.Router) {
		r.Use(requireModOrAdmin)
		r.Get("/moderation/reports", svc.ListReports)
		r.Post("/moderation/reports/{reportID}/review", svc.ReviewReport)
		r.Post("/moderation/users/{userID}/suspend", svc.SuspendUser)
		r.Post("/moderation/users/{userID}/unsuspend", svc.UnsuspendUser)
		r.Delete("/moderation/posts/{postID}", svc.DeletePost)
	})
}

func requireModOrAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := middleware.GetUserRole(r.Context())
		if role != "admin" && role != "moderator" {
			utils.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) CreateReport(w http.ResponseWriter, r *http.Request) {
	reporterID := middleware.GetUserID(r.Context())

	var req struct {
		ReportedUserID    *string `json:"reported_user_id"`
		ReportedPostID    *string `json:"reported_post_id"`
		ReportedCommentID *string `json:"reported_comment_id"`
		Reason            string  `json:"reason"`
		Details           string  `json:"details"`
	}
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.Reason == "" {
		utils.Error(w, http.StatusBadRequest, "reason required")
		return
	}

	_, err := s.db.Exec(`
		INSERT INTO reports (reporter_id, reported_user_id, reported_post_id, reported_comment_id, reason, details)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, reporterID, req.ReportedUserID, req.ReportedPostID, req.ReportedCommentID, req.Reason, req.Details)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "failed to submit report")
		return
	}

	utils.JSON(w, http.StatusCreated, map[string]string{"message": "report submitted"})
}

func (s *Service) ListReports(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}

	var reports []struct {
		Report
		ReporterUsername    string  `db:"reporter_username" json:"reporter_username"`
		ReportedUsername    *string `db:"reported_username" json:"reported_username,omitempty"`
	}

	s.db.Select(&reports, `
		SELECT r.*,
			u1.username as reporter_username,
			u2.username as reported_username
		FROM reports r
		JOIN users u1 ON r.reporter_id = u1.id
		LEFT JOIN users u2 ON r.reported_user_id = u2.id
		WHERE r.status = $1
		ORDER BY r.created_at ASC
	`, status)

	if reports == nil {
		reports = []struct {
			Report
			ReporterUsername string  `db:"reporter_username" json:"reporter_username"`
			ReportedUsername *string `db:"reported_username" json:"reported_username,omitempty"`
		}{}
	}

	utils.JSON(w, http.StatusOK, reports)
}

func (s *Service) ReviewReport(w http.ResponseWriter, r *http.Request) {
	reviewerID := middleware.GetUserID(r.Context())
	reportID := chi.URLParam(r, "reportID")

	var req struct {
		Status string `json:"status"` // reviewed, dismissed, actioned
		Notes  string `json:"notes"`
	}
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	s.db.Exec(`
		UPDATE reports SET status = $1, review_notes = $2, reviewed_by = $3, reviewed_at = NOW()
		WHERE id = $4
	`, req.Status, req.Notes, reviewerID, reportID)

	// Log moderation action
	s.db.Exec(`
		INSERT INTO audit_log (actor_id, action, target_type, target_id)
		VALUES ($1, 'review_report', 'report', $2)
	`, reviewerID, reportID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "report reviewed"})
}

func (s *Service) SuspendUser(w http.ResponseWriter, r *http.Request) {
	moderatorID := middleware.GetUserID(r.Context())
	targetUserID := chi.URLParam(r, "userID")

	var req struct {
		Reason string `json:"reason"`
	}
	utils.DecodeJSON(r, &req)

	s.db.Exec(`UPDATE users SET is_suspended = true, suspension_reason = $1 WHERE id = $2`, req.Reason, targetUserID)

	s.db.Exec(`
		INSERT INTO audit_log (actor_id, action, target_type, target_id, details)
		VALUES ($1, 'suspend_user', 'user', $2, $3)
	`, moderatorID, targetUserID, map[string]string{"reason": req.Reason})

	// Email notification
	var userEmail, username string
	s.db.Get(&userEmail, "SELECT email FROM users WHERE id = $1", targetUserID)
	s.db.Get(&username, "SELECT username FROM users WHERE id = $1", targetUserID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "user suspended"})
}

func (s *Service) UnsuspendUser(w http.ResponseWriter, r *http.Request) {
	moderatorID := middleware.GetUserID(r.Context())
	targetUserID := chi.URLParam(r, "userID")

	s.db.Exec(`UPDATE users SET is_suspended = false, suspension_reason = '' WHERE id = $1`, targetUserID)
	s.db.Exec(`
		INSERT INTO audit_log (actor_id, action, target_type, target_id)
		VALUES ($1, 'unsuspend_user', 'user', $2)
	`, moderatorID, targetUserID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "user unsuspended"})
}

func (s *Service) DeletePost(w http.ResponseWriter, r *http.Request) {
	moderatorID := middleware.GetUserID(r.Context())
	postID := chi.URLParam(r, "postID")

	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, postID)
	s.db.Exec(`
		INSERT INTO audit_log (actor_id, action, target_type, target_id)
		VALUES ($1, 'delete_post', 'post', $2)
	`, moderatorID, postID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "post deleted"})
}
