package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// AGORA-444: what an instance lets people upload. Both limits are instance
// settings an admin can change without a restart, read when an upload arrives.

const minVideoSeconds = 5

// maxVideoSeconds is the longest video the instance accepts: the admin's
// video_max_seconds, never above the built-in 2 minutes (what the transcode
// pipeline and storage were sized for) and never below a few seconds.
func (s *Service) maxVideoSeconds() float64 {
	n, err := strconv.Atoi(strings.TrimSpace(s.setting("video_max_seconds")))
	if err != nil || n < minVideoSeconds || n > maxVideoDurationSec {
		return maxVideoDurationSec
	}
	return float64(n)
}

// videoQuotaBytes is how much video storage one account may use, from the
// admin's video_quota_mb, or 0 for no limit.
func (s *Service) videoQuotaBytes() int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s.setting("video_quota_mb")), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n << 20
}

func (s *Service) setting(key string) string {
	var v string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = $1`, key).Scan(&v)
	return v
}

// videoQuotaError returns the message to show when an account is already at its
// video storage limit, or nil when it may upload. Storage is counted from the
// account's own posts that still exist, so deleting a video makes room again.
// The check is made before an upload is read, so a refused upload costs nothing;
// the video that takes an account over the line is allowed, since its size is
// not known until it has been transcoded.
func (s *Service) videoQuotaError(userID string) error {
	quota := s.videoQuotaBytes()
	if quota == 0 {
		return nil
	}
	var used int64
	s.db.QueryRow(`SELECT COALESCE(SUM(video_bytes), 0) FROM posts WHERE author_id = $1 AND deleted_at IS NULL`, userID).Scan(&used)
	if used < quota {
		return nil
	}
	return fmt.Errorf("You've used %s of the %s of video storage this instance gives each account. Delete one of your videos to make room for a new one", byteText(used), byteText(quota))
}

func byteText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/float64(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	}
}

// localVideoPath maps a post's video or thumbnail URL to its file under the
// upload directory, or "" for anything that isn't one of this instance's own
// uploaded videos (a remote video, a Bluesky playlist, a path trying to leave
// the directory).
func (s *Service) localVideoPath(url string) string {
	const prefix = "/uploads/videos/"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}
	name := filepath.Base(url)
	if name == "." || name == ".." || name != strings.TrimPrefix(url, prefix) {
		return ""
	}
	return filepath.Join(s.uploadDir, "videos", name)
}

// RemoveVideoFiles deletes a post's uploaded video and thumbnail from disk. It
// is for a moderator's takedown (AGORA-444), where the content has to be gone
// and not merely hidden. A URL that isn't one of this instance's own uploads is
// left alone. It reports how many files were removed.
func (s *Service) RemoveVideoFiles(urls ...string) int {
	removed := 0
	for _, u := range urls {
		path := s.localVideoPath(u)
		if path == "" {
			continue
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
	}
	return removed
}

// fileSize is a file's size in bytes, or 0 if it can't be read.
func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}
