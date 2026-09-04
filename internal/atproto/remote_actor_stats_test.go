package atproto

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-355: GetRemoteActorStats used to hit the Bluesky AppView
// unconditionally on every call, with no caching. This proves the
// cache-first path: with a fresh cached row, the call must return the
// cached values without needing (or attempting) a live AppView call.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestGetRemoteActorStatsServesFreshCache(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	did := fmt.Sprintf("did:plc:cached%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Exec(`DELETE FROM remote_actor_stats WHERE actor_key = $1`, did) })

	s.cacheRemoteActorStats(did, 10, 20, 30, "a bluesky bio")

	followers, following, posts, bio, ok := s.GetRemoteActorStats(did)
	if !ok {
		t.Fatal("GetRemoteActorStats returned ok=false for a freshly-cached DID")
	}
	if followers != 10 || following != 20 || posts != 30 || bio != "a bluesky bio" {
		t.Errorf("got (followers=%d, following=%d, posts=%d, bio=%q), want (10, 20, 30, %q)",
			followers, following, posts, bio, "a bluesky bio")
	}
}

func TestRemoteActorStatsCacheRoundTrip(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}
	did := fmt.Sprintf("did:plc:roundtrip%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Exec(`DELETE FROM remote_actor_stats WHERE actor_key = $1`, did) })

	if _, _, _, _, _, _, ok := s.cachedRemoteActorStats(did); ok {
		t.Fatal("expected a cache miss before anything was cached")
	}

	s.cacheRemoteActorStats(did, 1, 2, 3, "bio")
	followers, following, posts, bio, locked, fetchedAt, ok := s.cachedRemoteActorStats(did)
	if !ok {
		t.Fatal("expected a cache hit after caching")
	}
	if followers != 1 || following != 2 || posts != 3 || bio != "bio" || locked {
		t.Errorf("got (%d, %d, %d, %q, %v), want (1, 2, 3, \"bio\", false)", followers, following, posts, bio, locked)
	}
	if time.Since(fetchedAt) >= remoteActorStatsTTL {
		t.Errorf("freshly cached stats already read as stale: fetched_at = %v", fetchedAt)
	}
}
