package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-313: handleInboundFollowUser notifies the followed account, and the
// thing standing between that and a duplicate notification per delivery is
// recordAPFollower's xmax check. A Follow is not delivered once: Mastodon
// retries on its own schedule, and a refollow after a processed unfollow
// arrives at the same upsert. Only the delivery that actually creates the row
// may report itself as new.
//
// The full handler isn't reachable from a test (fedHTTPClient refuses to dial
// loopback, so the signed actor fetch it starts with can't be stubbed), which
// is why the upsert is its own seam.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestRecordAPFollowerReportsOnlyTheFirstDelivery(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	// Registered rather than deferred, and registered first so it runs last: a
	// deferred Close fires before any t.Cleanup, leaving the row cleanup below
	// to no-op against a closed pool and strand it in the shared test DB.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	unique := time.Now().UnixNano()
	username := fmt.Sprintf("agora313_followed_%d", unique)
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	actor := fmt.Sprintf("https://mastodon.example/users/follower%d", unique)
	inbox := actor + "/inbox"

	inserted, err := s.recordAPFollower(userID, actor, inbox)
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if !inserted {
		t.Fatal("first delivery reported the follower as already known, so no notification would ever be sent")
	}

	// A redelivery of the same Follow, with the inbox URL moved, as happens
	// when a remote instance migrates its inbox between retries.
	movedInbox := actor + "/inbox-v2"
	inserted, err = s.recordAPFollower(userID, actor, movedInbox)
	if err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if inserted {
		t.Fatal("redelivered Follow reported as a new follower, which would notify once per delivery")
	}

	var gotInbox string
	var rows int
	db.QueryRow(`SELECT COUNT(*) FROM ap_followers WHERE followed_user_id = $1 AND follower_actor_url = $2`,
		userID, actor).Scan(&rows)
	db.QueryRow(`SELECT follower_inbox_url FROM ap_followers WHERE followed_user_id = $1 AND follower_actor_url = $2`,
		userID, actor).Scan(&gotInbox)
	if rows != 1 {
		t.Fatalf("ap_followers rows = %d, want 1", rows)
	}
	if gotInbox != movedInbox {
		t.Fatalf("follower_inbox_url = %q, want the redelivery's %q: the update half of the upsert still has to apply", gotInbox, movedInbox)
	}

	// A different follower of the same account is still new.
	otherActor := fmt.Sprintf("https://mastodon.example/users/other%d", unique)
	inserted, err = s.recordAPFollower(userID, otherActor, otherActor+"/inbox")
	if err != nil {
		t.Fatalf("second follower: %v", err)
	}
	if !inserted {
		t.Fatal("a different follower of the same account was reported as already known")
	}
}
