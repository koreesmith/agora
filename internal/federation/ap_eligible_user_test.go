package federation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// TestApEligibleUserIgnoresPersonalActivityPubOptOut is AGORA-365: a user who
// turned off their own "Fediverse (ActivityPub)" setting used to become
// unresolvable over WebFinger/the actor document entirely, which also made
// them unfindable from a directly peered Agora instance, since Agora-to-Agora
// discovery resolves a handle through the exact same anonymous, unauthenticated
// WebFinger request a Mastodon server would use. There is no way to tell the
// two apart at that layer, so the fix drops the per-account opt-out from
// eligibility and leaves profile_private as the actual discoverability gate.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestApEligibleUserIgnoresPersonalActivityPubOptOut(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	username := fmt.Sprintf("agora365_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, profile_private, activitypub_enabled)
		VALUES ($1, $2, '', 'AGORA-365 Test User', false, false)
		RETURNING id
	`, username, username+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora365.example"}}

	if _, ok := s.apEligibleUser(username); !ok {
		t.Fatal("apEligibleUser rejected a public user solely for having activitypub_enabled = false, want it accepted")
	}

	// The actual user-facing symptom: WebFinger must resolve them now too.
	req := httptest.NewRequest(http.MethodGet,
		"/.well-known/webfinger?resource=acct:"+username+"@agora365.example", nil)
	w := httptest.NewRecorder()
	s.WebFinger(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("WebFinger status = %d, body = %s", w.Code, w.Body.String())
	}
	var jrd struct {
		Subject string `json:"subject"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &jrd); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "acct:" + username + "@agora365.example"
	if jrd.Subject != want {
		t.Errorf("WebFinger subject = %q, want %q", jrd.Subject, want)
	}
}

// TestApEligibleUserStillRespectsProfilePrivate confirms the fix above didn't
// quietly drop discoverability gating altogether: a private profile must
// stay unresolvable regardless of the activitypub_enabled column.
func TestApEligibleUserStillRespectsProfilePrivate(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()

	username := fmt.Sprintf("agora365_priv_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, profile_private, activitypub_enabled)
		VALUES ($1, $2, '', 'AGORA-365 Private Test User', true, true)
		RETURNING id
	`, username, username+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora365.example"}}

	if _, ok := s.apEligibleUser(username); ok {
		t.Error("apEligibleUser accepted a private profile, want it rejected regardless of activitypub_enabled")
	}
}
