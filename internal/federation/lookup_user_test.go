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

// TestLookupUserStripsLeadingAt is AGORA-362: a fediverse handle is
// conventionally typed and displayed with a leading @ (@user@instance.com).
// LookupUser used to strings.SplitN the raw handle without stripping it
// first, so that whole natural format 400'd. The local-instance branch below
// is exercised here rather than the remote-WebFinger one, since it needs no
// network access and stripping the @ is the only thing under test either
// way: both branches split on the same raw string.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestLookupUserStripsLeadingAt(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// AGORA-363: federation_enabled off (the default) must not block this.
	// That setting is this instance's own opt-in to being discoverable by
	// other Agora instances, not a gate on this instance's own users looking
	// someone up.
	var prevFed string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'federation_enabled'`).Scan(&prevFed)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('federation_enabled', 'false')
		ON CONFLICT (key) DO UPDATE SET value = 'false'`)
	t.Cleanup(func() { db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'federation_enabled'`, prevFed) })

	username := fmt.Sprintf("agora362_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, display_name)
		VALUES ($1, $2, '', 'AGORA-362 Test User')
		RETURNING id
	`, username, username+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora362.example"}}

	req := httptest.NewRequest(http.MethodGet, "/federation/lookup?handle=@"+username+"@agora362.example", nil)
	w := httptest.NewRecorder()
	s.LookupUser(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Local bool `json:"local"`
		User  struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Local || resp.User.Username != username {
		t.Errorf("LookupUser(@%s@agora362.example) = %+v, want local=true username=%q", username, resp, username)
	}
}

// TestLookupUserRejectsBareAt covers the input the handle is supposed to
// refuse: an @ with nothing in front of it even after the leading-@ strip
// (someone typed "@@instance.com" or just "@instance.com" with no username),
// so the fix above doesn't quietly turn into "any leading @ characters are
// stripped" and start accepting garbage.
func TestLookupUserRejectsBareAt(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora362.example"}}

	for _, handle := range []string{"@instance.example", "@@instance.example", "@"} {
		req := httptest.NewRequest(http.MethodGet, "/federation/lookup?handle="+handle, nil)
		w := httptest.NewRecorder()
		s.LookupUser(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("LookupUser(%q) status = %d, want 400", handle, w.Code)
		}
	}
}
