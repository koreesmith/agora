package store

import (
	"fmt"
	"testing"
	"time"
)

// AGORA-413: a new Fediverse or Bluesky follow shows in the main feed unless
// its owner turns it off. The follow paths insert without naming
// show_in_feed, so this inserts the same way and checks the column default.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestNewFollowsShowInFeedByDefault(t *testing.T) {
	db, err := Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil { t.Fatalf("migrate: %v", err) }

	n := time.Now().UnixNano() % 1e12
	var userID string
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		fmt.Sprintf("feed413_%d", n), fmt.Sprintf("feed413_%d@test.example", n)).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	var ap bool
	if err := db.QueryRow(`
		INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted)
		VALUES ($1, $2, $3, true) RETURNING show_in_feed
	`, userID, fmt.Sprintf("https://mastodon.example/users/feed413_%d", n), "https://mastodon.example/inbox").Scan(&ap); err != nil {
		t.Fatalf("insert ap_following: %v", err)
	}
	if !ap {
		t.Error("new Fediverse follow has show_in_feed = false")
	}

	var at bool
	if err := db.QueryRow(`
		INSERT INTO at_following (local_user_id, remote_did, remote_handle, display_name, avatar_url, rkey, record_cid)
		VALUES ($1, $2, 'feed413.bsky.social', '', '', 'rkey413', 'cid413') RETURNING show_in_feed
	`, userID, fmt.Sprintf("did:plc:feed413%d", n)).Scan(&at); err != nil {
		t.Fatalf("insert at_following: %v", err)
	}
	if !at {
		t.Error("new Bluesky follow has show_in_feed = false")
	}
}
