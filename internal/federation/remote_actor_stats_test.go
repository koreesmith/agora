package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-355: GetRemoteActorStats used to hit the network unconditionally on
// every call — up to 4 sequential signed HTTP round trips per profile view,
// with no caching at all. This proves the cache-first path: with a fresh
// cached row and zero local users in the DB (so a live fetch would be
// impossible — signerUserIDForActorFetch has nothing to return), the call
// must still succeed by serving the cache rather than falling through to a
// live fetch it can't complete.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestGetRemoteActorStatsServesFreshCacheWithoutNetwork(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://test.example"}}

	actorKey := fmt.Sprintf("https://mastodon.example/users/cached-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Exec(`DELETE FROM remote_actor_stats WHERE actor_key = $1`, actorKey) })

	s.cacheRemoteActorStats(actorKey, 42, 7, 99, "a cached bio", true)

	followers, following, posts, bio, locked, ok := s.GetRemoteActorStats(actorKey)
	if !ok {
		t.Fatal("GetRemoteActorStats returned ok=false for a freshly-cached actor with zero local users present — it must have tried a live fetch instead of serving the cache")
	}
	if followers != 42 || following != 7 || posts != 99 || bio != "a cached bio" || !locked {
		t.Errorf("got (followers=%d, following=%d, posts=%d, bio=%q, locked=%v), want (42, 7, 99, %q, true)",
			followers, following, posts, bio, locked, "a cached bio")
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

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://test.example"}}
	actorKey := fmt.Sprintf("https://mastodon.example/users/roundtrip-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Exec(`DELETE FROM remote_actor_stats WHERE actor_key = $1`, actorKey) })

	if _, _, _, _, _, _, ok := s.cachedRemoteActorStats(actorKey); ok {
		t.Fatal("expected a cache miss before anything was cached")
	}

	s.cacheRemoteActorStats(actorKey, 1, 2, 3, "bio", false)
	followers, following, posts, bio, locked, fetchedAt, ok := s.cachedRemoteActorStats(actorKey)
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
