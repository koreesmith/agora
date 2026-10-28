package federation

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-400: an inbound Note's image descriptions come from each attachment's
// "name" and must stay lined up with the image URLs matchAttachments picks,
// skipping the same non-image attachments it skips.
func TestAttachmentAltsLineUpWithImages(t *testing.T) {
	attachments := []apAttachment{
		{Type: "Document", MediaType: "image/jpeg", URL: "https://m.example/1.jpg", Name: "First"},
		{Type: "Video", MediaType: "video/mp4", URL: "https://m.example/v.mp4", Name: "a video"},
		{Type: "Image", URL: "", Name: "no url, skipped"},
		{Type: "Image", URL: "https://m.example/2.png"},
	}
	urls, _ := matchAttachments(attachments)
	alts := attachmentAlts(attachments)
	if len(urls) != 2 || len(alts) != 2 {
		t.Fatalf("got %d urls and %d alts, want 2 of each", len(urls), len(alts))
	}
	if alts[0] != "First" || alts[1] != "" {
		t.Errorf("alts = %q, want [First \"\"]", alts)
	}
}

// AGORA-400: a local post's descriptions go out as each attachment's "name",
// and an inbound post's descriptions are stored against the right images.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestImageAltsFederate(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
	t.Cleanup(func() { db.Close() }) // registered first, so it runs last
	if err := db.Migrate(); err != nil { t.Fatalf("migrate: %v", err) }

	var userID string
	name := fmt.Sprintf("altfed400_%d", time.Now().UnixNano())
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $1, 'x') RETURNING id`, name).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://test.example"}}

	newPost := func() string {
		var id string
		db.QueryRow(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'x', 'public') RETURNING id`, userID).Scan(&id)
		return id
	}

	t.Run("outbound attachments carry name", func(t *testing.T) {
		postID := newPost()
		db.Exec(`UPDATE posts SET image_url = '/uploads/posts/a.jpg', image_alt = 'A dog' WHERE id = $1`, postID)
		db.Exec(`INSERT INTO post_photos (post_id, url, position, alt) VALUES ($1, '/uploads/posts/a.jpg', 0, 'A dog'), ($1, '/uploads/posts/b.jpg', 1, '')`, postID)

		note := s.buildNoteObject("https://test.example/users/"+name, postID, "x", time.Now(), "", "")
		atts, _ := note["attachment"].([]map[string]any)
		if len(atts) != 2 {
			t.Fatalf("attachments = %v, want 2", note["attachment"])
		}
		if atts[0]["name"] != "A dog" {
			t.Errorf("first attachment name = %v, want A dog", atts[0]["name"])
		}
		if _, ok := atts[1]["name"]; ok {
			t.Errorf("an image with no description should carry no name, got %v", atts[1]["name"])
		}
	})

	t.Run("inbound descriptions are stored with their images", func(t *testing.T) {
		multi := newPost()
		s.storeInboundImages(multi, []string{"https://m.example/1.jpg", "https://m.example/2.jpg"}, []string{"One", "Two"})
		var first string
		db.QueryRow(`SELECT image_alt FROM posts WHERE id = $1`, multi).Scan(&first)
		var second string
		db.QueryRow(`SELECT alt FROM post_photos WHERE post_id = $1 AND position = 1`, multi).Scan(&second)
		if first != "One" || second != "Two" {
			t.Errorf("stored %q and %q, want One and Two", first, second)
		}

		single := newPost()
		s.storeInboundImages(single, []string{"https://m.example/3.jpg"}, nil)
		var alt string
		db.QueryRow(`SELECT image_alt FROM posts WHERE id = $1`, single).Scan(&alt)
		if alt != "" {
			t.Errorf("no description stored as %q, want empty", alt)
		}
	})
}
