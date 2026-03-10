package notifications

import (
	"fmt"
	"log"
	"strconv"

	"github.com/jmoiron/sqlx"
	"gopkg.in/gomail.v2"
)

type EmailService struct {
	db *sqlx.DB
}

func NewEmailService(db *sqlx.DB) *EmailService {
	return &EmailService{db: db}
}

type smtpConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	Enabled  bool
}

func (e *EmailService) getConfig() *smtpConfig {
	settings := make(map[string]string)
	rows, err := e.db.Queryx(`SELECT key, value FROM instance_settings WHERE key LIKE 'smtp_%'`)
	if err != nil {
		return &smtpConfig{Enabled: false}
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		settings[k] = v
	}

	port, _ := strconv.Atoi(settings["smtp_port"])
	if port == 0 {
		port = 587
	}

	return &smtpConfig{
		Host:     settings["smtp_host"],
		Port:     port,
		User:     settings["smtp_user"],
		Password: settings["smtp_password"],
		From:     settings["smtp_from"],
		Enabled:  settings["smtp_enabled"] == "true",
	}
}

func (e *EmailService) Send(to, subject, body string) error {
	cfg := e.getConfig()
	if !cfg.Enabled || cfg.Host == "" {
		log.Printf("Email not configured, skipping: to=%s subject=%s", to, subject)
		return nil
	}

	m := gomail.NewMessage()
	m.SetHeader("From", cfg.From)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body)

	d := gomail.NewDialer(cfg.Host, cfg.Port, cfg.User, cfg.Password)
	if err := d.DialAndSend(m); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
	return nil
}

func (e *EmailService) SendEmailVerification(to, username, token, instanceURL string) {
	verifyURL := fmt.Sprintf("%s/api/auth/verify-email?token=%s", instanceURL, token)
	body := fmt.Sprintf(`
		<h2>Welcome to Agora, %s!</h2>
		<p>Please verify your email address by clicking the link below:</p>
		<p><a href="%s">Verify Email Address</a></p>
		<p>This link expires in 24 hours.</p>
		<p>If you didn't create an account, you can safely ignore this email.</p>
	`, username, verifyURL)
	if err := e.Send(to, "Verify your Agora email address", body); err != nil {
		log.Printf("Failed to send verification email: %v", err)
	}
}

func (e *EmailService) SendPasswordReset(to, username, token, instanceURL string) {
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", instanceURL, token)
	body := fmt.Sprintf(`
		<h2>Password Reset Request</h2>
		<p>Hi %s, we received a request to reset your password.</p>
		<p><a href="%s">Reset Password</a></p>
		<p>This link expires in 1 hour.</p>
		<p>If you didn't request this, you can safely ignore this email.</p>
	`, username, resetURL)
	if err := e.Send(to, "Reset your Agora password", body); err != nil {
		log.Printf("Failed to send password reset email: %v", err)
	}
}

func (e *EmailService) SendFriendRequest(to, fromUsername string) {
	body := fmt.Sprintf(`
		<h2>New Friend Request</h2>
		<p>%s sent you a friend request on Agora.</p>
		<p>Log in to accept or decline.</p>
	`, fromUsername)
	e.Send(to, fmt.Sprintf("%s sent you a friend request", fromUsername), body)
}

func (e *EmailService) SendModerationAction(to, action, reason string) {
	body := fmt.Sprintf(`
		<h2>Moderation Action</h2>
		<p>A moderation action has been taken on your account: <strong>%s</strong></p>
		<p>Reason: %s</p>
		<p>If you believe this is a mistake, please contact the instance administrator.</p>
	`, action, reason)
	e.Send(to, "Moderation action on your Agora account", body)
}
