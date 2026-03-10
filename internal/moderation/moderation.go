package moderation

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db    *store.DB
	notif *notifications.Service
}

func NewService(db *store.DB, notif *notifications.Service) *Service {
	return &Service{db: db, notif: notif}
}

func RegisterRoutes(r chi.Router, s *Service) {
	// Any authenticated user can submit a report
	r.Post("/reports", s.CreateReport)

	// Moderator/admin routes
	r.Get("/moderation/reports",                     s.ListReports)
	r.Post("/moderation/reports/{id}/review",        s.ReviewReport)
	r.Post("/moderation/users/{userID}/suspend",     s.SuspendUser)
	r.Post("/moderation/users/{userID}/unsuspend",   s.UnsuspendUser)
}

func (s *Service) CreateReport(w http.ResponseWriter, r *http.Request) {
	reporterID := auth.UserIDFromCtx(r.Context())
	var req struct {
		ReportedUserID string `json:"reported_user_id"`
		ReportedPostID string `json:"reported_post_id"`
		Reason         string `json:"reason"`
		Details        string `json:"details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	if req.Reason == "" {
		writeError(w, 400, "reason required")
		return
	}
	if req.ReportedUserID == "" && req.ReportedPostID == "" {
		writeError(w, 400, "must report a user or a post")
		return
	}

	var userID, postID *string
	if req.ReportedUserID != "" { userID = &req.ReportedUserID }
	if req.ReportedPostID != "" { postID = &req.ReportedPostID }

	var id string
	err := s.db.QueryRow(`
		INSERT INTO reports (reporter_id, reported_user_id, reported_post_id, reason, details)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, reporterID, userID, postID, req.Reason, req.Details).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not submit report")
		return
	}
	writeJSON(w, 201, map[string]string{"id": id, "message": "report submitted"})
}

func (s *Service) ListReports(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" { status = "pending" }

	rows, err := s.db.Query(`
		SELECT r.id, r.reason, r.details, r.status, r.created_at, r.review_notes,
		       reporter.username AS reporter_username,
		       ru.username AS reported_user_username,
		       r.reported_post_id
		FROM reports r
		JOIN users reporter ON reporter.id = r.reporter_id
		LEFT JOIN users ru  ON ru.id = r.reported_user_id
		WHERE r.status = $1
		ORDER BY r.created_at DESC
		LIMIT 100
	`, status)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type Report struct {
		ID                    string  `json:"id"`
		Reason                string  `json:"reason"`
		Details               string  `json:"details"`
		Status                string  `json:"status"`
		CreatedAt             string  `json:"created_at"`
		ReviewNotes           string  `json:"review_notes"`
		ReporterUsername      string  `json:"reporter_username"`
		ReportedUserUsername  *string `json:"reported_user_username"`
		ReportedPostID        *string `json:"reported_post_id"`
	}

	var reports []Report
	for rows.Next() {
		var rp Report
		rows.Scan(&rp.ID, &rp.Reason, &rp.Details, &rp.Status, &rp.CreatedAt, &rp.ReviewNotes,
			&rp.ReporterUsername, &rp.ReportedUserUsername, &rp.ReportedPostID)
		reports = append(reports, rp)
	}
	if reports == nil { reports = []Report{} }
	writeJSON(w, 200, map[string]any{"reports": reports})
}

func (s *Service) ReviewReport(w http.ResponseWriter, r *http.Request) {
	reviewerID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req struct {
		Action string `json:"action"` // actioned | dismissed
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	s.db.Exec(`
		UPDATE reports SET status = $1, review_notes = $2,
		                   reviewed_by = $3, reviewed_at = NOW()
		WHERE id = $4
	`, req.Action, req.Notes, reviewerID, id)

	writeJSON(w, 200, map[string]string{"message": "report reviewed"})
}

func (s *Service) SuspendUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	var req struct{ Reason string `json:"reason"` }
	json.NewDecoder(r.Body).Decode(&req)

	s.db.Exec(`UPDATE users SET is_suspended = true, suspension_reason = $1 WHERE id = $2`, req.Reason, userID)

	go s.notif.SendModerationAction(userID, "suspended", req.Reason)

	writeJSON(w, 200, map[string]string{"message": "user suspended"})
}

func (s *Service) UnsuspendUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	s.db.Exec(`UPDATE users SET is_suspended = false, suspension_reason = '' WHERE id = $1`, userID)
	writeJSON(w, 200, map[string]string{"message": "user unsuspended"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
