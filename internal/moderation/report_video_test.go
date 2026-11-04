package moderation

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-444: a reported video has to be watchable from the moderation queue,
// so the report carries the post's video and thumbnail.
func TestReportListCarriesTheVideo(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	n := time.Now().UnixNano()
	mk := func(tag string) string {
		var id string
		name := fmt.Sprintf("rv444_%s_%d", tag, n)
		if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`, name, name+"@x.invalid").Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	reporter, author := mk("reporter"), mk("author")
	var videoPost, textPost string
	db.QueryRow(`INSERT INTO posts (author_id, content, visibility, video_url, video_thumb_url) VALUES ($1, 'a reel', 'public', '/uploads/videos/r.mp4', '/uploads/videos/r_thumb.jpg') RETURNING id`, author).Scan(&videoPost)
	db.QueryRow(`INSERT INTO posts (author_id, content, visibility) VALUES ($1, 'just words', 'public') RETURNING id`, author).Scan(&textPost)
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE author_id = $1`, author) })
	for _, pid := range []string{videoPost, textPost} {
		db.Exec(`INSERT INTO reports (reporter_id, reported_post_id, violation_type, details, status) VALUES ($1, $2, 'spam', 'x', 'pending')`, reporter, pid)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM reports WHERE reporter_id = $1`, reporter) })

	s := &Service{db: db}
	w := httptest.NewRecorder()
	s.ListReports(w, httptest.NewRequest("GET", "/moderation/reports?status=pending", nil))
	var out struct {
		Reports []struct {
			ReportedPostID    *string `json:"reported_post_id"`
			PostVideoURL      *string `json:"post_video_url"`
			PostVideoThumbURL *string `json:"post_video_thumb_url"`
		} `json:"reports"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, w.Body.String())
	}
	var sawVideo, sawText bool
	for _, r := range out.Reports {
		if r.ReportedPostID == nil {
			continue
		}
		switch *r.ReportedPostID {
		case videoPost:
			sawVideo = true
			if r.PostVideoURL == nil || *r.PostVideoURL != "/uploads/videos/r.mp4" || r.PostVideoThumbURL == nil || *r.PostVideoThumbURL != "/uploads/videos/r_thumb.jpg" {
				t.Errorf("the reported video's URLs are missing: %v %v", r.PostVideoURL, r.PostVideoThumbURL)
			}
		case textPost:
			sawText = true
			if r.PostVideoURL != nil && *r.PostVideoURL != "" {
				t.Errorf("a text post reported a video: %q", *r.PostVideoURL)
			}
		}
	}
	if !sawVideo || !sawText {
		t.Errorf("both reports should be listed (video %v, text %v)", sawVideo, sawText)
	}
}
