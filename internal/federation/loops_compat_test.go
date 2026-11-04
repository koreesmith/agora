package federation

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
)

// AGORA-445: compatibility with Loops, the federated short-video app.
//
// These do not talk to a Loops server: that needs a Loops instance and an
// Agora one both reachable from the internet over HTTPS. They check Agora's code
// against what Loops documents (FEDERATION.md in github.com/joinloops/loops-server,
// and the "Loops Joins the Fediverse" post):
//
//   - a video is a Create of a Note whose attachment is
//     {"type":"Document","mediaType":"video/mp4","url":...}
//   - replies are Notes with inReplyTo, likes are Like, shares Announce
//   - requests are signed with draft-cavage HTTP signatures from RSA keys of at
//     least 2048 bits, which is what Agora does (httpsig.go, activitypub.go)

// The example outbound Create from Loops' FEDERATION.md, with ids filled in.
const loopsCreateExample = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://loops.example.com/ap/users/maya/activities/1",
  "type": "Create",
  "actor": "https://loops.example.com/ap/users/maya",
  "object": {
    "id": "https://loops.example.com/ap/users/maya/statuses/42",
    "type": "Note",
    "attributedTo": "https://loops.example.com/ap/users/maya",
    "content": "<p>my first loop</p>",
    "published": "2026-11-03T12:00:00Z",
    "to": ["https://www.w3.org/ns/activitystreams#Public"],
    "attachment": {"type": "Document", "mediaType": "video/mp4", "url": "https://loops.example.com/storage/videos/123.mp4"}
  }
}`

// What a Loops video looks like to Agora's inbound code: Create > Note with a
// Document attachment of type video/mp4. In Loops' own example the attachment
// is a single object rather than an array, which a plain []apAttachment cannot
// decode, so Agora would have dropped the whole post.
func TestLoopsCreateIsReadAsAVideoPost(t *testing.T) {
	var act struct {
		Object struct {
			Attachment apAttachmentList `json:"attachment"`
		} `json:"object"`
	}
	if err := json.Unmarshal([]byte(loopsCreateExample), &act); err != nil {
		t.Fatalf("a Loops Create does not decode: %v", err)
	}
	images, videoURL := matchAttachments(act.Object.Attachment)
	if videoURL != "https://loops.example.com/storage/videos/123.mp4" || len(images) != 0 {
		t.Errorf("video %q images %v, want the mp4 and no images", videoURL, images)
	}
	if m := matchVideoMeta(act.Object.Attachment); m.known() {
		t.Errorf("Loops documents no width, height or duration, so none should be invented: %+v", m)
	}
}

// An attachment may be absent, null, one object or an array: all decode, and
// none of them can fail the note.
func TestAttachmentListAcceptsEveryShape(t *testing.T) {
	cases := map[string]int{
		`{}`:                  0,
		`{"attachment":null}`: 0,
		`{"attachment":[]}`:   0,
		`{"attachment":{"type":"Document","mediaType":"video/mp4","url":"https://x/v.mp4"}}`:                 1,
		`{"attachment":[{"type":"Document","mediaType":"video/mp4","url":"https://x/v.mp4"}]}`:               1,
		`{"attachment":[{"type":"Image","url":"https://x/a.jpg"},{"type":"Image","url":"https://x/b.jpg"}]}`: 2,
	}
	for in, want := range cases {
		var n struct {
			Attachment apAttachmentList `json:"attachment"`
		}
		if err := json.Unmarshal([]byte(in), &n); err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if len(n.Attachment) != want {
			t.Errorf("%s: %d attachments, want %d", in, len(n.Attachment), want)
		}
	}
	// a malformed attachment is still an error, not silently dropped
	var bad struct {
		Attachment apAttachmentList `json:"attachment"`
	}
	if err := json.Unmarshal([]byte(`{"attachment":"https://x/v.mp4"}`), &bad); err == nil {
		t.Error("a bare string attachment decoded without error")
	}
}

// Loops mentions several encodings of one video. Agora keeps the first.
func TestLoopsSeveralEncodingsKeepsTheFirst(t *testing.T) {
	atts := []apAttachment{
		{Type: "Document", MediaType: "video/mp4", URL: "https://loops.example.com/storage/videos/123_720.mp4"},
		{Type: "Document", MediaType: "video/mp4", URL: "https://loops.example.com/storage/videos/123_360.mp4"},
		{Type: "Document", MediaType: "image/jpeg", URL: "https://loops.example.com/storage/thumbs/123.jpg"},
	}
	images, videoURL := matchAttachments(atts)
	if videoURL != atts[0].URL {
		t.Errorf("video = %q, want the first encoding", videoURL)
	}
	if len(images) != 1 {
		t.Errorf("images = %v, want the one thumbnail", images)
	}
}

// What Agora sends is what Agora reads: a reel goes out as the Note shape Loops
// documents, and the same JSON read back through the inbound decoding yields the
// video. This is the check that the outgoing change in AGORA-445 did not break
// Agora-to-Agora federation, which keys on mediaType.
func TestAgoraVideoNoteMatchesLoopsShapeAndRoundTrips(t *testing.T) {
	db := openMetaTestDB(t)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora445.example"}}
	_, postID := mkMetaPost(t, db, "loops", "/uploads/videos/reel.mp4", 720, 1280, 30000)
	db.Exec(`UPDATE posts SET video_thumb_url = '/uploads/videos/reel_thumb.jpg' WHERE id = $1`, postID)

	note := s.buildNoteObject("https://agora445.example/users/x", postID, "a reel", time.Now(), "", "")
	if note["type"] != "Note" {
		t.Errorf("object type = %v, want Note (Loops reads Notes, not Video objects)", note["type"])
	}
	atts, _ := note["attachment"].([]map[string]any)
	if len(atts) != 1 {
		t.Fatalf("attachment = %#v", note["attachment"])
	}
	a := atts[0]
	if a["type"] != "Document" || a["mediaType"] != "video/mp4" || a["url"] != "https://agora445.example/uploads/videos/reel.mp4" {
		t.Errorf("attachment = %v, want the Document/video/mp4 shape Loops documents", a)
	}

	// round trip through the JSON an inbox would receive
	b, _ := json.Marshal(note)
	var back struct {
		Attachment []apAttachment `json:"attachment"`
	}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("our own note does not decode: %v", err)
	}
	if _, v := matchAttachments(back.Attachment); v != "https://agora445.example/uploads/videos/reel.mp4" {
		t.Errorf("video read back = %q", v)
	}
	if m := matchVideoMeta(back.Attachment); m.Width != 720 || m.Height != 1280 || m.DurationMs != 30000 {
		t.Errorf("shape read back = %+v, want 720x1280, 30s", m)
	}
}
