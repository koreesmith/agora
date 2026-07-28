package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-265: handleInboundAnnounce used to silently drop a boost of any post
// that wasn't Agora-originated — resolveFederatableTarget only ever resolves
// against our own posts, so a followed actor boosting genuinely third-party
// fediverse content never landed in the local feed. The fallback,
// handleInboundAnnounceOfRemotePost, dereferences the boosted object over
// the network, which fedHTTPClient refuses to do against loopback addresses
// by design — so this only exercises the short-circuits that must hold
// before any such fetch is attempted: no local follower of the boosting
// actor means no audience to attribute a repost to, so nothing should be
// ingested and no network call should even be reached.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestHandleInboundAnnounceOfRemotePostRequiresLocalFollower(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora.example"}}
	unique := time.Now().UnixNano()
	actorURL := fmt.Sprintf("https://mastodon.example/users/booster_%d", unique)
	objectURL := fmt.Sprintf("https://mastodon.example/users/author_%d/statuses/1", unique)
	activityID := fmt.Sprintf("https://mastodon.example/users/booster_%d/statuses/1/activity", unique)

	var postCountBefore int
	db.QueryRow(`SELECT count(*) FROM posts WHERE is_remote = true`).Scan(&postCountBefore)

	// No ap_following row for actorURL exists yet — nobody locally follows
	// the boosting actor, so this must no-op without attempting the
	// (untestable, network-bound) dereference.
	s.handleInboundAnnounceOfRemotePost(activityID, actorURL, objectURL)

	var postCountAfter int
	db.QueryRow(`SELECT count(*) FROM posts WHERE is_remote = true`).Scan(&postCountAfter)
	if postCountAfter != postCountBefore {
		t.Fatalf("expected no posts ingested with no local follower, before=%d after=%d", postCountBefore, postCountAfter)
	}
}
