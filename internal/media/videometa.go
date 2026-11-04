package media

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// VideoMeta is a video's shape (AGORA-437): enough for a player to reserve the
// right space before the file loads, and for feeds to show a length. Zero
// means unknown.
type VideoMeta struct {
	Width      int
	Height     int
	DurationMs int
}

// ProbeVideo reads a video file's display size and duration with ffprobe.
// Dimensions are the first video stream's, with a 90° or 270° rotation
// applied (a phone video stored sideways and flagged to rotate). ffmpeg's own
// transcode already bakes that rotation in, so for files Agora produced this
// is a no-op, but it keeps the answer right for anything else.
func ProbeVideo(path string) (VideoMeta, error) {
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height:stream_side_data=rotation:stream_tags=rotate:format=duration",
		"-of", "json",
		path,
	).Output()
	if err != nil {
		return VideoMeta{}, err
	}
	return parseProbe(out)
}

func parseProbe(out []byte) (VideoMeta, error) {
	var p struct {
		Streams []struct {
			Width    int               `json:"width"`
			Height   int               `json:"height"`
			Tags     map[string]string `json:"tags"`
			SideData []struct {
				Rotation json.Number `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return VideoMeta{}, err
	}
	if len(p.Streams) == 0 || p.Streams[0].Width <= 0 || p.Streams[0].Height <= 0 {
		return VideoMeta{}, fmt.Errorf("no video stream")
	}
	st := p.Streams[0]
	m := VideoMeta{Width: st.Width, Height: st.Height}

	rot := 0.0
	if r := st.Tags["rotate"]; r != "" {
		rot, _ = strconv.ParseFloat(r, 64)
	}
	for _, sd := range st.SideData {
		if v, err := sd.Rotation.Float64(); err == nil && v != 0 {
			rot = v
		}
	}
	if q := int(math.Abs(math.Round(rot))) % 180; q == 90 {
		m.Width, m.Height = m.Height, m.Width
	}

	if d, err := strconv.ParseFloat(strings.TrimSpace(p.Format.Duration), 64); err == nil && d > 0 {
		m.DurationMs = int(math.Round(d * 1000))
	}
	return m, nil
}

// BackfillVideoMeta fills in width, height and duration for Agora-hosted
// videos posted before they were recorded (AGORA-437). Safe to run on every
// start: it only touches posts that still have none, a video that can't be
// read is skipped and retried next time, and it works in small batches so it
// never holds the database for long.
func (s *Service) BackfillVideoMeta() {
	const batch = 200
	done, failed := 0, 0
	lastID := ""
	for {
		rows, err := s.db.Query(
			`SELECT id, video_url FROM posts
			  WHERE video_width = 0 AND video_url LIKE '/uploads/videos/%' AND id::text > $1
			  ORDER BY id::text LIMIT $2`, lastID, batch)
		if err != nil {
			log.Printf("media: video meta backfill: %v", err)
			return
		}
		type item struct{ id, url string }
		var items []item
		for rows.Next() {
			var it item
			if rows.Scan(&it.id, &it.url) == nil {
				items = append(items, it)
			}
		}
		rows.Close()
		if len(items) == 0 {
			break
		}
		for _, it := range items {
			lastID = it.id
			meta, err := ProbeVideo(filepath.Join(s.uploadDir, "videos", filepath.Base(it.url)))
			if err != nil {
				failed++
				continue
			}
			s.db.Exec(`UPDATE posts SET video_width=$2, video_height=$3, video_duration_ms=$4 WHERE id=$1`,
				it.id, meta.Width, meta.Height, meta.DurationMs)
			done++
		}
	}
	if done > 0 || failed > 0 {
		log.Printf("media: video meta backfill: %d filled in, %d could not be read", done, failed)
	}
	s.BackfillVideoBytes()
}

// BackfillVideoBytes records how much disk each Agora-hosted video takes, for
// videos posted before that was tracked (AGORA-444), so per-account storage
// limits count them. Same shape as BackfillVideoMeta: safe to repeat, small
// batches, files it can't find are skipped.
func (s *Service) BackfillVideoBytes() {
	const batch = 200
	filled := 0
	lastID := ""
	for {
		rows, err := s.db.Query(
			`SELECT id, video_url, video_thumb_url FROM posts
			  WHERE video_bytes = 0 AND video_url LIKE '/uploads/videos/%' AND id::text > $1
			  ORDER BY id::text LIMIT $2`, lastID, batch)
		if err != nil {
			log.Printf("media: video size backfill: %v", err)
			return
		}
		type item struct{ id, video, thumb string }
		var items []item
		for rows.Next() {
			var it item
			if rows.Scan(&it.id, &it.video, &it.thumb) == nil {
				items = append(items, it)
			}
		}
		rows.Close()
		if len(items) == 0 {
			break
		}
		for _, it := range items {
			lastID = it.id
			n := fileSize(s.localVideoPath(it.video))
			if t := s.localVideoPath(it.thumb); t != "" {
				n += fileSize(t)
			}
			if n > 0 {
				s.db.Exec(`UPDATE posts SET video_bytes = $2 WHERE id = $1`, it.id, n)
				filled++
			}
		}
	}
	if filled > 0 {
		log.Printf("media: video size backfill: %d videos measured", filled)
	}
}
