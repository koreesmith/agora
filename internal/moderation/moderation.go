package moderation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	r.Post("/reports", s.CreateReport)

	r.Get("/moderation/reports",                     s.ListReports)
	r.Post("/moderation/reports/{id}/review",        s.ReviewReport)
	r.Get("/moderation/users",                       s.ListModeratedUsers)
	r.Post("/moderation/users/{userID}/suspend",     s.SuspendUser)
	r.Post("/moderation/users/{userID}/unsuspend",   s.UnsuspendUser)
	r.Post("/moderation/users/{userID}/ban",         s.BanUser)
	r.Post("/moderation/users/{userID}/unban",       s.UnbanUser)
	r.Get("/moderation/instance-bans",               s.ListInstanceBans)
	r.Post("/moderation/instance-bans",              s.BanInstance)
	r.Delete("/moderation/instance-bans/{id}",       s.UnbanInstance)
}

// ── Reports ───────────────────────────────────────────────────────────────────

func (s *Service) CreateReport(w http.ResponseWriter, r *http.Request) {
	reporterID := auth.UserIDFromCtx(r.Context())
	var req struct {
		ReportedUserID    string `json:"reported_user_id"`
		ReportedPostID    string `json:"reported_post_id"`
		ReportedCommentID string `json:"reported_comment_id"`
		ReportedPageID    string `json:"reported_page_id"`
		ViolationType     string `json:"violation_type"`
		RuleID            string `json:"rule_id"`
		Reason            string `json:"reason"`
		Details           string `json:"details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}
	// Allow either violation_type or reason as the report category (pages use reason)
	if req.ViolationType == "" && req.Reason != "" {
		req.ViolationType = req.Reason
	}
	if req.ViolationType == "" {
		writeError(w, 400, "violation_type required"); return
	}
	if req.ReportedUserID == "" && req.ReportedPostID == "" && req.ReportedCommentID == "" && req.ReportedPageID == "" {
		writeError(w, 400, "must report a user, post, comment, or page"); return
	}

	var userID, postID, commentID, pageID, ruleID *string
	if req.ReportedUserID != ""    { userID    = &req.ReportedUserID }
	if req.ReportedPostID != ""    { postID    = &req.ReportedPostID }
	if req.ReportedCommentID != "" { commentID = &req.ReportedCommentID }
	if req.ReportedPageID != ""    { pageID    = &req.ReportedPageID }
	if req.RuleID != ""            { ruleID    = &req.RuleID }

	var id string
	err := s.db.QueryRow(`
		INSERT INTO reports (reporter_id, reported_user_id, reported_post_id, reported_comment_id,
		                     reported_page_id, violation_type, rule_id, details, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $6) RETURNING id
	`, reporterID, userID, postID, commentID, pageID, req.ViolationType, ruleID, req.Details).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not submit report"); return
	}

	// Notify all admins
	go s.notifyAdmins(reporterID, id, req.ViolationType)

	writeJSON(w, 201, map[string]string{"id": id, "message": "report submitted"})
}

func (s *Service) notifyAdmins(reporterID, reportID, violationType string) {
	rows, err := s.db.Query(`SELECT id FROM users WHERE role IN ('admin','moderator') AND deletion_scheduled_at IS NULL`)
	if err != nil { return }
	defer rows.Close()
	for rows.Next() {
		var adminID string
		rows.Scan(&adminID)
		if adminID == reporterID { continue }
		// Pass report ID in data field; postID left empty so it doesn't route to /post/:id
		s.notif.Create(adminID, reporterID, "new_report", "", reportID)
	}
}

func (s *Service) ListReports(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" { status = "pending" }

	rows, err := s.db.Query(`
		SELECT r.id, r.violation_type, r.details, r.status, r.created_at, r.review_notes,
		       reporter.username AS reporter_username,
		       COALESCE(ru.username, pa.username) AS reported_user_username,
		       COALESCE(ru.id, pa.id)             AS reported_user_id,
		       COALESCE(ru.is_suspended, pa.is_suspended, false) AS is_suspended,
		       COALESCE(ru.banned_at, pa.banned_at) AS banned_at,
		       COALESCE(ru.is_remote, pa.is_remote, false) AS is_remote,
		       COALESCE(ru.remote_instance, pa.remote_instance, '') AS remote_instance,
		       r.reported_post_id, r.reported_comment_id,
		       p.content AS post_content,
		       r.rule_id, ir.text AS rule_text,
		       r.reviewed_by, reviewer.username AS reviewer_username, r.reviewed_at
		FROM reports r
		JOIN users reporter ON reporter.id = r.reporter_id
		LEFT JOIN users ru  ON ru.id = r.reported_user_id
		LEFT JOIN posts p   ON p.id = COALESCE(r.reported_post_id, r.reported_comment_id)
		LEFT JOIN users pa  ON pa.id = p.author_id AND ru.id IS NULL
		LEFT JOIN instance_rules ir ON ir.id = r.rule_id
		LEFT JOIN users reviewer ON reviewer.id = r.reviewed_by
		WHERE r.status = $1
		ORDER BY r.created_at DESC
		LIMIT 100
	`, status)
	if err != nil { writeError(w, 500, "db error"); return }
	defer rows.Close()

	type Report struct {
		ID                   string  `json:"id"`
		ViolationType        string  `json:"violation_type"`
		Details              string  `json:"details"`
		Status               string  `json:"status"`
		CreatedAt            string  `json:"created_at"`
		ReviewNotes          string  `json:"review_notes"`
		ReporterUsername     string  `json:"reporter_username"`
		ReportedUserUsername *string `json:"reported_user_username"`
		ReportedUserID       *string `json:"reported_user_id"`
		IsSuspended          bool    `json:"is_suspended"`
		IsBanned             bool    `json:"is_banned"`
		IsRemote             bool    `json:"is_remote"`
		RemoteInstance       string  `json:"remote_instance"`
		ReportedPostID       *string `json:"reported_post_id"`
		ReportedCommentID    *string `json:"reported_comment_id"`
		PostContent          *string `json:"post_content"`
		RuleID               *string `json:"rule_id"`
		RuleText             *string `json:"rule_text"`
		ReviewedBy           *string `json:"reviewed_by"`
		ReviewerUsername     *string `json:"reviewer_username"`
		ReviewedAt           *string `json:"reviewed_at"`
	}

	var reports []Report
	for rows.Next() {
		var rp Report
		var bannedAt *string
		rows.Scan(&rp.ID, &rp.ViolationType, &rp.Details, &rp.Status, &rp.CreatedAt, &rp.ReviewNotes,
			&rp.ReporterUsername, &rp.ReportedUserUsername, &rp.ReportedUserID, &rp.IsSuspended, &bannedAt,
			&rp.IsRemote, &rp.RemoteInstance,
			&rp.ReportedPostID, &rp.ReportedCommentID, &rp.PostContent,
			&rp.RuleID, &rp.RuleText,
			&rp.ReviewedBy, &rp.ReviewerUsername, &rp.ReviewedAt)
		rp.IsBanned = bannedAt != nil
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
		writeError(w, 400, "invalid json"); return
	}
	s.db.Exec(`
		UPDATE reports SET status = $1, review_notes = $2, reviewed_by = $3, reviewed_at = NOW()
		WHERE id = $4
	`, req.Action, req.Notes, reviewerID, id)
	writeJSON(w, 200, map[string]string{"message": "report reviewed"})
}

// ── User moderation ───────────────────────────────────────────────────────────

func (s *Service) ListModeratedUsers(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter") // suspended | banned | all
	var where string
	switch filter {
	case "suspended":
		where = "WHERE is_suspended = true AND deletion_scheduled_at IS NULL"
	case "banned":
		where = "WHERE banned_at IS NOT NULL AND deletion_scheduled_at IS NULL"
	default:
		where = "WHERE (is_suspended = true OR banned_at IS NOT NULL) AND deletion_scheduled_at IS NULL"
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT id, username, display_name, email, role,
		       is_suspended, suspension_reason, suspension_notes, suspension_expires_at,
		       banned_at, ban_reason, ban_notes,
		       is_remote, remote_instance
		FROM users %s
		ORDER BY COALESCE(banned_at, NOW()) DESC, COALESCE(suspension_expires_at, NOW()) DESC
		LIMIT 100
	`, where))
	if err != nil { writeError(w, 500, "db error"); return }
	defer rows.Close()

	type User struct {
		ID                  string  `json:"id"`
		Username            string  `json:"username"`
		DisplayName         string  `json:"display_name"`
		Email               string  `json:"email"`
		Role                string  `json:"role"`
		IsSuspended         bool    `json:"is_suspended"`
		SuspensionReason    string  `json:"suspension_reason"`
		SuspensionNotes     string  `json:"suspension_notes"`
		SuspensionExpiresAt *string `json:"suspension_expires_at"`
		BannedAt            *string `json:"banned_at"`
		BanReason           string  `json:"ban_reason"`
		BanNotes            string  `json:"ban_notes"`
		IsRemote            bool    `json:"is_remote"`
		RemoteInstance      string  `json:"remote_instance"`
	}
	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Role,
			&u.IsSuspended, &u.SuspensionReason, &u.SuspensionNotes, &u.SuspensionExpiresAt,
			&u.BannedAt, &u.BanReason, &u.BanNotes,
			&u.IsRemote, &u.RemoteInstance)
		users = append(users, u)
	}
	if users == nil { users = []User{} }
	writeJSON(w, 200, map[string]any{"users": users})
}

func (s *Service) SuspendUser(w http.ResponseWriter, r *http.Request) {
	adminID := auth.UserIDFromCtx(r.Context())
	userID := chi.URLParam(r, "userID")
	var req struct {
		Reason  string `json:"reason"`
		Notes   string `json:"notes"`
		Days    int    `json:"days"` // 0 = indefinite
	}
	json.NewDecoder(r.Body).Decode(&req)

	var expiresAt *time.Time
	if req.Days > 0 {
		t := time.Now().AddDate(0, 0, req.Days)
		expiresAt = &t
	}

	s.db.Exec(`
		UPDATE users SET is_suspended = true, suspension_reason = $1,
		                 suspension_notes = $2, suspension_expires_at = $3
		WHERE id = $4
	`, req.Reason, req.Notes, expiresAt, userID)

	// Log action
	s.db.Exec(`
		UPDATE reports SET review_notes = CONCAT(review_notes, $1), reviewed_by = $2, reviewed_at = NOW()
		WHERE reported_user_id = $3 AND status = 'pending'
	`, fmt.Sprintf("\n[Suspended by admin %s]", adminID), adminID, userID)

	go s.notif.SendModerationAction(userID, "suspended", req.Reason)
	writeJSON(w, 200, map[string]string{"message": "user suspended"})
}

func (s *Service) UnsuspendUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	s.db.Exec(`
		UPDATE users SET is_suspended = false, suspension_reason = '',
		                 suspension_notes = '', suspension_expires_at = NULL
		WHERE id = $1
	`, userID)
	writeJSON(w, 200, map[string]string{"message": "user unsuspended"})
}

func (s *Service) BanUser(w http.ResponseWriter, r *http.Request) {
	adminID := auth.UserIDFromCtx(r.Context())
	userID := chi.URLParam(r, "userID")
	var req struct {
		Reason string `json:"reason"`
		Notes  string `json:"notes"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	s.db.Exec(`
		UPDATE users SET banned_at = NOW(), ban_reason = $1, ban_notes = $2,
		                 is_suspended = false, suspension_expires_at = NULL
		WHERE id = $3
	`, req.Reason, req.Notes, userID)

	s.db.Exec(`
		UPDATE reports SET status = 'actioned', review_notes = CONCAT(review_notes, $1),
		                   reviewed_by = $2, reviewed_at = NOW()
		WHERE reported_user_id = $3 AND status = 'pending'
	`, fmt.Sprintf("\n[Banned by admin %s]", adminID), adminID, userID)

	go s.notif.SendModerationAction(userID, "banned", req.Reason)
	writeJSON(w, 200, map[string]string{"message": "user banned"})
}

func (s *Service) UnbanUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	s.db.Exec(`UPDATE users SET banned_at = NULL, ban_reason = '', ban_notes = '' WHERE id = $1`, userID)
	writeJSON(w, 200, map[string]string{"message": "user unbanned"})
}

// ── Instance bans ─────────────────────────────────────────────────────────────

func (s *Service) ListInstanceBans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT b.id, b.instance, b.reason, b.notes, b.created_at, u.username AS banned_by
		FROM instance_bans b
		LEFT JOIN users u ON u.id = b.banned_by
		ORDER BY b.created_at DESC
	`)
	if err != nil { writeError(w, 500, "db error"); return }
	defer rows.Close()

	type Ban struct {
		ID        string  `json:"id"`
		Instance  string  `json:"instance"`
		Reason    string  `json:"reason"`
		Notes     string  `json:"notes"`
		CreatedAt string  `json:"created_at"`
		BannedBy  *string `json:"banned_by"`
	}
	var bans []Ban
	for rows.Next() {
		var b Ban
		rows.Scan(&b.ID, &b.Instance, &b.Reason, &b.Notes, &b.CreatedAt, &b.BannedBy)
		bans = append(bans, b)
	}
	if bans == nil { bans = []Ban{} }
	writeJSON(w, 200, map[string]any{"bans": bans})
}

func (s *Service) BanInstance(w http.ResponseWriter, r *http.Request) {
	adminID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Instance string `json:"instance"`
		Reason   string `json:"reason"`
		Notes    string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Instance) == "" {
		writeError(w, 400, "instance required"); return
	}
	var id string
	s.db.QueryRow(`
		INSERT INTO instance_bans (instance, reason, notes, banned_by)
		VALUES ($1, $2, $3, $4) ON CONFLICT (instance) DO UPDATE
		SET reason = $2, notes = $3, banned_by = $4
		RETURNING id
	`, strings.TrimSpace(req.Instance), req.Reason, req.Notes, adminID).Scan(&id)
	writeJSON(w, 201, map[string]string{"id": id, "message": "instance banned"})
}

func (s *Service) UnbanInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM instance_bans WHERE id = $1`, id)
	writeJSON(w, 200, map[string]string{"message": "instance unbanned"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
