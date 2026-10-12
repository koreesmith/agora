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

func agora366TestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedAgora366User(t *testing.T, db *store.DB, prefix string, private bool) (id, username string) {
	t.Helper()
	username = fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name, bio, avatar_url, cover_url, profile_private, activitypub_enabled)
		VALUES ($1, $2, '', 'AGORA-366 Test', 'a bio nobody but friends should see', '/uploads/avatars/x.jpg', '/uploads/covers/x.jpg', $3, true)
		RETURNING id
	`, username, username+"@example.invalid", private).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return id, username
}

// TestApEligibleUserIdentityOnlyFindsPrivateProfile is AGORA-366's core
// change: apEligibleUser still excludes a private profile entirely (search
// and browsing must never surface one), but apEligibleUserIdentityOnly finds
// it, which is what lets an exact known handle resolve at all.
func TestApEligibleUserIdentityOnlyFindsPrivateProfile(t *testing.T) {
	db := agora366TestDB(t)
	_, username := seedAgora366User(t, db, "agora366_priv", true)

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora366.example"}}

	if _, ok := s.apEligibleUser(username); ok {
		t.Error("apEligibleUser found a private profile, want it excluded")
	}
	u, ok := s.apEligibleUserIdentityOnly(username)
	if !ok {
		t.Fatal("apEligibleUserIdentityOnly did not find a private profile, want it found")
	}
	if !u.Private {
		t.Error("apEligibleUserIdentityOnly did not report the profile as private")
	}
}

// TestWebFingerResolvesPrivateProfile is the actual user-facing symptom:
// acct:<private user>@<instance> must resolve instead of 404ing, the same
// way it did for winston@agorasocial.online before this fix.
func TestWebFingerResolvesPrivateProfile(t *testing.T) {
	db := agora366TestDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, username := seedAgora366User(t, db, "agora366_wf", true)

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora366.example"}}

	req := httptest.NewRequest(http.MethodGet,
		"/.well-known/webfinger?resource=acct:"+username+"@agora366.example", nil)
	w := httptest.NewRecorder()
	s.WebFinger(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("WebFinger status = %d, body = %s", w.Code, w.Body.String())
	}
}

// TestActorObjectRedactsPrivateProfile confirms the resolved actor document
// carries nothing beyond protocol plumbing for a private profile: no display
// name, no bio, no avatar, no cover, exactly the "just the username" scope
// this feature is meant to allow, no more.
func TestActorObjectRedactsPrivateProfile(t *testing.T) {
	db := agora366TestDB(t)
	_, username := seedAgora366User(t, db, "agora366_actor", true)

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora366.example"}}

	w := httptest.NewRecorder()
	s.writeActorObject(w, username)

	if w.Code != http.StatusOK {
		t.Fatalf("writeActorObject status = %d, body = %s", w.Code, w.Body.String())
	}
	var obj map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &obj); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, field := range []string{"name", "summary", "icon", "image"} {
		if _, present := obj[field]; present {
			t.Errorf("actor object for a private profile included %q, want it omitted", field)
		}
	}
	if obj["preferredUsername"] != username {
		t.Errorf("preferredUsername = %v, want %q", obj["preferredUsername"], username)
	}
	if obj["inbox"] == nil || obj["publicKey"] == nil {
		t.Error("actor object is missing protocol-required fields (inbox/publicKey) even for a private profile")
	}
}

// TestActorObjectIncludesContentForPublicProfile is the control: a public
// profile must keep getting its full actor object, unaffected by the
// redaction added for private ones.
func TestActorObjectIncludesContentForPublicProfile(t *testing.T) {
	db := agora366TestDB(t)
	_, username := seedAgora366User(t, db, "agora366_pub", false)

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora366.example"}}

	w := httptest.NewRecorder()
	s.writeActorObject(w, username)

	var obj map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &obj); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if obj["name"] != "AGORA-366 Test" {
		t.Errorf("name = %v, want the seeded display name for a public profile", obj["name"])
	}
	if obj["summary"] == "" || obj["summary"] == nil {
		t.Error("summary is empty for a public profile, want the seeded bio")
	}
	if obj["icon"] == nil {
		t.Error("icon is missing for a public profile with an avatar set")
	}
}
