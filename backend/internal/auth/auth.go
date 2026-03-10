package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/pkg/config"
	"github.com/agora-social/agora/pkg/middleware"
	"github.com/agora-social/agora/pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db      *sqlx.DB
	cfg     *config.Config
	notifSvc *notifications.Service
	jwt     *middleware.JWTMiddleware
}

func NewService(db *sqlx.DB, cfg *config.Config, notifSvc *notifications.Service) *Service {
	return &Service{
		db:      db,
		cfg:     cfg,
		notifSvc: notifSvc,
		jwt:     middleware.NewJWTMiddleware(cfg.JWTSecret),
	}
}

type RegisterRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	InviteCode  string `json:"invite_code"`
}

type LoginRequest struct {
	UsernameOrEmail string `json:"username_or_email"`
	Password        string `json:"password"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func RegisterRoutes(r chi.Router, svc *Service) {
	r.Post("/auth/register", svc.Register)
	r.Post("/auth/login", svc.Login)
	r.Get("/auth/verify-email", svc.VerifyEmail)
	r.Post("/auth/forgot-password", svc.ForgotPassword)
	r.Post("/auth/reset-password", svc.ResetPassword)

	r.Group(func(r chi.Router) {
		r.Use(middleware.NewJWTMiddleware(svc.cfg.JWTSecret).Authenticate)
		r.Post("/auth/change-password", svc.ChangePassword)
		r.Get("/auth/me", svc.Me)
	})
}

func (s *Service) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Validate
	if len(req.Username) < 3 || len(req.Username) > 50 {
		utils.Error(w, http.StatusBadRequest, "username must be between 3 and 50 characters")
		return
	}
	if len(req.Password) < 8 {
		utils.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	// Check admin default password lock
	var adminMustChange bool
	err := s.db.Get(&adminMustChange, "SELECT must_change_password FROM users WHERE username = 'admin'")
	if err == nil && adminMustChange {
		utils.Error(w, http.StatusForbidden, "registration is locked until the admin changes the default password")
		return
	}

	// Check registration mode
	var regMode string
	s.db.Get(&regMode, "SELECT value FROM instance_settings WHERE key = 'registration_mode'")

	if regMode == "closed" {
		utils.Error(w, http.StatusForbidden, "registration is closed on this instance")
		return
	}

	if regMode == "invite" {
		if req.InviteCode == "" {
			utils.Error(w, http.StatusForbidden, "an invite code is required to register")
			return
		}
		var inviteID string
		err := s.db.Get(&inviteID, `
			SELECT id FROM invite_codes 
			WHERE code = $1 AND used_by IS NULL 
			AND (expires_at IS NULL OR expires_at > NOW())
		`, req.InviteCode)
		if err != nil {
			utils.Error(w, http.StatusForbidden, "invalid or expired invite code")
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Username
	}

	var userID string
	err = s.db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, req.Username, req.Email, string(hash), displayName).Scan(&userID)
	if err != nil {
		utils.Error(w, http.StatusConflict, "username or email already taken")
		return
	}

	// Mark invite as used
	if regMode == "invite" && req.InviteCode != "" {
		s.db.Exec(`UPDATE invite_codes SET used_by = $1, used_at = NOW() WHERE code = $2`, userID, req.InviteCode)
	}

	// Send verification email
	token := generateToken()
	s.db.Exec(`
		INSERT INTO email_verification_tokens (user_id, token, expires_at)
		VALUES ($1, $2, $3)
	`, userID, token, time.Now().Add(24*time.Hour))

	go s.notifSvc.SendEmailVerification(req.Email, req.Username, token, s.cfg.InstanceURL)

	utils.JSON(w, http.StatusCreated, map[string]string{
		"message": "account created — please check your email to verify your account",
		"user_id": userID,
	})
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	var user struct {
		ID                string `db:"id"`
		Username          string `db:"username"`
		PasswordHash      string `db:"password_hash"`
		Role              string `db:"role"`
		EmailVerified     bool   `db:"email_verified"`
		IsSuspended       bool   `db:"is_suspended"`
		MustChangePassword bool  `db:"must_change_password"`
	}

	err := s.db.Get(&user, `
		SELECT id, username, password_hash, role, email_verified, is_suspended, must_change_password
		FROM users
		WHERE (username = $1 OR email = $1) AND is_remote = false
	`, req.UsernameOrEmail)
	if err != nil {
		utils.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		utils.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if !user.EmailVerified {
		utils.Error(w, http.StatusForbidden, "please verify your email before logging in")
		return
	}

	if user.IsSuspended {
		utils.Error(w, http.StatusForbidden, "your account has been suspended")
		return
	}

	token, err := s.jwt.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "could not generate token")
		return
	}

	utils.JSON(w, http.StatusOK, map[string]any{
		"token":                token,
		"user_id":              user.ID,
		"username":             user.Username,
		"role":                 user.Role,
		"must_change_password": user.MustChangePassword,
	})
}

func (s *Service) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		utils.Error(w, http.StatusBadRequest, "token required")
		return
	}

	var userID string
	err := s.db.Get(&userID, `
		SELECT user_id FROM email_verification_tokens
		WHERE token = $1 AND expires_at > NOW()
	`, token)
	if err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid or expired token")
		return
	}

	s.db.Exec(`UPDATE users SET email_verified = true WHERE id = $1`, userID)
	s.db.Exec(`DELETE FROM email_verification_tokens WHERE user_id = $1`, userID)

	// Redirect to frontend
	http.Redirect(w, r, s.cfg.InstanceURL+"/?verified=true", http.StatusFound)
}

func (s *Service) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email string `json:"email"` }
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	var user struct {
		ID       string `db:"id"`
		Username string `db:"username"`
	}
	err := s.db.Get(&user, "SELECT id, username FROM users WHERE email = $1", req.Email)
	if err != nil {
		// Don't reveal if email exists
		utils.JSON(w, http.StatusOK, map[string]string{"message": "if that email exists, a reset link has been sent"})
		return
	}

	token := generateToken()
	s.db.Exec(`
		INSERT INTO password_reset_tokens (user_id, token, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, user.ID, token, time.Now().Add(1*time.Hour))

	go s.notifSvc.SendPasswordReset(req.Email, user.Username, token, s.cfg.InstanceURL)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "if that email exists, a reset link has been sent"})
}

func (s *Service) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	if len(req.Password) < 8 {
		utils.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	var userID string
	err := s.db.Get(&userID, `
		SELECT user_id FROM password_reset_tokens
		WHERE token = $1 AND expires_at > NOW() AND used = false
	`, req.Token)
	if err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid or expired token")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		utils.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.db.Exec(`UPDATE users SET password_hash = $1, must_change_password = false WHERE id = $2`, string(hash), userID)
	s.db.Exec(`UPDATE password_reset_tokens SET used = true WHERE token = $1`, req.Token)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "password reset successfully"})
}

func (s *Service) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req ChangePasswordRequest
	if err := utils.DecodeJSON(r, &req); err != nil {
		utils.Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	if len(req.NewPassword) < 8 {
		utils.Error(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}

	var currentHash string
	s.db.Get(&currentHash, "SELECT password_hash FROM users WHERE id = $1", userID)

	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
		utils.Error(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	s.db.Exec(`UPDATE users SET password_hash = $1, must_change_password = false WHERE id = $2`, string(hash), userID)

	utils.JSON(w, http.StatusOK, map[string]string{"message": "password changed successfully"})
}

func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	var user struct {
		ID                 string    `db:"id" json:"id"`
		Username           string    `db:"username" json:"username"`
		Email              string    `db:"email" json:"email"`
		DisplayName        string    `db:"display_name" json:"display_name"`
		Bio                string    `db:"bio" json:"bio"`
		AvatarURL          string    `db:"avatar_url" json:"avatar_url"`
		CoverURL           string    `db:"cover_url" json:"cover_url"`
		Role               string    `db:"role" json:"role"`
		MustChangePassword bool      `db:"must_change_password" json:"must_change_password"`
		ProfilePrivate     bool      `db:"profile_private" json:"profile_private"`
		CreatedAt          time.Time `db:"created_at" json:"created_at"`
	}

	if err := s.db.Get(&user, `
		SELECT id, username, email, display_name, bio, avatar_url, cover_url, role, must_change_password, profile_private, created_at
		FROM users WHERE id = $1
	`, userID); err != nil {
		utils.Error(w, http.StatusNotFound, "user not found")
		return
	}

	utils.JSON(w, http.StatusOK, user)
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return fmt.Sprintf("%s", hex.EncodeToString(b))
}
