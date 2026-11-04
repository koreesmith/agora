package media

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

func TestLimitText(t *testing.T) {
	for sec, want := range map[float64]string{120: "2 minutes", 60: "1 minute", 90: "90 seconds", 5: "5 seconds", 180: "3 minutes"} {
		if got := limitText(sec); got != want {
			t.Errorf("limitText(%v) = %q, want %q", sec, got, want)
		}
	}
}

func TestByteText(t *testing.T) {
	for n, want := range map[int64]string{512 * 1024: "512 KB", 5 << 20: "5 MB", 3 << 30: "3.0 GB", 1536 << 20: "1.5 GB"} {
		if got := byteText(n); got != want {
			t.Errorf("byteText(%d) = %q, want %q", n, got, want)
		}
	}
}

// The limit message follows the admin's number, not a fixed 2 minutes.
func TestParseVideoEditUsesTheGivenLimit(t *testing.T) {
	_, err := parseVideoEdit("", "", "", 100, 90)
	if err == nil || !strings.Contains(err.Error(), "max 90 seconds — this video is 100 seconds") {
		t.Errorf("untrimmed over a 90s limit: %v", err)
	}
	if _, err := parseVideoEdit("0", "85000", "", 100, 90); err != nil {
		t.Errorf("trimmed to fit a 90s limit was refused: %v", err)
	}
	_, err = parseVideoEdit("0", "95000", "", 100, 90)
	if err == nil || !strings.Contains(err.Error(), "the part you kept is 95 seconds") {
		t.Errorf("trimmed but still over: %v", err)
	}
}

// Only this instance's own uploaded videos can be removed, and nothing outside
// the videos directory can be reached.
func TestRemoveVideoFiles(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "videos"), 0755)
	os.MkdirAll(filepath.Join(dir, "posts"), 0755)
	mk := func(rel string) string {
		p := filepath.Join(dir, rel)
		os.WriteFile(p, []byte("x"), 0644)
		return p
	}
	video, thumb := mk("videos/a.mp4"), mk("videos/a_thumb.jpg")
	image := mk("posts/pic.jpg")
	outside := filepath.Join(filepath.Dir(dir), "outside-"+filepath.Base(dir))
	os.WriteFile(outside, []byte("x"), 0644)
	t.Cleanup(func() { os.Remove(outside) })

	s := &Service{uploadDir: dir}
	n := s.RemoveVideoFiles(
		"/uploads/videos/a.mp4", "/uploads/videos/a_thumb.jpg",
		"/uploads/posts/pic.jpg",                            // an image, not a video
		"https://elsewhere.example/v.mp4",                   // remote
		"https://video.bsky.app/x/playlist.m3u8",            // a Bluesky playlist
		"/uploads/videos/../../outside-"+filepath.Base(dir), // trying to leave the directory
		"/uploads/videos/sub/../a.mp4",                      // not a plain file name
		"",
	)
	if n != 2 {
		t.Errorf("removed %d files, want 2 (the video and its thumbnail)", n)
	}
	for _, p := range []string{video, thumb} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was not removed", p)
		}
	}
	for _, p := range []string{image, outside} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s must be left alone: %v", p, err)
		}
	}
	// removing what is already gone is not an error
	if n := s.RemoveVideoFiles("/uploads/videos/a.mp4"); n != 0 {
		t.Errorf("removing a missing file counted %d", n)
	}
}

func openLimitsDB(t *testing.T) *store.DB {
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

func setSetting(t *testing.T, db *store.DB, key, value string) {
	t.Helper()
	var prev string
	had := db.QueryRow(`SELECT value FROM instance_settings WHERE key = $1`, key).Scan(&prev) == nil
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = $2`, key, value)
	t.Cleanup(func() {
		if had {
			db.Exec(`UPDATE instance_settings SET value = $2 WHERE key = $1`, key, prev)
		} else {
			db.Exec(`DELETE FROM instance_settings WHERE key = $1`, key)
		}
	})
}

func TestMaxVideoSecondsSetting(t *testing.T) {
	db := openLimitsDB(t)
	s := &Service{db: db}
	for value, want := range map[string]float64{
		"120": 120, "90": 90, "5": 5,
		"4": 120, "0": 120, "-3": 120, "121": 120, "9999": 120, "": 120, "soon": 120, " 60 ": 60,
	} {
		setSetting(t, db, "video_max_seconds", value)
		if got := s.maxVideoSeconds(); got != want {
			t.Errorf("video_max_seconds=%q gave %v, want %v", value, got, want)
		}
	}
}

// A new instance (and an upgraded one) starts at the old behaviour.
func TestVideoLimitDefaults(t *testing.T) {
	db := openLimitsDB(t)
	var secs, quota string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'video_max_seconds'`).Scan(&secs)
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'video_quota_mb'`).Scan(&quota)
	if secs != "120" || quota != "0" {
		t.Errorf("defaults are %q seconds and %q MB, want 120 and 0 (no quota)", secs, quota)
	}
}

func TestVideoQuota(t *testing.T) {
	db := openLimitsDB(t)
	s := &Service{db: db}
	name := fmt.Sprintf("q444_%d", time.Now().UnixNano())
	var userID, other string
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`, name, name+"@x.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`, name+"o", name+"o@x.invalid").Scan(&other)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id IN ($1, $2)`, userID, other) })
	post := func(author string, bytes int64) string {
		var id string
		db.QueryRow(`INSERT INTO posts (author_id, content, visibility, video_url, video_bytes) VALUES ($1, 'v', 'public', '/uploads/videos/q.mp4', $2) RETURNING id`, author, bytes).Scan(&id)
		return id
	}

	// off by default: any amount is fine
	setSetting(t, db, "video_quota_mb", "0")
	post(userID, 500<<20)
	if err := s.videoQuotaError(userID); err != nil {
		t.Errorf("quota off but refused: %v", err)
	}

	// on: under, at and over
	setSetting(t, db, "video_quota_mb", "100")
	if err := s.videoQuotaError(userID); err == nil {
		t.Error("500 MB used of a 100 MB quota was allowed")
	} else if !strings.Contains(err.Error(), "500 MB") || !strings.Contains(err.Error(), "100 MB") {
		t.Errorf("the message should say what is used and the limit: %v", err)
	}
	if err := s.videoQuotaError(other); err != nil {
		t.Errorf("another account with nothing used was refused: %v", err)
	}

	// deleting a video makes room again, and only live posts count
	db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE author_id = $1`, userID)
	if err := s.videoQuotaError(userID); err != nil {
		t.Errorf("after deleting the video the account is still refused: %v", err)
	}
	post(userID, 60<<20)
	if err := s.videoQuotaError(userID); err != nil {
		t.Errorf("60 MB of 100 MB refused: %v", err)
	}
	post(userID, 60<<20)
	if err := s.videoQuotaError(userID); err == nil {
		t.Error("120 MB of a 100 MB quota was allowed")
	}
}

// The quota is enforced before the upload is read: an account at its limit is
// turned away even with nothing attached.
func TestStartVideoJobRefusesAnAccountAtItsQuota(t *testing.T) {
	db := openLimitsDB(t)
	s := &Service{db: db, uploadDir: t.TempDir()}
	name := fmt.Sprintf("qj444_%d", time.Now().UnixNano())
	var userID string
	db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`, name, name+"@x.invalid").Scan(&userID)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
	db.Exec(`INSERT INTO posts (author_id, content, visibility, video_url, video_bytes) VALUES ($1, 'v', 'public', '/uploads/videos/z.mp4', $2)`, userID, 5<<20)
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE author_id = $1`, userID) })
	setSetting(t, db, "video_quota_mb", "1")

	req := httptest.NewRequest("POST", "/media/upload?category=videos", strings.NewReader(""))
	_, err := s.StartVideoJob(req, userID)
	if err == nil || !strings.Contains(err.Error(), "video storage") {
		t.Fatalf("got %v, want the video storage message", err)
	}

	// with the quota off, the same request gets as far as complaining about the
	// missing file, which is the usual first complaint
	setSetting(t, db, "video_quota_mb", "0")
	if _, err := s.StartVideoJob(httptest.NewRequest("POST", "/media/upload?category=videos", strings.NewReader("")), userID); err == nil || !strings.Contains(err.Error(), "no file attached") {
		t.Errorf("quota off: got %v, want the no-file message", err)
	}
}
