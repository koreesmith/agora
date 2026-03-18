package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db       *store.DB
	cfg      *config.Config
	notifSvc *notifications.Service
}

func NewService(db *store.DB, cfg *config.Config, notifSvc *notifications.Service) *Service {
	return &Service{db: db, cfg: cfg, notifSvc: notifSvc}
}

// ── Middleware ────────────────────────────────────────────────────────────────

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Support ?token= for WebSocket connections (can't set headers)
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "missing token"); return
			}
			tokenStr = strings.TrimPrefix(header, "Bearer ")
		}
		claims, err := s.parseToken(tokenStr)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token"); return
		}
		ctx := context.WithValue(r.Context(), ctxkeys.UserID, claims.UserID)
		ctx  = context.WithValue(ctx, ctxkeys.UserRole, claims.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := ctxkeys.GetUserRole(r.Context())
		if role != "admin" && role != "moderator" {
			writeError(w, http.StatusForbidden, "admin required"); return
		}
		next.ServeHTTP(w, r)
	})
}

func UserIDFromCtx(ctx context.Context) string { return ctxkeys.GetUserID(ctx) }
func RoleFromCtx(ctx context.Context) string     { return ctxkeys.GetUserRole(ctx) }

// ── Routes ────────────────────────────────────────────────────────────────────

func RegisterPublicRoutes(r chi.Router, s *Service) {
	r.Get("/setup",                 s.SetupStatus)
	r.Post("/setup",                s.RunSetup)
	r.Post("/auth/register",        s.Register)
	r.Post("/auth/login",           s.Login)
	r.Get("/auth/verify-email",     s.VerifyEmail)
	r.Post("/auth/forgot-password", s.ForgotPassword)
	r.Post("/auth/reset-password",  s.ResetPassword)
	r.Get("/auth/me",               s.Me)
}

// ── Setup (first-run) ─────────────────────────────────────────────────────────

func (s *Service) SetupStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := s.db.NeedsSetup()
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	writeJSON(w, 200, map[string]bool{"needs_setup": needed})
}

func (s *Service) RunSetup(w http.ResponseWriter, r *http.Request) {
	needed, err := s.db.NeedsSetup()
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	if !needed {
		writeError(w, 403, "setup already complete"); return
	}

	var req struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		InstanceName string `json:"instance_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}
	if len(req.Username) < 3 {
		writeError(w, 400, "username must be at least 3 characters"); return
	}
	if len(req.Password) < 8 {
		writeError(w, 400, "password must be at least 8 characters"); return
	}
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		writeError(w, 400, "valid email required"); return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, 500, "server error"); return
	}

	displayName := req.DisplayName
	if displayName == "" { displayName = req.Username }

	var userID string
	err = s.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, role, email_verified, profile_private)
		VALUES ($1, $2, $3, $4, 'admin', true, false)
		RETURNING id
	`, req.Username, strings.ToLower(req.Email), string(hash), displayName).Scan(&userID)
	if err != nil {
		writeError(w, 500, "could not create admin user"); return
	}

	// Set instance name if provided
	if req.InstanceName != "" {
		s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('instance_name', $1)
			ON CONFLICT (key) DO UPDATE SET value = $1`, req.InstanceName)
	}

	token, err := s.makeToken(userID, "admin")
	if err != nil {
		writeError(w, 500, "could not create session"); return
	}

	writeJSON(w, 201, map[string]any{
		"token":        token,
		"id":           userID,
		"username":     req.Username,
		"email":        strings.ToLower(req.Email),
		"display_name": displayName,
		"avatar_url":   "",
		"role":         "admin",
	})
}

// ── Register ──────────────────────────────────────────────────────────────────

func (s *Service) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		InviteCode  string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json"); return
	}
	if len(req.Username) < 3 || len(req.Username) > 50 {
		writeError(w, 400, "username must be 3–50 characters"); return
	}
	if !isValidUsername(req.Username) {
		writeError(w, 400, "username may only contain letters, numbers, underscores, and hyphens"); return
	}
	if len(req.Password) < 8 {
		writeError(w, 400, "password must be at least 8 characters"); return
	}
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		writeError(w, 400, "valid email required"); return
	}

	// Block if no admin yet
	needed, _ := s.db.NeedsSetup()
	if needed {
		writeError(w, 403, "instance setup not complete"); return
	}

	regMode := s.getSetting("registration_mode")
	if regMode == "closed" {
		writeError(w, 403, "registration is closed"); return
	}
	if regMode == "invite" {
		if req.InviteCode == "" {
			writeError(w, 403, "an invite code is required"); return
		}
		var inviteID string
		err := s.db.QueryRow(`
			SELECT id FROM invite_codes
			WHERE code = $1 AND used_by IS NULL
			  AND (expires_at IS NULL OR expires_at > NOW())
		`, req.InviteCode).Scan(&inviteID)
		if err != nil {
			writeError(w, 403, "invalid or expired invite code"); return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, 500, "server error"); return
	}

	verifyToken, _ := randomHex(32)
	verifyExpires := time.Now().Add(24 * time.Hour)
	displayName := req.DisplayName
	if displayName == "" { displayName = req.Username }

	var userID string
	err = s.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name,
		                   email_verify_token, email_verify_expires,
		                   unsubscribe_token)
		VALUES ($1, $2, $3, $4, $5, $6, replace(uuid_generate_v4()::text, '-', '') || replace(uuid_generate_v4()::text, '-', '')) RETURNING id
	`, req.Username, strings.ToLower(req.Email), string(hash), displayName,
		verifyToken, verifyExpires).Scan(&userID)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			if strings.Contains(err.Error(), "username") {
				writeError(w, 409, "username already taken")
			} else {
				writeError(w, 409, "email already registered")
			}
			return
		}
		writeError(w, 500, "could not create account"); return
	}

	if regMode == "invite" && req.InviteCode != "" {
		s.db.Exec(`UPDATE invite_codes SET used_by = $1, used_at = NOW() WHERE code = $2`, userID, req.InviteCode)
	}

	go s.notifSvc.SendEmailVerification(userID, req.Email, displayName, verifyToken)

	writeJSON(w, 201, map[string]string{"message": "account created — check your email to verify"})
}

// ── Login ─────────────────────────────────────────────────────────────────────

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UsernameOrEmail string `json:"username_or_email"`
		Password        string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}

	var u struct {
		ID             string
		Username       string
		Email          string
		PasswordHash   string
		DisplayName    string
		AvatarURL      string
		Role           string
		IsSuspended    bool
		SuspensionReason string
		EmailVerified  bool
		ProfilePrivate bool
	}

	login := strings.ToLower(strings.TrimSpace(req.UsernameOrEmail))
	err := s.db.QueryRow(`
		SELECT id, username, email, password_hash, display_name, avatar_url,
		       role, is_suspended, suspension_reason, email_verified, profile_private
		FROM users
		WHERE (LOWER(username) = $1 OR LOWER(email) = $1) AND is_remote = false
	`, login).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.AvatarURL, &u.Role, &u.IsSuspended, &u.SuspensionReason,
		&u.EmailVerified, &u.ProfilePrivate)
	if err != nil {
		writeError(w, 401, "invalid credentials"); return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, 401, "invalid credentials"); return
	}
	if u.IsSuspended {
		writeError(w, 403, "account suspended: "+u.SuspensionReason); return
	}
	if !u.EmailVerified {
		writeError(w, 403, "email not verified — check your inbox"); return
	}

	token, err := s.makeToken(u.ID, u.Role)
	if err != nil {
		writeError(w, 500, "could not create session"); return
	}

	writeJSON(w, 200, map[string]any{
		"token":           token,
		"id":              u.ID,
		"username":        u.Username,
		"email":           u.Email,
		"display_name":    u.DisplayName,
		"avatar_url":      u.AvatarURL,
		"role":            u.Role,
		"profile_private": u.ProfilePrivate,
	})
}

// ── Me ────────────────────────────────────────────────────────────────────────

func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeError(w, 401, "not authenticated"); return
	}
	claims, err := s.parseToken(strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		writeError(w, 401, "invalid token"); return
	}

	var u struct {
		ID             string
		Username       string
		Email          string
		DisplayName    string
		Pronouns       string
		Bio            string
		AvatarURL      string
		CoverURL             string
		CoverPosition        string
		Location             string
		Website              string
		Role                 string
		ProfilePrivate       bool
		WallApprovalRequired bool
	}
	err = s.db.QueryRow(`
		SELECT id, username, email, display_name, pronouns, bio, avatar_url, cover_url,
		       cover_position, location, website, role, profile_private, wall_approval_required
		FROM users WHERE id = $1
	`, claims.UserID).Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.Pronouns, &u.Bio,
		&u.AvatarURL, &u.CoverURL, &u.CoverPosition, &u.Location, &u.Website, &u.Role, &u.ProfilePrivate, &u.WallApprovalRequired)
	if err != nil {
		writeError(w, 401, "user not found"); return
	}

	writeJSON(w, 200, map[string]any{
		"id": u.ID, "username": u.Username, "email": u.Email,
		"display_name": u.DisplayName, "pronouns": u.Pronouns, "bio": u.Bio,
		"avatar_url": u.AvatarURL, "cover_url": u.CoverURL, "cover_position": u.CoverPosition,
		"location": u.Location, "website": u.Website,
		"role": u.Role, "profile_private": u.ProfilePrivate,
		"wall_approval_required": u.WallApprovalRequired,
	})
}

// ── Email verify / password reset ─────────────────────────────────────────────

func (s *Service) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" { writeError(w, 400, "missing token"); return }
	res, err := s.db.Exec(`
		UPDATE users SET email_verified = true, email_verify_token = '', email_verify_expires = NULL
		WHERE email_verify_token = $1 AND email_verify_expires > NOW() AND email_verified = false
	`, token)
	if err != nil { writeError(w, 500, "server error"); return }
	n, _ := res.RowsAffected()
	if n == 0 { writeError(w, 400, "invalid or expired verification token"); return }
	writeJSON(w, 200, map[string]string{"message": "email verified"})
}

func (s *Service) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email string `json:"email"` }
	json.NewDecoder(r.Body).Decode(&req)
	var userID, displayName string
	err := s.db.QueryRow(`SELECT id, display_name FROM users WHERE LOWER(email) = $1`, strings.ToLower(req.Email)).
		Scan(&userID, &displayName)
	if err != nil {
		writeJSON(w, 200, map[string]string{"message": "if that email is registered, a reset link has been sent"}); return
	}
	resetToken, _ := randomHex(32)
	s.db.Exec(`UPDATE users SET password_reset_token = $1, password_reset_expires = $2 WHERE id = $3`,
		resetToken, time.Now().Add(2*time.Hour), userID)
	go s.notifSvc.SendPasswordReset(userID, req.Email, displayName, resetToken)
	writeJSON(w, 200, map[string]string{"message": "if that email is registered, a reset link has been sent"})
}

func (s *Service) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if len(req.NewPassword) < 8 { writeError(w, 400, "password must be at least 8 characters"); return }
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	res, _ := s.db.Exec(`
		UPDATE users SET password_hash = $1, password_reset_token = '', password_reset_expires = NULL
		WHERE password_reset_token = $2 AND password_reset_expires > NOW()
	`, string(hash), req.Token)
	n, _ := res.RowsAffected()
	if n == 0 { writeError(w, 400, "invalid or expired reset token"); return }
	writeJSON(w, 200, map[string]string{"message": "password reset successfully"})
}

func (s *Service) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromCtx(r.Context())
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if len(req.NewPassword) < 8 { writeError(w, 400, "new password must be at least 8 characters"); return }
	var currentHash string
	s.db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&currentHash)
	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
		writeError(w, 401, "current password is incorrect"); return
	}
	newHash, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	s.db.Exec(`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, string(newHash), userID)
	writeJSON(w, 200, map[string]string{"message": "password changed"})
}

// ── JWT ───────────────────────────────────────────────────────────────────────

type claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func (s *Service) makeToken(userID, role string) (string, error) {
	c := claims{
		UserID: userID, Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(s.cfg.JWTSecret))
}

func (s *Service) parseToken(tokenStr string) (*claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil || !t.Valid { return nil, errors.New("invalid token") }
	c, ok := t.Claims.(*claims)
	if !ok { return nil, errors.New("invalid claims") }
	return c, nil
}

func (s *Service) getSetting(key string) string {
	var val string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = $1`, key).Scan(&val)
	return val
}

func isValidUsername(u string) bool {
	for _, c := range u {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil { return "", err }
	return hex.EncodeToString(b), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = sql.ErrNoRows

// ── Instance info (public) ────────────────────────────────────────────────────

func RegisterInstanceRoute(r chi.Router, s *Service) {
	r.Get("/instance",       s.InstanceInfo)
	r.Get("/instance/rules", s.InstanceRules)
	r.Get("/stats",          s.PublicStats)
}

func (s *Service) InstanceInfo(w http.ResponseWriter, r *http.Request) {
	keys := []string{"instance_name", "instance_description", "registration_mode", "logo_url"}
	info := map[string]string{}
	for _, k := range keys {
		info[k] = s.getSetting(k)
	}
	writeJSON(w, 200, info)
}

func (s *Service) InstanceRules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, position, text FROM instance_rules ORDER BY position ASC, created_at ASC`)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()
	type Rule struct {
		ID       string `json:"id"`
		Position int    `json:"position"`
		Text     string `json:"text"`
	}
	var rules []Rule
	for rows.Next() {
		var rule Rule
		rows.Scan(&rule.ID, &rule.Position, &rule.Text)
		rules = append(rules, rule)
	}
	if rules == nil { rules = []Rule{} }
	writeJSON(w, 200, map[string]any{"rules": rules})
}

func (s *Service) PublicStats(w http.ResponseWriter, r *http.Request) {
	var userCount, postCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE deletion_scheduled_at IS NULL`).Scan(&userCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL AND parent_id IS NULL`).Scan(&postCount)
	writeJSON(w, 200, map[string]any{
		"user_count": userCount,
		"post_count": postCount,
	})
}
