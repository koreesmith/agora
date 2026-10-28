package atproto

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-400: an ingested Bluesky post keeps each image's alt as its
// description, and a local post's descriptions are what postImages hands the
// outbound image embed.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestImageAltsBluesky(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
	t.Cleanup(func() { db.Close() }) // registered first, so it runs last
	if err := db.Migrate(); err != nil { t.Fatalf("migrate: %v", err) }

	var userID string
	name := fmt.Sprintf("altbsky400_%d", time.Now().UnixNano())
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $1, 'x') RETURNING id`, name).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
	s := &Service{db: db}

	newPost := func() string {
		var id string
		db.QueryRow(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'x', 'public') RETURNING id`, userID).Scan(&id)
		return id
	}

	t.Run("inbound alt is stored", func(t *testing.T) {
		postID := newPost()
		s.storeInboundBskyImages(postID, &bsky.EmbedImages_View{Images: []*bsky.EmbedImages_ViewImage{
			{Fullsize: "https://cdn.example/1.jpg", Alt: "A heron"},
			{Fullsize: "", Alt: "no image, skipped"},
			{Fullsize: "https://cdn.example/2.jpg", Alt: ""},
		}})
		var first, second string
		db.QueryRow(`SELECT image_alt FROM posts WHERE id = $1`, postID).Scan(&first)
		db.QueryRow(`SELECT alt FROM post_photos WHERE post_id = $1 AND position = 1`, postID).Scan(&second)
		if first != "A heron" || second != "" {
			t.Errorf("stored %q and %q, want A heron and empty", first, second)
		}
	})

	t.Run("postImages returns descriptions for the outbound embed", func(t *testing.T) {
		postID := newPost()
		db.Exec(`UPDATE posts SET image_url = '/uploads/posts/a.jpg', image_alt = 'A dog' WHERE id = $1`, postID)
		imgs := s.postImages(context.Background(), postID)
		if len(imgs) != 1 || imgs[0].Alt != "A dog" {
			t.Errorf("postImages = %+v, want one image described A dog", imgs)
		}
	})
}
