package atproto

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"

	"github.com/agora-social/agora/internal/store"
)

// TestIsBlueskyGifURL is AGORA-377: only a Tenor media file counts as a
// picked GIF. A tenor.com share page or a .gif hosted anywhere else is a
// link someone shared, and stays a link card.
func TestIsBlueskyGifURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://media.tenor.com/abc123AAAAC/happy-dance.gif?hh=280&ww=498", true},
		{"https://media.tenor.com/abc123AAAAC/happy-dance.gif", true},
		{"https://MEDIA.TENOR.COM/abc123AAAAC/Happy-Dance.GIF", true},
		{"http://media.tenor.com/abc123AAAAC/happy-dance.gif", false},
		{"https://tenor.com/view/happy-dance-gif-12345", false},
		{"https://media.tenor.com/abc123AAAPo/happy-dance.mp4", false},
		{"https://example.com/funny.gif", false},
		{"https://media.tenor.com.evil.example/x.gif", false},
		{"", false},
		{"not a url", false},
	}
	for _, c := range cases {
		if got := isBlueskyGifURL(c.url); got != c.want {
			t.Errorf("isBlueskyGifURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

// TestStoreInboundEmbedGifGoesToImageURL is AGORA-377: a GIF reply from
// Bluesky was stored as a link card, which a comment never renders, so it
// showed up as an empty comment. It must land in image_url, with the link
// card columns left empty so a top-level post doesn't render it twice. A
// plain link is the control and must keep its link card.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestStoreInboundEmbedGifGoesToImageURL(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	username := fmt.Sprintf("agora377_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	newPost := func(t *testing.T) string {
		t.Helper()
		var id string
		if err := db.QueryRow(`
			INSERT INTO posts (author_id, content, visibility) VALUES ($1, '', 'public')
			RETURNING id
		`, userID).Scan(&id); err != nil {
			t.Fatalf("insert test post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}
	external := func(uri string) *bsky.FeedDefs_PostView_Embed {
		thumb := "https://cdn.bsky.app/img/feed_thumbnail/plain/did:plc:x/bafy@jpeg"
		return &bsky.FeedDefs_PostView_Embed{EmbedExternal_View: &bsky.EmbedExternal_View{
			External: &bsky.EmbedExternal_ViewExternal{Uri: uri, Title: "Happy Dance GIF", Description: "Alt: Happy Dance GIF", Thumb: &thumb},
		}}
	}
	columns := func(t *testing.T, id string) (imageURL, linkURL, linkImage string) {
		t.Helper()
		if err := db.QueryRow(`SELECT image_url, link_url, link_image FROM posts WHERE id = $1`, id).
			Scan(&imageURL, &linkURL, &linkImage); err != nil {
			t.Fatalf("read back post: %v", err)
		}
		return
	}

	t.Run("gif", func(t *testing.T) {
		const gif = "https://media.tenor.com/abc123AAAAC/happy-dance.gif?hh=280&ww=498"
		id := newPost(t)
		s.storeInboundEmbed(context.Background(), id, external(gif))
		imageURL, linkURL, linkImage := columns(t, id)
		if imageURL != gif {
			t.Errorf("image_url = %q, want the GIF %q", imageURL, gif)
		}
		if linkURL != "" || linkImage != "" {
			t.Errorf("link_url = %q, link_image = %q, want both empty for a GIF", linkURL, linkImage)
		}
	})

	t.Run("link", func(t *testing.T) {
		const link = "https://example.com/article"
		id := newPost(t)
		s.storeInboundEmbed(context.Background(), id, external(link))
		imageURL, linkURL, linkImage := columns(t, id)
		if imageURL != "" {
			t.Errorf("image_url = %q, want empty for a plain link", imageURL)
		}
		if linkURL != link || linkImage == "" {
			t.Errorf("link_url = %q, link_image = %q, want the link card kept", linkURL, linkImage)
		}
	})
}
