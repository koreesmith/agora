package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

func TestValidateSetting(t *testing.T) {
	ok := map[string][]string{
		"reels_discover_enabled": {"true", "false"},
		"video_max_seconds":      {"5", "90", "120", " 60 "},
		"video_quota_mb":         {"0", "500", "10000000"},
		"instance_name":          {"anything at all", ""},
	}
	bad := map[string][]string{
		"reels_discover_enabled": {"yes", "", "1", "TRUE"},
		"video_max_seconds":      {"4", "121", "0", "-1", "", "two minutes", "90.5"},
		"video_quota_mb":         {"-1", "10000001", "", "lots", "1.5"},
	}
	for k, vs := range ok {
		for _, v := range vs {
			if err := validateSetting(k, v); err != nil {
				t.Errorf("%s=%q refused: %v", k, v, err)
			}
		}
	}
	for k, vs := range bad {
		for _, v := range vs {
			if err := validateSetting(k, v); err == nil {
				t.Errorf("%s=%q was accepted", k, v)
			}
		}
	}
}

type fakeMedia struct{ dir string }

func (f fakeMedia) UploadDir() string { return f.dir }

func openAdminDB(t *testing.T) *store.DB {
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

// A bad value is refused with 400 and nothing in the request is applied, so a
// typo can't leave the instance half updated.
func TestUpdateSettingsRefusesBadVideoValues(t *testing.T) {
	db := openAdminDB(t)
	s := &Service{db: db, cfg: &config.Config{}, media: fakeMedia{t.TempDir()}}
	get := func(k string) string {
		v := ""
		db.QueryRow(`SELECT value FROM instance_settings WHERE key = $1`, k).Scan(&v)
		return v
	}
	prevName, prevMax := get("instance_name"), get("video_max_seconds")
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'instance_name'`, prevName)
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'video_max_seconds'`, prevMax)
	})

	patch := func(body map[string]string) int {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/admin/settings", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.UpdateSettings(w, req)
		return w.Code
	}
	if code := patch(map[string]string{"instance_name": "Half Updated", "video_max_seconds": "9000"}); code != 400 {
		t.Errorf("a bad video limit got %d, want 400", code)
	}
	if got := get("instance_name"); got != prevName {
		t.Errorf("instance_name was changed to %q even though the request was refused", got)
	}
	if code := patch(map[string]string{"video_max_seconds": "90", "video_quota_mb": "250", "reels_discover_enabled": "false"}); code != 200 {
		t.Fatalf("a good request got %d", code)
	}
	if get("video_max_seconds") != "90" || get("video_quota_mb") != "250" || get("reels_discover_enabled") != "false" {
		t.Errorf("settings not saved: %q %q %q", get("video_max_seconds"), get("video_quota_mb"), get("reels_discover_enabled"))
	}
	db.Exec(`UPDATE instance_settings SET value = '0' WHERE key = 'video_quota_mb'`)
	db.Exec(`UPDATE instance_settings SET value = 'true' WHERE key = 'reels_discover_enabled'`)

	// and the settings API hands them back to the admin screen
	req := httptest.NewRequest("GET", "/admin/settings", nil)
	w := httptest.NewRecorder()
	s.GetSettings(w, req)
	var all map[string]string
	json.Unmarshal(w.Body.Bytes(), &all)
	for _, k := range []string{"video_max_seconds", "video_quota_mb", "reels_discover_enabled"} {
		if _, ok := all[k]; !ok {
			t.Errorf("GET /admin/settings does not return %s", k)
		}
	}
}

// Video storage is reported apart from images, a video counted once (its
// thumbnail is not a second video), and the remote media cache left out.
func TestMediaStorageReport(t *testing.T) {
	db := openAdminDB(t)
	dir := t.TempDir()
	write := func(rel string, n int) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0755)
		os.WriteFile(p, make([]byte, n), 0644)
	}
	write("videos/a.mp4", 1000)
	write("videos/a_thumb.jpg", 100)
	write("videos/b.mp4", 2000)
	write("posts/p1.jpg", 300)
	write("posts/p2.jpg", 400)
	write("avatar/u.jpg", 50)

	s := &Service{db: db, cfg: &config.Config{}, media: fakeMedia{dir}}
	rep := s.computeStorage()
	if rep.Video.Count != 2 || rep.Video.Bytes != 3100 {
		t.Errorf("video: %d files, %d bytes; want 2 and 3100", rep.Video.Count, rep.Video.Bytes)
	}
	if rep.Images.Count != 3 || rep.Images.Bytes != 750 {
		t.Errorf("images: %d files, %d bytes; want 3 and 750", rep.Images.Count, rep.Images.Bytes)
	}
	if len(rep.Categories) != 3 || rep.Categories[0].Name != "videos" {
		t.Errorf("categories should be biggest first, got %+v", rep.Categories)
	}

	// the top accounts come from the sizes recorded on live posts
	name := fmt.Sprintf("st444_%d", time.Now().UnixNano())
	var uid string
	db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`, name, name+"@x.invalid").Scan(&uid)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, uid) })
	db.Exec(`INSERT INTO posts (author_id, content, visibility, video_url, video_bytes) VALUES ($1, 'a', 'public', '/uploads/videos/a.mp4', 900000000000)`, uid)
	var deleted string
	db.QueryRow(`INSERT INTO posts (author_id, content, visibility, video_url, video_bytes, deleted_at) VALUES ($1, 'b', 'public', '/uploads/videos/b.mp4', 5, NOW()) RETURNING id`, uid).Scan(&deleted)
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE author_id = $1`, uid) })
	rep = s.computeStorage()
	if len(rep.TopVideoUsers) == 0 || rep.TopVideoUsers[0].Username != name || rep.TopVideoUsers[0].Videos != 1 || rep.TopVideoUsers[0].Bytes != 900000000000 {
		t.Errorf("top video users: %+v", rep.TopVideoUsers)
	}
	if rep.MaxSeconds != 120 {
		t.Errorf("max seconds %d, want the default 120", rep.MaxSeconds)
	}
}

// AGORA-431: a file a saved draft points to is still wanted, so the cleanup of
// unused media leaves it alone.
func TestDraftMediaIsNotOrphaned(t *testing.T) {
	db := openAdminDB(t)
	name := fmt.Sprintf("dm431_%d", time.Now().UnixNano())
	var uid string
	db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`, name, name+"@x.invalid").Scan(&uid)
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, uid) })
	url := fmt.Sprintf("/uploads/posts/draft-%d.jpg", time.Now().UnixNano())
	db.Exec(`INSERT INTO post_drafts (user_id, content, media_urls) VALUES ($1, 'x', ARRAY[$2]::text[])`, uid, url)

	s := &Service{db: db, cfg: &config.Config{}, media: fakeMedia{t.TempDir()}}
	refs, err := s.collectReferencedURLs()
	if err != nil {
		t.Fatal(err)
	}
	if !refs[url] {
		t.Errorf("a draft's photo %s is not counted as in use", url)
	}
	db.Exec(`DELETE FROM post_drafts WHERE user_id = $1`, uid)
	refs, _ = s.collectReferencedURLs()
	if refs[url] {
		t.Error("with the draft gone the photo should be unreferenced again")
	}
}
