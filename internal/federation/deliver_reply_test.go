package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-264: DeliverReply/DeliverReplyUpdate used to re-implement the
// remote-target lookup inline instead of calling lookupRemoteTarget, so they
// never got the AGORA-254 self-heal for a stale cached actor stub with no
// ap_inbox_url — DeliverLike/DeliverAnnounce got the fix, DeliverReply didn't.
// This exercises the now-shared happy path (a fully-populated remote target,
// no self-heal/network fetch needed) end to end: a reply to a fediverse post
// should land a row in ap_delivery_queue addressed to the target's inbox.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestDeliverReplyEnqueuesToRemoteTarget(t *testing.T) {
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

	var replierID string
	replierUsername := fmt.Sprintf("agora264_replier_%d", unique)
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, replierUsername, replierUsername+"@example.com").Scan(&replierID); err != nil {
		t.Fatalf("insert replier: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, replierID) })

	remoteActorURL := fmt.Sprintf("https://mastodon.example/users/agora264_%d", unique)
	remoteInboxURL := remoteActorURL + "/inbox"
	var remoteAuthorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url, ap_inbox_url)
		VALUES ($1, $1, 'x', true, 'mastodon.example', $2, $3)
		RETURNING id
	`, fmt.Sprintf("r264_%d@mastodon.example", unique), remoteActorURL, remoteInboxURL).Scan(&remoteAuthorID); err != nil {
		t.Fatalf("insert remote author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteAuthorID) })

	remoteNoteID := fmt.Sprintf("https://mastodon.example/users/agora264_%d/statuses/1", unique)
	var replyToID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, is_remote, remote_post_id, remote_instance)
		VALUES ($1, 'a remote post', 'public', true, $2, 'mastodon.example')
		RETURNING id
	`, remoteAuthorID, remoteNoteID).Scan(&replyToID); err != nil {
		t.Fatalf("insert remote target post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, replyToID) })

	var commentID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, parent_id)
		VALUES ($1, 'my reply', 'public', $2)
		RETURNING id
	`, replierID, replyToID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, commentID) })

	s.DeliverReply(replierID, commentID, replyToID)

	var queuedInbox string
	err = db.QueryRow(`
		SELECT inbox_url FROM ap_delivery_queue WHERE actor_user_id = $1
	`, replierID).Scan(&queuedInbox)
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, replierID) })
	if err != nil {
		t.Fatalf("expected a queued delivery for replier %s, got none: %v", replierID, err)
	}
	if queuedInbox != remoteInboxURL {
		t.Errorf("queued inbox_url = %q, want %q", queuedInbox, remoteInboxURL)
	}
}
