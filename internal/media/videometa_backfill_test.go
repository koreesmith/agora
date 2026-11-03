package media

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-437: the startup backfill fills in videos posted before their shape
// was recorded, leaves posts that already have one alone, skips files it
// can't read, and is safe to run twice.
//
// Needs ffmpeg (to make a sample video; MPEG-4 is used because every build has
// it, unlike libx264) and the local agora-postgres-test
// instance (localhost:15433); skips if either is missing.
func TestBackfillVideoMeta(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("skipping: ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("skipping: ffprobe not installed")
	}
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "videos"), 0755)
	name := fmt.Sprintf("bf437_%d", time.Now().UnixNano())
	good := filepath.Join(dir, "videos", name+".mp4")
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=360x640:rate=24",
		"-t", "2", "-pix_fmt", "yuv420p", "-c:v", "mpeg4", "-y", good).CombinedOutput(); err != nil {
		t.Fatalf("make sample video: %v %s", err, out)
	}
	bad := filepath.Join(dir, "videos", name+"_bad.mp4")
	os.WriteFile(bad, []byte("not a video"), 0644)

	var userID string
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		name, name+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	mk := func(url string, w int) string {
		var id string
		if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility, video_url, video_width, video_height, video_duration_ms)
			VALUES ($1, 'x', 'public', $2, $3, $3, $3) RETURNING id`, userID, url, w).Scan(&id); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		return id
	}
	old := mk("/uploads/videos/"+name+".mp4", 0)
	broken := mk("/uploads/videos/"+name+"_bad.mp4", 0)
	missing := mk("/uploads/videos/"+name+"_gone.mp4", 0)
	already := mk("/uploads/videos/"+name+".mp4", 7)
	remote := mk("https://elsewhere.example/v.mp4", 0)

	svc := &Service{db: db, uploadDir: dir}
	svc.BackfillVideoMeta()
	svc.BackfillVideoMeta() // running it again changes nothing

	shape := func(id string) (w, h, ms int) {
		db.QueryRow(`SELECT video_width, video_height, video_duration_ms FROM posts WHERE id = $1`, id).Scan(&w, &h, &ms)
		return
	}
	if w, h, ms := shape(old); w != 360 || h != 640 || ms < 1900 || ms > 2100 {
		t.Errorf("old video: got %dx%d %dms, want 360x640 ~2000ms", w, h, ms)
	}
	for id, what := range map[string]string{broken: "unreadable file", missing: "missing file", remote: "remote video"} {
		if w, _, _ := shape(id); w != 0 {
			t.Errorf("%s was given a width of %d", what, w)
		}
	}
	if w, h, ms := shape(already); w != 7 || h != 7 || ms != 7 {
		t.Errorf("a post that already had a shape was changed to %d/%d/%d", w, h, ms)
	}
}
