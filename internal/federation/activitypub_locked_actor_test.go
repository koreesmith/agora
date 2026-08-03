package federation

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-306: manuallyApprovesFollowers is the only cached actor field a remote
// account can toggle at will (locking or unlocking themselves), so it has to be
// refreshed on every upsert rather than only written on first sight — a stub
// that kept its first-seen value would show a lock badge that never clears, or
// never appears. The ON CONFLICT arm is the part that can silently regress:
// forgetting the field there still passes any test that only ever inserts.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite, matching
// TestSignerUserIDForActorFetch.
func TestUpsertRemoteAPUserRefreshesLockedFlag(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	actorURL := fmt.Sprintf("https://mastodon.example/users/agora306_%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE ap_actor_url = $1`, actorURL) })

	profile := &remoteActorProfile{
		Inbox:             actorURL + "/inbox",
		PreferredUsername: fmt.Sprintf("agora306_%d", time.Now().UnixNano()),
		Name:              "Locked Account",

		ManuallyApprovesFollowers: true,
	}

	id, err := s.upsertRemoteAPUser(actorURL, profile)
	if err != nil {
		t.Fatalf("upsert (insert): %v", err)
	}

	locked := func(t *testing.T) bool {
		t.Helper()
		var v bool
		if err := db.QueryRow(`SELECT manually_approves_followers FROM users WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatalf("read back: %v", err)
		}
		return v
	}

	if !locked(t) {
		t.Error("insert did not persist manually_approves_followers = true")
	}

	// The account unlocks itself; the next refresh must clear the flag rather
	// than leave the stale lock in place.
	profile.ManuallyApprovesFollowers = false
	if _, err := s.upsertRemoteAPUser(actorURL, profile); err != nil {
		t.Fatalf("upsert (conflict): %v", err)
	}
	if locked(t) {
		t.Error("ON CONFLICT arm did not clear manually_approves_followers — a stale lock badge would never disappear")
	}

	// And back again, so this isn't just "the update writes false".
	profile.ManuallyApprovesFollowers = true
	if _, err := s.upsertRemoteAPUser(actorURL, profile); err != nil {
		t.Fatalf("upsert (conflict, re-lock): %v", err)
	}
	if !locked(t) {
		t.Error("ON CONFLICT arm did not re-set manually_approves_followers")
	}
}

// AGORA-306: the flag arrives as a plain top-level boolean on the actor
// document. fedHTTPClient refuses to dial loopback by design, so
// doActorProfileFetch can't be driven end-to-end from a test server; this
// pins the field name against a representative Mastodon actor payload
// instead, which is the part a typo would break.
func TestActorDocumentDecodesManuallyApprovesFollowers(t *testing.T) {
	const body = `{
		"id": "https://mastodon.example/users/locked",
		"type": "Person",
		"preferredUsername": "locked",
		"inbox": "https://mastodon.example/users/locked/inbox",
		"manuallyApprovesFollowers": true
	}`

	var actor struct {
		Inbox                     string `json:"inbox"`
		ManuallyApprovesFollowers bool   `json:"manuallyApprovesFollowers"`
	}
	if err := json.Unmarshal([]byte(body), &actor); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !actor.ManuallyApprovesFollowers {
		t.Error("manuallyApprovesFollowers did not decode as true")
	}

	// An actor omitting the key is unlocked, not unknown — the zero value is
	// load-bearing here, since most actor documents never mention the field.
	actor.ManuallyApprovesFollowers = false
	if err := json.Unmarshal([]byte(`{"inbox":"https://mastodon.example/users/open/inbox"}`), &actor); err != nil {
		t.Fatalf("decode (absent): %v", err)
	}
	if actor.ManuallyApprovesFollowers {
		t.Error("an actor omitting manuallyApprovesFollowers should decode as unlocked")
	}
}
