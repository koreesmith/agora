package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-175: threads.net enforces "authorized fetch" — it blanket-404s any
// unsigned GET to an actor document, including the one we used to make when
// dereferencing the signer's public key to verify an inbound HTTP Signature.
// That broke verification of every activity Threads ever sent us, among them
// the Accept(Follow) that would have confirmed an outbound follow, leaving
// it stuck on "Requested" forever. fetchActorPublicKeySigned now signs that
// GET as a local user, chosen by signerUserIDForActorFetch — this exercises
// that choice against a real DB (fedHTTPClient itself refuses to dial
// loopback addresses by design, so the network hop isn't testable here).
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestSignerUserIDForActorFetch(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	mkUser := func(t *testing.T) string {
		t.Helper()
		username := fmt.Sprintf("agora175_%d", time.Now().UnixNano())
		var id string
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
			RETURNING id
		`, username, username+"@example.com").Scan(&id); err != nil {
			t.Fatalf("insert test user: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}

	t.Run("prefers the user with a pending follow of this actor", func(t *testing.T) {
		follower := mkUser(t)
		other := mkUser(t)
		actorURL := fmt.Sprintf("https://threads.example/users/%d", time.Now().UnixNano())
		if _, err := db.Exec(`
			INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url)
			VALUES ($1, $2, $3)
		`, follower, actorURL, actorURL+"/inbox"); err != nil {
			t.Fatalf("insert ap_following: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1`, follower) })

		got := s.signerUserIDForActorFetch(actorURL)
		if got != follower {
			t.Errorf("signerUserIDForActorFetch = %q, want the follower %q (not %q)", got, follower, other)
		}
	})

	t.Run("falls back to some local user when there's no matching follow", func(t *testing.T) {
		_ = mkUser(t) // ensures at least one local user exists to fall back to
		actorURL := fmt.Sprintf("https://threads.example/users/nobody-follows-%d", time.Now().UnixNano())

		got := s.signerUserIDForActorFetch(actorURL)
		if got == "" {
			t.Error("expected a fallback local user id, got empty string")
		}
	})
}
