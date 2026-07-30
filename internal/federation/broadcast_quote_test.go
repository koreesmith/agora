package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-267: BroadcastPublicPost built a quote-share's Create(Note) — the
// activity carrying the quoting user's actual commentary — addressed only to
// followers/mentions/relays, never the quoted post's own remote author.
// DeliverAnnounce already delivered the (content-less) Announce half
// directly to that author via lookupRemoteTarget; this exercises the same
// lookup now being applied to the Create half too: quoting a fediverse post
// should enqueue a delivery to the quoted author's inbox even with no local
// followers and no fediverse mentions in the commentary.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestBroadcastPublicPostDeliversQuoteToRemoteAuthor(t *testing.T) {
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

	var quoterID string
	quoterUsername := fmt.Sprintf("agora267_quoter_%d", unique)
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, quoterUsername, quoterUsername+"@example.com").Scan(&quoterID); err != nil {
		t.Fatalf("insert quoter: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, quoterID) })

	remoteActorURL := fmt.Sprintf("https://mastodon.example/users/agora267_%d", unique)
	remoteInboxURL := remoteActorURL + "/inbox"
	var remoteAuthorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url, ap_inbox_url)
		VALUES ($1, $1, 'x', true, 'mastodon.example', $2, $3)
		RETURNING id
	`, fmt.Sprintf("r267_%d@mastodon.example", unique), remoteActorURL, remoteInboxURL).Scan(&remoteAuthorID); err != nil {
		t.Fatalf("insert remote author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteAuthorID) })

	remoteNoteID := fmt.Sprintf("https://mastodon.example/users/agora267_%d/statuses/1", unique)
	var quotedPostID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, is_remote, remote_post_id, remote_instance)
		VALUES ($1, 'a remote post worth quoting', 'public', true, $2, 'mastodon.example')
		RETURNING id
	`, remoteAuthorID, remoteNoteID).Scan(&quotedPostID); err != nil {
		t.Fatalf("insert quoted post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, quotedPostID) })

	var quoteShareID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, repost_of_id)
		VALUES ($1, 'my commentary on this', 'public', $2)
		RETURNING id
	`, quoterID, quotedPostID).Scan(&quoteShareID); err != nil {
		t.Fatalf("insert quote-share: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, quoteShareID) })

	s.BroadcastPublicPost(quoterID, quoteShareID)

	var queuedInbox string
	err = db.QueryRow(`
		SELECT inbox_url FROM ap_delivery_queue WHERE actor_user_id = $1 AND inbox_url = $2
	`, quoterID, remoteInboxURL).Scan(&queuedInbox)
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, quoterID) })
	if err != nil {
		t.Fatalf("expected a Create delivery queued to the quoted author's inbox %s, got none: %v", remoteInboxURL, err)
	}
}
