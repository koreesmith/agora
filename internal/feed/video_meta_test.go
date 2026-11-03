package feed

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

func openVideoMetaDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() }) // registered first, so it runs last
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mkVideoMetaUser(t *testing.T, db *store.DB, tag string) string {
	t.Helper()
	name := fmt.Sprintf("vm437_%s_%d", tag, time.Now().UnixNano())
	var id string
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $1, 'x') RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return id
}

func mkVideoMetaPost(t *testing.T, db *store.DB, authorID, videoURL string, w, h, ms int) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, video_url, video_width, video_height, video_duration_ms)
		VALUES ($1, 'clip', 'public', $2, $3, $4, $5) RETURNING id`, authorID, videoURL, w, h, ms).Scan(&id); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
	return id
}

// AGORA-437: a video post in any feed carries its size and length; a post
// with no video, or a video of unknown shape, carries nothing extra.
func TestMarkVideoMeta(t *testing.T) {
	db := openVideoMetaDB(t)
	s := &Service{db: db}
	author := mkVideoMetaUser(t, db, "mark")

	known := mkVideoMetaPost(t, db, author, "/uploads/videos/k.mp4", 720, 1280, 65000)
	unknown := mkVideoMetaPost(t, db, author, "/uploads/videos/u.mp4", 0, 0, 0)
	text := mkVideoMetaPost(t, db, author, "", 0, 0, 0)

	posts := []Post{
		{ID: known, VideoURL: "/uploads/videos/k.mp4"},
		{ID: unknown, VideoURL: "/uploads/videos/u.mp4"},
		{ID: text},
	}
	s.markVideoMeta(posts)

	if p := posts[0]; p.VideoWidth != 720 || p.VideoHeight != 1280 || p.VideoDurationMs != 65000 {
		t.Errorf("known video: got %d/%d/%d, want 720/1280/65000", p.VideoWidth, p.VideoHeight, p.VideoDurationMs)
	}
	for i, name := range map[int]string{1: "unknown-shape video", 2: "text post"} {
		if p := posts[i]; p.VideoWidth != 0 || p.VideoHeight != 0 || p.VideoDurationMs != 0 {
			t.Errorf("%s: got %d/%d/%d, want zeros", name, p.VideoWidth, p.VideoHeight, p.VideoDurationMs)
		}
	}

	// no videos at all: must not query or panic
	s.markVideoMeta([]Post{{ID: text}})
	s.markVideoMeta(nil)
}

// A new post picks up the shape its upload job measured, but only from the
// poster's own finished job for that exact URL.
func TestApplyUploadedVideoMeta(t *testing.T) {
	db := openVideoMetaDB(t)
	s := &Service{db: db}
	author := mkVideoMetaUser(t, db, "apply")
	other := mkVideoMetaUser(t, db, "applyother")
	url := fmt.Sprintf("/uploads/videos/vm437_%d.mp4", time.Now().UnixNano())

	var jobID string
	if err := db.QueryRow(`
		INSERT INTO video_transcode_jobs (user_id, status, video_url, video_width, video_height, video_duration_ms)
		VALUES ($1, 'done', $2, 1080, 1920, 42000) RETURNING id`, author, url).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM video_transcode_jobs WHERE id = $1`, jobID) })

	shape := func(id string) (w, h, ms int) {
		db.QueryRow(`SELECT video_width, video_height, video_duration_ms FROM posts WHERE id = $1`, id).Scan(&w, &h, &ms)
		return
	}

	mine := mkVideoMetaPost(t, db, author, url, 0, 0, 0)
	s.applyUploadedVideoMeta(mine, author, url)
	if w, h, ms := shape(mine); w != 1080 || h != 1920 || ms != 42000 {
		t.Errorf("own upload: got %d/%d/%d, want 1080/1920/42000", w, h, ms)
	}

	// someone else posting the same URL does not get another user's numbers
	theirs := mkVideoMetaPost(t, db, other, url, 0, 0, 0)
	s.applyUploadedVideoMeta(theirs, other, url)
	if w, h, ms := shape(theirs); w != 0 || h != 0 || ms != 0 {
		t.Errorf("another user's post got %d/%d/%d from a job that isn't theirs", w, h, ms)
	}

	// a URL no job made (an external link) is left alone
	ext := mkVideoMetaPost(t, db, author, "https://elsewhere.example/v.mp4", 0, 0, 0)
	s.applyUploadedVideoMeta(ext, author, "https://elsewhere.example/v.mp4")
	if w, _, _ := shape(ext); w != 0 {
		t.Errorf("external URL got a width of %d", w)
	}

	// and an unfinished job contributes nothing
	db.Exec(`UPDATE video_transcode_jobs SET status = 'processing' WHERE id = $1`, jobID)
	pending := mkVideoMetaPost(t, db, author, url, 0, 0, 0)
	s.applyUploadedVideoMeta(pending, author, url)
	if w, _, _ := shape(pending); w != 0 {
		t.Errorf("unfinished job gave a width of %d", w)
	}
}
