package atproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

// fakeAppView stands in for bsky.app's getFollowers. appviewClient() forces an
// https:// scheme, so this has to be a TLS server with relayHTTPClient swapped
// for one that trusts it. Both are package-level and restored on cleanup.
type fakeAppView struct {
	srv       *httptest.Server
	followers []map[string]any
}

func newFakeAppView(t *testing.T, db *store.DB) *fakeAppView {
	t.Helper()
	f := &fakeAppView{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xrpc/app.bsky.graph.getFollowers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"followers": f.followers})
	}))
	t.Cleanup(f.srv.Close)

	prevClient := relayHTTPClient
	relayHTTPClient = f.srv.Client()
	t.Cleanup(func() { relayHTTPClient = prevClient })

	var prevHost string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_appview_host'`).Scan(&prevHost)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_appview_host', $1)
	         ON CONFLICT (key) DO UPDATE SET value = $1`, f.srv.Listener.Addr().String())
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_appview_host'`, prevHost)
	})
	return f
}

func (f *fakeAppView) setFollowers(dids ...string) {
	f.followers = nil
	for _, did := range dids {
		f.followers = append(f.followers, map[string]any{
			"did":         did,
			"handle":      did[len("did:plc:"):] + ".bsky.social",
			"displayName": did[len("did:plc:"):],
		})
	}
}

// AGORA-313: a Bluesky follow is never delivered to this instance, so
// pollFollowersFor learns about one by diffing getFollowers against
// at_followers. The subtlety worth pinning down is the first walk: every
// follower it finds already existed before the feature shipped, so notifying
// for them would mean a notification per pre-existing follower on the first
// boot after deploy. Covers that seeding pass, the arrival that follows it,
// and the departure.
func TestPollFollowersSeedsSilentlyThenNotifies(t *testing.T) {
	db := testDB(t)
	// Registered rather than deferred, and registered first so it runs last:
	// a deferred Close fires before any t.Cleanup, leaving every cleanup below
	// to no-op against a closed pool and strand its rows in the shared test DB.
	t.Cleanup(func() { db.Close() })

	fake := newFakeAppView(t, db)

	unique := time.Now().UnixNano()
	username := fmt.Sprintf("agora313_local_%d", unique)
	did := "did:web:" + username
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did)
		VALUES ($1, $2, '', false, true, $3)
		RETURNING id
	`, username, username+"@example.invalid", did).Scan(&userID); err != nil {
		t.Fatalf("seed local user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	oldFollower := fmt.Sprintf("did:plc:agora313old%d", unique)
	newFollower := fmt.Sprintf("did:plc:agora313new%d", unique)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM users WHERE atproto_remote_did IN ($1, $2)`, oldFollower, newFollower)
	})
	// Registered last so it runs first (t.Cleanup is LIFO): the notification
	// rows reference both the local user and the follower stubs, and those
	// deletes are no-ops while it still points at them.
	t.Cleanup(func() { db.Exec(`DELETE FROM notifications WHERE user_id = $1`, userID) })

	s := &Service{
		db:    db,
		cfg:   &config.Config{InstanceDomain: "http://localhost:8080"},
		notif: notifications.NewService(db, notifications.NewEmailService(db, &config.Config{})),
	}

	countNotifs := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'atproto_follow'`, userID).Scan(&n)
		return n
	}
	seeded := func() bool {
		var b bool
		db.QueryRow(`SELECT atproto_followers_seeded FROM users WHERE id = $1`, userID).Scan(&b)
		return b
	}
	followerRows := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM at_followers WHERE local_user_id = $1`, userID).Scan(&n)
		return n
	}

	// First walk: one pre-existing follower, recorded but not announced.
	fake.setFollowers(oldFollower)
	s.pollFollowersFor(context.Background(), userID, did, false)

	if got := followerRows(); got != 1 {
		t.Fatalf("seeding walk: at_followers rows = %d, want 1", got)
	}
	if got := countNotifs(); got != 0 {
		t.Fatalf("seeding walk produced %d notifications, want 0: every follower it sees predates the feature", got)
	}
	if !seeded() {
		t.Fatal("seeding walk did not set atproto_followers_seeded, so the next walk would seed again and never notify")
	}

	// Second walk: the same follower is still there, plus a new one. Only the
	// arrival is announced.
	fake.setFollowers(oldFollower, newFollower)
	s.pollFollowersFor(context.Background(), userID, did, true)

	if got := followerRows(); got != 2 {
		t.Fatalf("after arrival: at_followers rows = %d, want 2", got)
	}
	if got := countNotifs(); got != 1 {
		t.Fatalf("after arrival: %d notifications, want exactly 1 (the new follower only)", got)
	}
	var actorDID string
	db.QueryRow(`
		SELECT u.atproto_remote_did FROM notifications n JOIN users u ON u.id = n.actor_id
		WHERE n.user_id = $1 AND n.type = 'atproto_follow'
	`, userID).Scan(&actorDID)
	if actorDID != newFollower {
		t.Fatalf("notification names %q, want the new follower %q", actorDID, newFollower)
	}

	// Re-polling an unchanged set must not re-announce anyone.
	s.pollFollowersFor(context.Background(), userID, did, true)
	if got := countNotifs(); got != 1 {
		t.Fatalf("re-poll of an unchanged follower set produced %d notifications, want the original 1", got)
	}

	// Departure: the row goes, and nothing is announced for it.
	fake.setFollowers(newFollower)
	s.pollFollowersFor(context.Background(), userID, did, true)

	if got := followerRows(); got != 1 {
		t.Fatalf("after departure: at_followers rows = %d, want 1", got)
	}
	if got := countNotifs(); got != 1 {
		t.Fatalf("departure changed the notification count to %d, want the original 1", got)
	}
}

// An account with no followers at all still has to be marked seeded, otherwise
// every later walk treats itself as the first one and its genuine first
// follower is filled in silently and never announced. This is why seeding is a
// per-user flag rather than an "is at_followers empty for them" test.
func TestPollFollowersMarksEmptyAccountSeeded(t *testing.T) {
	db := testDB(t)
	t.Cleanup(func() { db.Close() }) // see the note in the test above

	fake := newFakeAppView(t, db)
	fake.setFollowers()

	unique := time.Now().UnixNano()
	username := fmt.Sprintf("agora313_empty_%d", unique)
	did := "did:web:" + username
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did)
		VALUES ($1, $2, '', false, true, $3)
		RETURNING id
	`, username, username+"@example.invalid", did).Scan(&userID); err != nil {
		t.Fatalf("seed local user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	firstFollower := fmt.Sprintf("did:plc:agora313first%d", unique)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE atproto_remote_did = $1`, firstFollower) })
	// See the LIFO note in TestPollFollowersSeedsSilentlyThenNotifies.
	t.Cleanup(func() { db.Exec(`DELETE FROM notifications WHERE user_id = $1`, userID) })

	s := &Service{
		db:    db,
		cfg:   &config.Config{InstanceDomain: "http://localhost:8080"},
		notif: notifications.NewService(db, notifications.NewEmailService(db, &config.Config{})),
	}

	s.pollFollowersFor(context.Background(), userID, did, false)

	var isSeeded bool
	db.QueryRow(`SELECT atproto_followers_seeded FROM users WHERE id = $1`, userID).Scan(&isSeeded)
	if !isSeeded {
		t.Fatal("a walk that found no followers left the account unseeded, so its first real follower would never be announced")
	}

	fake.setFollowers(firstFollower)
	s.pollFollowersFor(context.Background(), userID, did, true)

	var n int
	db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'atproto_follow'`, userID).Scan(&n)
	if n != 1 {
		t.Fatalf("first follower of a previously-empty account produced %d notifications, want 1", n)
	}
}
