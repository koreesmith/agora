package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-268: DeliverVote is the outbound half of ActivityPub poll voting —
// when a local user votes on a poll that originated from a remote Mastodon
// actor, a Create(Note) with "name" set to the chosen option must be
// delivered directly to that actor's inbox (there is no Public/followers
// audience for a vote, only the poll's own actor).
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestDeliverVoteEnqueuesToRemotePollActor(t *testing.T) {
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

	var voterID string
	voterUsername := fmt.Sprintf("agora268_voter_%d", unique)
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, voterUsername, voterUsername+"@example.com").Scan(&voterID); err != nil {
		t.Fatalf("insert voter: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, voterID) })

	remoteActorURL := fmt.Sprintf("https://mastodon.example/users/agora268_%d", unique)
	remoteInboxURL := remoteActorURL + "/inbox"
	var remoteAuthorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url, ap_inbox_url)
		VALUES ($1, $1, 'x', true, 'mastodon.example', $2, $3)
		RETURNING id
	`, fmt.Sprintf("r268_%d@mastodon.example", unique), remoteActorURL, remoteInboxURL).Scan(&remoteAuthorID); err != nil {
		t.Fatalf("insert remote author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteAuthorID) })

	remoteNoteID := fmt.Sprintf("https://mastodon.example/users/agora268_%d/statuses/1", unique)
	var pollPostID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, is_remote, remote_post_id, remote_instance)
		VALUES ($1, 'pick one', 'public', true, $2, 'mastodon.example')
		RETURNING id
	`, remoteAuthorID, remoteNoteID).Scan(&pollPostID); err != nil {
		t.Fatalf("insert poll post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, pollPostID) })

	var optionID string
	if err := db.QueryRow(`
		INSERT INTO poll_options (post_id, text, position) VALUES ($1, 'Option A', 0)
		RETURNING id
	`, pollPostID).Scan(&optionID); err != nil {
		t.Fatalf("insert poll option: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM poll_options WHERE post_id = $1`, pollPostID) })

	s.DeliverVote(voterID, pollPostID, optionID)

	var queuedInbox string
	err = db.QueryRow(`
		SELECT inbox_url FROM ap_delivery_queue WHERE actor_user_id = $1
	`, voterID).Scan(&queuedInbox)
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, voterID) })
	if err != nil {
		t.Fatalf("expected a queued vote delivery for voter %s, got none: %v", voterID, err)
	}
	if queuedInbox != remoteInboxURL {
		t.Errorf("queued inbox_url = %q, want %q", queuedInbox, remoteInboxURL)
	}
}

// TestHandleInboundVote is a regression test for the inbound half of
// AGORA-268 — a remote actor's Vote (a Note with "name" set, inReplyTo one
// of our own polls) must record a poll_votes row attributed to that actor's
// local stub, and must not double-count a redelivered vote or accept one
// past the poll's expiry.
func TestHandleInboundVote(t *testing.T) {
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

	var pollAuthorID string
	pollAuthorUsername := fmt.Sprintf("agora268b_author_%d", unique)
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, activitypub_enabled) VALUES ($1, $2, 'x', true)
		RETURNING id
	`, pollAuthorUsername, pollAuthorUsername+"@example.com").Scan(&pollAuthorID); err != nil {
		t.Fatalf("insert poll author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, pollAuthorID) })

	voterActorURL := fmt.Sprintf("https://mastodon.example/users/agora268b_voter_%d", unique)
	var voterStubID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url, ap_inbox_url)
		VALUES ($1, $1, 'x', true, 'mastodon.example', $2, $3)
		RETURNING id
	`, fmt.Sprintf("voter268b_%d@mastodon.example", unique), voterActorURL, voterActorURL+"/inbox").Scan(&voterStubID); err != nil {
		t.Fatalf("insert voter stub: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, voterStubID) })

	var pollPostID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'pick one', 'public')
		RETURNING id
	`, pollAuthorID).Scan(&pollPostID); err != nil {
		t.Fatalf("insert poll post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, pollPostID) })

	var optionID string
	if err := db.QueryRow(`
		INSERT INTO poll_options (post_id, text, position) VALUES ($1, 'Option A', 0)
		RETURNING id
	`, pollPostID).Scan(&optionID); err != nil {
		t.Fatalf("insert poll option: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM poll_options WHERE post_id = $1`, pollPostID) })

	pollObjectURL := fmt.Sprintf("https://agora.example/federation/users/%s/posts/%s", pollAuthorUsername, pollPostID)

	handled := s.handleInboundVote(voterActorURL, pollObjectURL, "Option A")
	if !handled {
		t.Fatalf("expected handleInboundVote to recognize a vote on our own poll")
	}

	var voteCount int
	db.QueryRow(`SELECT count(*) FROM poll_votes WHERE user_id = $1 AND option_id = $2`, voterStubID, optionID).Scan(&voteCount)
	t.Cleanup(func() { db.Exec(`DELETE FROM poll_votes WHERE user_id = $1`, voterStubID) })
	if voteCount != 1 {
		t.Fatalf("expected exactly 1 vote row after first delivery, got %d", voteCount)
	}

	// Redelivery must not double-count.
	s.handleInboundVote(voterActorURL, pollObjectURL, "Option A")
	db.QueryRow(`SELECT count(*) FROM poll_votes WHERE user_id = $1 AND option_id = $2`, voterStubID, optionID).Scan(&voteCount)
	if voteCount != 1 {
		t.Fatalf("expected exactly 1 vote row after redelivery, got %d", voteCount)
	}

	// A vote naming an option that doesn't exist on this poll (or targeting
	// something that isn't one of our posts) must not be recognized as a
	// vote at all, leaving the normal reply path to handle it instead.
	if s.handleInboundVote(voterActorURL, pollObjectURL, "Not A Real Option") {
		t.Fatalf("expected handleInboundVote to return false for a non-matching option")
	}
}
