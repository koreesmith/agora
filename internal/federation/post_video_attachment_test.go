package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// TestBuildNoteObjectAttachesVideo is AGORA-368: a post's video never made
// it into the outgoing Note's "attachment" array, so it federated as
// text-only to any peer, Agora, Mastodon, or otherwise. This confirms the
// attachment is now present with the fields a receiving instance needs
// (mediaType so matchAttachments recognizes it, url to fetch it, an icon
// carrying the poster thumbnail).
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestBuildNoteObjectAttachesVideo(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora368.example"}}

	username := fmt.Sprintf("agora368_%d", time.Now().UnixNano())
	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.invalid").Scan(&authorID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, video_url, video_thumb_url)
		VALUES ($1, 'watch this', 'public', '/uploads/videos/x.mp4', '/uploads/videos/x_thumb.jpg')
		RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert test post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	note := s.buildNoteObject(s.actorURL(username), postID, "watch this", time.Now(), "", "")

	attachments, ok := note["attachment"].([]map[string]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("note[attachment] = %#v, want exactly one video attachment", note["attachment"])
	}
	a := attachments[0]
	if a["type"] != "Video" {
		t.Errorf("attachment type = %v, want Video", a["type"])
	}
	if a["mediaType"] != "video/mp4" {
		t.Errorf("attachment mediaType = %v, want video/mp4", a["mediaType"])
	}
	if a["url"] != "https://agora368.example/uploads/videos/x.mp4" {
		t.Errorf("attachment url = %v, want the absolute video URL", a["url"])
	}
	icon, ok := a["icon"].(map[string]string)
	if !ok || icon["url"] != "https://agora368.example/uploads/videos/x_thumb.jpg" {
		t.Errorf("attachment icon = %#v, want the absolute thumbnail URL", a["icon"])
	}
}

// TestBuildNoteObjectNoAttachmentWithoutMedia is the control: a plain
// text-only post must not grow an attachment array out of nowhere.
func TestBuildNoteObjectNoAttachmentWithoutMedia(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora368.example"}}

	username := fmt.Sprintf("agora368_plain_%d", time.Now().UnixNano())
	var authorID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.invalid").Scan(&authorID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, authorID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'just words', 'public')
		RETURNING id
	`, authorID).Scan(&postID); err != nil {
		t.Fatalf("insert test post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	note := s.buildNoteObject(s.actorURL(username), postID, "just words", time.Now(), "", "")
	if _, present := note["attachment"]; present {
		t.Errorf("note[attachment] = %#v for a text-only post, want no attachment key at all", note["attachment"])
	}
}
