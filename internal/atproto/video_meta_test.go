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
	"github.com/bluesky-social/indigo/api/bsky"
)

func openVideoMetaTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mkVideoMetaTestPost(t *testing.T, db *store.DB, videoURL string, w, h int) (userID, postID string) {
	t.Helper()
	name := fmt.Sprintf("vm437_%d", time.Now().UnixNano())
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		name, name+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_blocks WHERE user_id = $1`, userID) })
	if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility, video_url, video_width, video_height)
		VALUES ($1, 'clip', 'public', $2, $3, $4) RETURNING id`, userID, videoURL, w, h).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })
	return
}

// AGORA-437: a Bluesky video's aspect ratio becomes the post's width and
// height; a missing or nonsensical one leaves them unknown.
func TestStoreInboundVideoAspectRatio(t *testing.T) {
	db := openVideoMetaTestDB(t)
	s := &Service{db: db, cfg: &config.Config{}}

	cases := []struct {
		name         string
		ratio        *bsky.EmbedDefs_AspectRatio
		wantW, wantH int
	}{
		{"vertical", &bsky.EmbedDefs_AspectRatio{Width: 1080, Height: 1920}, 1080, 1920},
		{"missing", nil, 0, 0},
		{"zero", &bsky.EmbedDefs_AspectRatio{Width: 0, Height: 0}, 0, 0},
		{"negative", &bsky.EmbedDefs_AspectRatio{Width: -4, Height: 3}, 0, 0},
		{"absurd", &bsky.EmbedDefs_AspectRatio{Width: 1 << 40, Height: 1 << 40}, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, postID := mkVideoMetaTestPost(t, db, "", 0, 0)
			s.storeInboundVideo(postID, &bsky.EmbedVideo_View{Playlist: "https://video.bsky.app/x/playlist.m3u8", AspectRatio: c.ratio})
			var url string
			var w, h int
			db.QueryRow(`SELECT video_url, video_width, video_height FROM posts WHERE id = $1`, postID).Scan(&url, &w, &h)
			if url == "" {
				t.Fatal("video_url was not stored")
			}
			if w != c.wantW || h != c.wantH {
				t.Errorf("got %dx%d, want %dx%d", w, h, c.wantW, c.wantH)
			}
		})
	}
}

// An outgoing video record carries the aspect ratio when it is known, and
// is unchanged when it isn't.
func TestBuildVideoEmbedAspectRatio(t *testing.T) {
	db := openVideoMetaTestDB(t)
	uploadDir := t.TempDir()
	os.MkdirAll(filepath.Join(uploadDir, "videos"), 0755)
	os.WriteFile(filepath.Join(uploadDir, "videos", "vm437.mp4"), []byte("not a real mp4, just needs bytes"), 0644)
	s := &Service{db: db, cfg: &config.Config{UploadDir: uploadDir}}

	userID, withShape := mkVideoMetaTestPost(t, db, "/uploads/videos/vm437.mp4", 720, 1280)
	bs := &pgBlockstore{db: db, userID: userID}
	embed := s.buildVideoEmbed(context.Background(), bs, withShape)
	if embed == nil || embed.EmbedVideo == nil {
		t.Fatal("no video embed")
	}
	ar := embed.EmbedVideo.AspectRatio
	if ar == nil || ar.Width != 720 || ar.Height != 1280 {
		t.Errorf("aspectRatio = %+v, want 720x1280", ar)
	}

	var noShape string
	db.QueryRow(`INSERT INTO posts (author_id, content, visibility, video_url) VALUES ($1, 'clip', 'public', '/uploads/videos/vm437.mp4') RETURNING id`, userID).Scan(&noShape)
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, noShape) })
	embed = s.buildVideoEmbed(context.Background(), bs, noShape)
	if embed == nil || embed.EmbedVideo == nil {
		t.Fatal("no video embed for unknown shape")
	}
	if embed.EmbedVideo.AspectRatio != nil {
		t.Errorf("aspectRatio = %+v for a video of unknown shape, want none", embed.EmbedVideo.AspectRatio)
	}
}
