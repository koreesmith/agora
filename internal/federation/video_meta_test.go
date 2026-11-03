package federation

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

func TestAPDurationMs(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{`"PT1M5S"`, 65000},
		{`"PT0H5M0S"`, 300000},
		{`"PT1H"`, 3600000},
		{`"PT12.5S"`, 12500},
		{`"pt30s"`, 30000},
		{`42`, 42000},
		{`12.5`, 12500},
		{`"90"`, 90000},
		{`""`, 0},
		{`"soon"`, 0},
		{`"PT"`, 0},
		{`-5`, 0},
		{`"P2DT1H"`, 0}, // longer than a day: not a short video, treated as unknown
		{`null`, 0},
		{`{}`, 0},
	}
	for _, c := range cases {
		if got := apDurationMs(json.RawMessage(c.in)); got != c.want {
			t.Errorf("apDurationMs(%s) = %d, want %d", c.in, got, c.want)
		}
	}
	if got := apDurationMs(nil); got != 0 {
		t.Errorf("apDurationMs(nil) = %d, want 0", got)
	}
}

func TestAPInt(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{`1080`, 1080}, {`1080.4`, 1080}, {`"720"`, 720}, {`" 720 "`, 720},
		{`0`, 0}, {`-1`, 0}, {`"wide"`, 0}, {`null`, 0}, {`[1]`, 0}, {`99999999`, 0},
	}
	for _, c := range cases {
		if got := apInt(json.RawMessage(c.in)); got != c.want {
			t.Errorf("apInt(%s) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestIsoDurationFromMs(t *testing.T) {
	cases := map[int]string{0: "PT0S", 500: "PT1S", 65000: "PT1M5S", 3600000: "PT1H0M0S", 3725000: "PT1H2M5S", 59000: "PT59S"}
	for ms, want := range cases {
		if got := isoDurationFromMs(ms); got != want {
			t.Errorf("isoDurationFromMs(%d) = %q, want %q", ms, got, want)
		}
		// what we write must be what we read
		if ms >= 1000 {
			if back := apDurationMs(json.RawMessage(fmt.Sprintf("%q", isoDurationFromMs(ms)))); back != ms {
				t.Errorf("round trip of %d came back as %d", ms, back)
			}
		}
	}
}

// A Note whose width, height or duration is an unexpected type must still
// decode, and must not stop its video from being found.
func TestAttachmentDecodesOddSizeFields(t *testing.T) {
	var note struct {
		Attachment []apAttachment `json:"attachment"`
	}
	raw := `{"attachment":[
		{"type":"Document","mediaType":"video/mp4","url":"https://a.example/v.mp4","width":1080,"height":1920,"duration":"PT30S"},
		{"type":"Video","url":"https://b.example/w.mp4","width":"wide","height":{"x":1},"duration":[1]}
	]}`
	if err := json.Unmarshal([]byte(raw), &note); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	m := matchVideoMeta(note.Attachment)
	if m != (videoMeta{1080, 1920, 30000}) {
		t.Errorf("first video: got %+v", m)
	}
	// the second one alone: odd values read as unknown, the video is still a video
	odd := matchVideoMeta(note.Attachment[1:])
	if odd.known() {
		t.Errorf("odd values should read as unknown, got %+v", odd)
	}
	if _, url := matchAttachments(note.Attachment[1:]); url != "https://b.example/w.mp4" {
		t.Errorf("video url not matched, got %q", url)
	}
}

// matchVideoMeta describes the same video matchAttachments picks, even with
// images in front and a second video behind.
func TestMatchVideoMetaPicksTheMatchedVideo(t *testing.T) {
	atts := []apAttachment{
		{Type: "Image", MediaType: "image/jpeg", URL: "https://x/i.jpg", Width: json.RawMessage(`10`), Height: json.RawMessage(`10`)},
		{Type: "Video", MediaType: "video/mp4", URL: "https://x/1.mp4", Width: json.RawMessage(`720`), Height: json.RawMessage(`1280`)},
		{Type: "Video", MediaType: "video/mp4", URL: "https://x/2.mp4", Width: json.RawMessage(`1920`), Height: json.RawMessage(`1080`)},
	}
	if _, u := matchAttachments(atts); u != "https://x/1.mp4" {
		t.Fatalf("matchAttachments picked %q", u)
	}
	if m := matchVideoMeta(atts); m.Width != 720 || m.Height != 1280 {
		t.Errorf("got %+v, want the first video's 720x1280", m)
	}
	if m := matchVideoMeta(atts[:1]); m.known() {
		t.Errorf("an image-only post has no video shape, got %+v", m)
	}
}

func openMetaTestDB(t *testing.T) *store.DB {
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

func mkMetaPost(t *testing.T, db *store.DB, tag, videoURL string, w, h, ms int) (userID, postID string) {
	t.Helper()
	name := fmt.Sprintf("vm437_%s_%d", tag, time.Now().UnixNano())
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		name, name+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })
	if err := db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, video_url, video_thumb_url, video_width, video_height, video_duration_ms)
		VALUES ($1, 'clip', 'public', $2, '', $3, $4, $5) RETURNING id`, userID, videoURL, w, h, ms).Scan(&postID); err != nil {
		t.Fatalf("insert post: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, postID) })
	return
}

// The video attachment on an outgoing Note carries the video's size and length.
func TestBuildNoteObjectCarriesVideoShape(t *testing.T) {
	db := openMetaTestDB(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://vm437.example"}}
	userID, postID := mkMetaPost(t, db, "out", "/uploads/videos/a.mp4", 720, 1280, 65000)
	_ = userID

	note := s.buildNoteObject("https://vm437.example/users/x", postID, "clip", time.Now(), "", "")
	atts, _ := note["attachment"].([]map[string]any)
	if len(atts) != 1 {
		t.Fatalf("attachment = %#v", note["attachment"])
	}
	a := atts[0]
	if a["width"] != 720 || a["height"] != 1280 {
		t.Errorf("width/height = %v/%v, want 720/1280", a["width"], a["height"])
	}
	if a["duration"] != "PT1M5S" {
		t.Errorf("duration = %v, want PT1M5S", a["duration"])
	}

	// unknown shape: the attachment is exactly what it was before this change
	_, plainID := mkMetaPost(t, db, "outplain", "/uploads/videos/b.mp4", 0, 0, 0)
	note = s.buildNoteObject("https://vm437.example/users/x", plainID, "clip", time.Now(), "", "")
	atts, _ = note["attachment"].([]map[string]any)
	if len(atts) != 1 {
		t.Fatalf("attachment = %#v", note["attachment"])
	}
	for _, k := range []string{"width", "height", "duration"} {
		if _, has := atts[0][k]; has {
			t.Errorf("attachment has %q for a video of unknown shape", k)
		}
	}
}

func TestStoreInboundVideoMeta(t *testing.T) {
	db := openMetaTestDB(t)
	s := &Service{db: db, cfg: &config.Config{}}
	_, postID := mkMetaPost(t, db, "in", "https://remote.example/v.mp4", 0, 0, 0)

	s.storeInboundVideoMeta(postID, videoMeta{})
	var w, h, ms int
	db.QueryRow(`SELECT video_width, video_height, video_duration_ms FROM posts WHERE id = $1`, postID).Scan(&w, &h, &ms)
	if w != 0 || h != 0 || ms != 0 {
		t.Errorf("unknown shape wrote %d/%d/%d", w, h, ms)
	}

	s.storeInboundVideoMeta(postID, videoMeta{Width: 1080, Height: 1920, DurationMs: 30000})
	db.QueryRow(`SELECT video_width, video_height, video_duration_ms FROM posts WHERE id = $1`, postID).Scan(&w, &h, &ms)
	if w != 1080 || h != 1920 || ms != 30000 {
		t.Errorf("got %d/%d/%d, want 1080/1920/30000", w, h, ms)
	}
}
