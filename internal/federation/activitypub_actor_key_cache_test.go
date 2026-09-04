package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-354: every inbound federated request used to pay for a live, signed
// HTTP GET of the sender's actor document just to verify its HTTP Signature.
// Any transient failure of that fetch failed verification outright, which
// the sender's delivery queue recorded as a failed attempt and backed off on
// (AGORA-353's exponential retry schedule) — turning a network blip into an
// hours-long, sometimes permanent, sync delay. This exercises the
// remote_actor_keys cache round-trip and TTL/staleness handling directly
// against a real DB (fedHTTPClient itself refuses to dial loopback by
// design, so the live-fetch path isn't testable here).
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestActorPublicKeyCache(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	t.Run("cache miss returns ok=false", func(t *testing.T) {
		actorURL := fmt.Sprintf("https://example.test/users/nobody-%d", time.Now().UnixNano())
		_, _, ok := s.cachedActorPublicKeyPEM(actorURL)
		if ok {
			t.Error("expected a cache miss for an actor URL never cached")
		}
	})

	t.Run("cached key round-trips and is reported fresh within TTL", func(t *testing.T) {
		actorURL := fmt.Sprintf("https://example.test/users/cached-%d", time.Now().UnixNano())
		t.Cleanup(func() { db.Exec(`DELETE FROM remote_actor_keys WHERE actor_url = $1`, actorURL) })

		const fakePEM = "-----BEGIN PUBLIC KEY-----\nfake\n-----END PUBLIC KEY-----\n"
		s.cacheActorPublicKeyPEM(actorURL, fakePEM)

		pubPEM, fetchedAt, ok := s.cachedActorPublicKeyPEM(actorURL)
		if !ok {
			t.Fatal("expected a cache hit right after caching")
		}
		if pubPEM != fakePEM {
			t.Errorf("cached PEM = %q, want %q", pubPEM, fakePEM)
		}
		if time.Since(fetchedAt) >= remoteActorKeyTTL {
			t.Errorf("freshly cached key already reads as stale: fetched_at = %v", fetchedAt)
		}
	})

	t.Run("re-caching the same actor URL overwrites rather than duplicates", func(t *testing.T) {
		actorURL := fmt.Sprintf("https://example.test/users/rotated-%d", time.Now().UnixNano())
		t.Cleanup(func() { db.Exec(`DELETE FROM remote_actor_keys WHERE actor_url = $1`, actorURL) })

		s.cacheActorPublicKeyPEM(actorURL, "old-key")
		s.cacheActorPublicKeyPEM(actorURL, "new-key")

		pubPEM, _, ok := s.cachedActorPublicKeyPEM(actorURL)
		if !ok {
			t.Fatal("expected a cache hit")
		}
		if pubPEM != "new-key" {
			t.Errorf("cached PEM = %q, want the overwritten %q", pubPEM, "new-key")
		}

		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM remote_actor_keys WHERE actor_url = $1`, actorURL).Scan(&count); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		if count != 1 {
			t.Errorf("expected exactly one row for %q after re-caching, got %d", actorURL, count)
		}
	})
}
