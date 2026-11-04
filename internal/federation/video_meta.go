package federation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// videoMeta (AGORA-437) is a video's width, height and length as a remote
// server described them. Zero means it didn't say.
type videoMeta struct {
	Width      int
	Height     int
	DurationMs int
}

func (m videoMeta) known() bool { return m.Width > 0 || m.Height > 0 || m.DurationMs > 0 }

// matchVideoMeta returns the shape of the attachment matchAttachments picked
// as the post's video (the first video, by the same test), so the two always
// describe the same file.
func matchVideoMeta(attachments []apAttachment) videoMeta {
	for _, a := range attachments {
		if isImageAttachment(a) {
			continue
		}
		if (strings.HasPrefix(a.MediaType, "video/") || a.Type == "Video") && a.URL != "" {
			return videoMeta{
				Width:      apInt(a.Width),
				Height:     apInt(a.Height),
				DurationMs: apDurationMs(a.Duration),
			}
		}
	}
	return videoMeta{}
}

// apInt reads a number that remote servers send as an integer, a float or
// (rarely) a string, and returns 0 for anything else. These fields are
// optional and untrusted, and one odd value must never fail the whole Note.
func apInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return clampInt(f)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return clampInt(f)
		}
	}
	return 0
}

// clampInt keeps a remote-supplied number inside a sane range so it can't
// overflow the column or describe an impossible video.
func clampInt(f float64) int {
	if math.IsNaN(f) || f <= 0 || f > 100000 {
		return 0
	}
	return int(math.Round(f))
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?T?(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?$`)

// apDurationMs reads an ActivityStreams duration: an ISO 8601 string such as
// "PT1M5S" (what PeerTube and the spec use) or a plain number of seconds.
// Returns 0 when it is missing, malformed, or longer than a day.
func apDurationMs(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var secs float64
	var s string
	switch {
	case json.Unmarshal(raw, &secs) == nil:
	case json.Unmarshal(raw, &s) == nil:
		s = strings.TrimSpace(s)
		if m := isoDuration.FindStringSubmatch(strings.ToUpper(s)); m != nil && s != "" {
			for i, unit := range []float64{86400, 3600, 60, 1} {
				if m[i+1] != "" {
					v, _ := strconv.ParseFloat(m[i+1], 64)
					secs += v * unit
				}
			}
		} else if f, err := strconv.ParseFloat(s, 64); err == nil {
			secs = f
		}
	default:
		return 0
	}
	if math.IsNaN(secs) || secs <= 0 || secs > 86400 {
		return 0
	}
	return int(math.Round(secs * 1000))
}

// isoDurationFromMs renders a length the way ActivityStreams writes it,
// e.g. 65000 -> "PT1M5S".
func isoDurationFromMs(ms int) string {
	total := int(math.Round(float64(ms) / 1000))
	h, m, s := total/3600, (total%3600)/60, total%60
	out := "PT"
	if h > 0 {
		out += fmt.Sprintf("%dH", h)
	}
	if h > 0 || m > 0 {
		out += fmt.Sprintf("%dM", m)
	}
	return out + fmt.Sprintf("%dS", s)
}

// storeInboundVideoMeta records a remote video's shape on the post, when the
// remote server gave one (AGORA-437). Called alongside storeInboundVideo.
func (s *Service) storeInboundVideoMeta(postID string, m videoMeta) {
	if !m.known() {
		return
	}
	s.db.Exec(`UPDATE posts SET video_width = $2, video_height = $3, video_duration_ms = $4 WHERE id = $1`,
		postID, m.Width, m.Height, m.DurationMs)
}

// postVideoShape returns a local post's recorded video size and length, for
// the outgoing attachment. Zeros when unknown.
func (s *Service) postVideoShape(postID string) videoMeta {
	var m videoMeta
	s.db.QueryRow(`SELECT video_width, video_height, video_duration_ms FROM posts WHERE id = $1`, postID).
		Scan(&m.Width, &m.Height, &m.DurationMs)
	return m
}

// apAttachmentList (AGORA-445) decodes an object's "attachment". JSON-LD lets it
// be a single object as well as an array: Mastodon always sends an array, but
// Loops' own documentation shows a single object for a video, and a plain
// []apAttachment fails to decode that, which would drop the whole post.
type apAttachmentList []apAttachment

func (l *apAttachmentList) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*l = nil
		return nil
	}
	if trimmed[0] == '{' {
		var one apAttachment
		if err := json.Unmarshal(trimmed, &one); err != nil {
			return err
		}
		*l = apAttachmentList{one}
		return nil
	}
	var many []apAttachment
	if err := json.Unmarshal(trimmed, &many); err != nil {
		return err
	}
	*l = many
	return nil
}
