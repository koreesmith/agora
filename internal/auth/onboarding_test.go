package auth

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-387: /auth/me tells the client whether to send the account to the
// getting started wizard, and reports the saved dm_privacy (which it never
// used to, so a fresh sign-in always looked like "Everyone").
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestMeReportsOnboardingAndDmPrivacy(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	// Registered first so it runs last, after the row cleanups below.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := &Service{db: db, cfg: &config.Config{JWTSecret: "agora387-test-secret"}}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var id string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, email_verified, dm_privacy)
		VALUES ($1, $2, 'x', true, 'friends')
		RETURNING id
	`, "agora387me"+suffix, "agora387me"+suffix+"@example.com").Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

	token, err := s.makeToken(id, "user")
	if err != nil {
		t.Fatalf("make token: %v", err)
	}
	me := func() map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", "/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.Me(w, req)
		if w.Code != 200 {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body
	}

	body := me()
	if body["onboarding_completed"] != false {
		t.Errorf("onboarding_completed = %v for a new account, want false", body["onboarding_completed"])
	}
	if body["dm_privacy"] != "friends" {
		t.Errorf("dm_privacy = %v, want \"friends\"", body["dm_privacy"])
	}

	db.Exec(`UPDATE users SET onboarding_completed_at = NOW() WHERE id = $1`, id)
	if got := me()["onboarding_completed"]; got != true {
		t.Errorf("onboarding_completed = %v after completing, want true", got)
	}
}

// AGORA-387: /api/instance carries the two network switches with the same
// defaults the federation and atproto packages enforce, plus the bare domain.
// Read-only: it never changes instance_settings, which other tests share.
func TestInstanceInfoReportsNetworkSwitchesAndDomain(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	// Registered first so it runs last, after the row cleanups below.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora.example/"}}

	w := httptest.NewRecorder()
	s.InstanceInfo(w, httptest.NewRequest("GET", "/instance", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body["domain"] != "agora.example" {
		t.Errorf("domain = %v, want agora.example", body["domain"])
	}
	if got, want := body["activitypub_enabled"], s.getSetting("activitypub_enabled") != "false"; got != want {
		t.Errorf("activitypub_enabled = %v, want %v", got, want)
	}
	if got, want := body["atproto_enabled"], s.getSetting("atproto_enabled") == "true"; got != want {
		t.Errorf("atproto_enabled = %v, want %v", got, want)
	}
	// The existing string fields keep their type for older clients.
	if _, ok := body["instance_name"].(string); !ok {
		t.Errorf("instance_name = %#v, want a string", body["instance_name"])
	}
}
