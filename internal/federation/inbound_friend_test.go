package federation

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-318: an inbound friend request over the legacy Agora-to-Agora protocol
// wrote the friendship row and told the recipient nothing, so the request only
// surfaced if they happened to open the Friends screen and look. The notify
// call is one line; what needs a test is the condition around it. A friend
// request is not delivered exactly once: the sending instance retries on its
// own schedule, so the notification has to be tied to the delivery that
// actually created the row, not to the arrival of an activity.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestInboundFriendRequestNotifiesOnceAndRespectsBlocks(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	// Registered first so it runs last: a deferred Close would fire before the
	// row cleanups below and strand them in the shared test database.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, notif: notifications.NewService(db, notifications.NewEmailService(db, &config.Config{}))}

	unique := time.Now().UnixNano()
	instance := fmt.Sprintf("agora318-%d.example", unique)

	localName := fmt.Sprintf("agora318_local_%d", unique)
	var localID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id
	`, localName, localName+"@example.com").Scan(&localID); err != nil {
		t.Fatalf("insert local user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localID) })

	activity := func(from string) Activity {
		obj, _ := json.Marshal(map[string]string{"from_handle": from, "to_handle": localName})
		return Activity{Type: "friend_request", InstanceID: instance, Object: obj}
	}

	remoteHandle := "alice"
	s.handleInboundFriendRequest(activity(remoteHandle))

	var remoteID string
	db.QueryRow(`SELECT id FROM users WHERE remote_user_id = $1 AND remote_instance = $2`,
		remoteHandle, instance).Scan(&remoteID)
	if remoteID == "" {
		t.Fatal("no remote stub was created for the requesting account")
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteID) })

	countNotifs := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'friend_request'`, localID).Scan(&n)
		return n
	}

	if got := countNotifs(); got != 1 {
		t.Fatalf("first delivery produced %d notifications, want 1", got)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM friendships WHERE requester_id = $1 AND addressee_id = $2`,
		remoteID, localID).Scan(&status); err != nil {
		t.Fatalf("no friendship row was created: %v", err)
	}
	if status != "pending" {
		t.Errorf("friendship status = %q, want pending", status)
	}

	// The sending instance redelivers. The row already exists, so nothing new
	// happened and the recipient must not be told again.
	s.handleInboundFriendRequest(activity(remoteHandle))
	if got := countNotifs(); got != 1 {
		t.Errorf("redelivery produced %d notifications, want 1. A retrying peer would ring the bell repeatedly", got)
	}

	t.Run("a blocked account cannot reach the person who blocked them", func(t *testing.T) {
		blockedHandle := "mallory"
		blockedID := s.getOrCreateRemoteUser(blockedHandle, instance)
		if blockedID == "" {
			t.Fatal("could not create the blocked account's stub")
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, blockedID) })

		if _, err := db.Exec(`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, localID, blockedID); err != nil {
			t.Fatalf("insert block: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM blocks WHERE blocker_id = $1`, localID) })

		before := countNotifs()
		s.handleInboundFriendRequest(activity(blockedHandle))

		var exists bool
		db.QueryRow(`SELECT EXISTS(SELECT 1 FROM friendships WHERE requester_id = $1 AND addressee_id = $2)`,
			blockedID, localID).Scan(&exists)
		if exists {
			t.Error("a friendship row was created for an account the recipient had blocked")
		}
		if countNotifs() != before {
			t.Error("a blocked account's friend request produced a notification")
		}
	})

	t.Cleanup(func() {
		db.Exec(`DELETE FROM friendships WHERE addressee_id = $1 OR requester_id = $1`, localID)
		db.Exec(`DELETE FROM notifications WHERE user_id = $1`, localID)
	})
}

// TestInboundFriendAcceptNotifiesOnlyOnARealTransition covers the other
// direction: the local user sent the request, the remote side accepted. The
// notification has to follow the pending-to-accepted transition, so a
// redelivered accept (or one for a friendship that was never pending) stays
// silent.
func TestInboundFriendAcceptNotifiesOnlyOnARealTransition(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, notif: notifications.NewService(db, notifications.NewEmailService(db, &config.Config{}))}

	unique := time.Now().UnixNano()
	instance := fmt.Sprintf("agora318acc-%d.example", unique)

	localName := fmt.Sprintf("agora318_sender_%d", unique)
	var localID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id
	`, localName, localName+"@example.com").Scan(&localID); err != nil {
		t.Fatalf("insert local user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, localID) })

	remoteHandle := "bob"
	remoteID := s.getOrCreateRemoteUser(remoteHandle, instance)
	if remoteID == "" {
		t.Fatal("could not create the remote stub")
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteID) })

	countNotifs := func() int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'friend_accepted'`, localID).Scan(&n)
		return n
	}

	obj, _ := json.Marshal(map[string]string{"from_handle": remoteHandle, "to_handle": localName})
	accept := Activity{Type: "friend_accept", InstanceID: instance, Object: obj}

	// No pending friendship yet: an accept out of nowhere must do nothing.
	s.handleInboundFriendAccept(accept)
	if got := countNotifs(); got != 0 {
		t.Fatalf("an accept with no pending request produced %d notifications, want 0", got)
	}

	if _, err := db.Exec(`
		INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'pending')
	`, localID, remoteID); err != nil {
		t.Fatalf("seed pending friendship: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1`, localID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM notifications WHERE user_id = $1`, localID) })

	s.handleInboundFriendAccept(accept)
	if got := countNotifs(); got != 1 {
		t.Fatalf("accepting a pending request produced %d notifications, want 1", got)
	}

	var status string
	db.QueryRow(`SELECT status FROM friendships WHERE requester_id = $1 AND addressee_id = $2`,
		localID, remoteID).Scan(&status)
	if status != "accepted" {
		t.Errorf("friendship status = %q, want accepted", status)
	}

	s.handleInboundFriendAccept(accept)
	if got := countNotifs(); got != 1 {
		t.Errorf("a redelivered accept produced %d notifications, want 1", got)
	}
}
