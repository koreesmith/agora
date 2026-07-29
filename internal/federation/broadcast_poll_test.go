package federation

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-277: BroadcastPublicPost previously never queried a post's poll
// options at all, so a poll always federated as a plain Note — the poll
// itself silently vanished, leaving only the post's commentary text.
// Verifies a poll post now federates as an ActivityPub Question with its
// options intact.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestBroadcastPublicPostFederatesPollAsQuestion(t *testing.T) {
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

	var authorID string
	username := fmt.Sprintf("agora277_author_%d", unique)
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private) VALUES ($1, $2, 'x', false)
		RETURNING id
	`, username, username+"@example.com").Scan(&authorID); err != nil {
		t.Fatalf("insert author: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	followerInbox := fmt.Sprintf("https://mastodon.example/users/agora277_%d/inbox", unique)
	if _, err := db.Exec(`
		INSERT INTO ap_followers (followed_user_id, follower_actor_url, follower_inbox_url)
		VALUES ($1, $2, $3)
	`, authorID, fmt.Sprintf("https://mastodon.example/users/agora277_%d", unique), followerInbox); err != nil {
		t.Fatalf("insert follower: %v", err)
	}

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, poll_multiple_choice)
		VALUES ($1, 'cats or dogs?', 'public', false)
		RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert poll post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	if _, err := db.Exec(`INSERT INTO poll_options (post_id, text, position) VALUES ($1, 'Cats', 0), ($1, 'Dogs', 1)`, postID); err != nil {
		t.Fatalf("insert poll options: %v", err)
	}

	s.BroadcastPublicPost(authorID, postID)
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, authorID) })

	var payloadRaw []byte
	if err := db.QueryRow(`
		SELECT activity FROM ap_delivery_queue WHERE actor_user_id = $1 AND inbox_url = $2
	`, authorID, followerInbox).Scan(&payloadRaw); err != nil {
		t.Fatalf("expected a Create delivery queued to the follower's inbox %s, got none: %v", followerInbox, err)
	}

	var activity struct {
		Object struct {
			Type  string `json:"type"`
			OneOf []struct {
				Name string `json:"name"`
			} `json:"oneOf"`
		} `json:"object"`
	}
	if err := json.Unmarshal(payloadRaw, &activity); err != nil {
		t.Fatalf("unmarshal queued activity: %v", err)
	}
	if activity.Object.Type != "Question" {
		t.Fatalf("expected object type %q, got %q", "Question", activity.Object.Type)
	}
	if len(activity.Object.OneOf) != 2 || activity.Object.OneOf[0].Name != "Cats" || activity.Object.OneOf[1].Name != "Dogs" {
		t.Fatalf("expected oneOf [Cats, Dogs], got %+v", activity.Object.OneOf)
	}
}
