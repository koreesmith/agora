package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// Following an unlocked fediverse account used to leave ap_following pending
// until the remote's Accept arrived and matched, so a lost Accept kept the
// follow on "Requested" forever and dropped every post they sent. Only a
// locked account should wait on approval now.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestRecordOutboundFollow(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	username := fmt.Sprintf("outfollow_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1`, userID)
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	})

	stored := func(t *testing.T, actorURL string) bool {
		t.Helper()
		var v bool
		if err := db.QueryRow(`SELECT accepted FROM ap_following WHERE follower_user_id = $1 AND followed_actor_url = $2`,
			userID, actorURL).Scan(&v); err != nil {
			t.Fatalf("read back: %v", err)
		}
		return v
	}
	follow := func(t *testing.T, actorURL string, locked bool) bool {
		t.Helper()
		got, err := s.recordOutboundFollow(userID, actorURL, actorURL+"/inbox", locked)
		if err != nil {
			t.Fatalf("recordOutboundFollow: %v", err)
		}
		if got != stored(t, actorURL) {
			t.Fatalf("returned accepted = %v but stored %v", got, !got)
		}
		return got
	}
	actor := func() string { return fmt.Sprintf("https://mastodon.example/users/of_%d", time.Now().UnixNano()) }

	t.Run("unlocked account is followed immediately", func(t *testing.T) {
		if !follow(t, actor(), false) {
			t.Error("follow of an unlocked account was left pending")
		}
	})

	t.Run("locked account waits for approval", func(t *testing.T) {
		if follow(t, actor(), true) {
			t.Error("follow of a locked account was accepted without an Accept")
		}
	})

	t.Run("refollowing a stuck pending row after unlock promotes it", func(t *testing.T) {
		a := actor()
		follow(t, a, true)
		if !follow(t, a, false) {
			t.Error("pending row stayed pending after the account unlocked")
		}
	})

	t.Run("refollowing never demotes an accepted follow", func(t *testing.T) {
		a := actor()
		follow(t, a, false)
		if !follow(t, a, true) {
			t.Error("accepted follow was demoted back to pending")
		}
	})
}
