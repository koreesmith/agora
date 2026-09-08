package atproto

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// TestBuildMediaEmbedUsesVideoWhenNoImages is AGORA-368: a post's video
// never made it into the Bluesky record at all, buildImageEmbed was the
// only embed builder BroadcastPost/BroadcastUpdatePost ever called. This
// confirms buildMediaEmbed picks up a video when there's no image, and that
// the resulting embed carries a real blob (not just a URL string, AT Proto
// records reference uploaded blobs, not links).
//
// Requires the local agora-postgres-test instance (localhost:15433); skips
// if it isn't reachable rather than failing the suite.
func TestBuildMediaEmbedUsesVideoWhenNoImages(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	uploadDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(uploadDir, "videos"), 0755); err != nil {
		t.Fatalf("mkdir videos dir: %v", err)
	}
	// Content doesn't need to be a real playable video: uploadBlob only
	// reads bytes and content-sniffs a mimetype, it never validates video
	// correctness (Agora's own transcode step already did that on upload).
	videoPath := filepath.Join(uploadDir, "videos", "agora368.mp4")
	if err := os.WriteFile(videoPath, []byte("not a real mp4, just needs bytes"), 0644); err != nil {
		t.Fatalf("write fake video file: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{UploadDir: uploadDir}}

	username := fmt.Sprintf("agora368_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_blocks WHERE user_id = $1`, userID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, video_url)
		VALUES ($1, 'watch this', 'public', '/uploads/videos/agora368.mp4')
		RETURNING id
	`, userID).Scan(&postID); err != nil {
		t.Fatalf("insert test post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	bs := &pgBlockstore{db: db, userID: userID}
	embed := s.buildMediaEmbed(context.Background(), bs, postID)

	if embed == nil {
		t.Fatal("buildMediaEmbed returned nil for a post with a video, want a video embed")
	}
	if embed.EmbedImages != nil {
		t.Error("embed.EmbedImages is set for a post with no images, want nil")
	}
	if embed.EmbedVideo == nil {
		t.Fatal("embed.EmbedVideo is nil, want the post's video embedded")
	}
	if embed.EmbedVideo.Video == nil {
		t.Error("embed.EmbedVideo.Video (the blob ref) is nil, want the uploaded blob")
	}
}

// TestBuildMediaEmbedNilWithoutMedia is the control: a plain text-only post
// must not produce an embed of either kind.
func TestBuildMediaEmbedNilWithoutMedia(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()

	s := &Service{db: db, cfg: &config.Config{UploadDir: t.TempDir()}}

	username := fmt.Sprintf("agora368_plain_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')
		RETURNING id
	`, username, username+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	var postID string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'just words', 'public')
		RETURNING id
	`, userID).Scan(&postID); err != nil {
		t.Fatalf("insert test post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })

	bs := &pgBlockstore{db: db, userID: userID}
	embed := s.buildMediaEmbed(context.Background(), bs, postID)
	if embed != nil {
		t.Errorf("buildMediaEmbed = %#v for a text-only post, want nil", embed)
	}
}
