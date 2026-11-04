package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// AGORA-444: how much disk uploads take, with video apart from images, so an
// operator can see what Reels costs and decide on limits.

type storageCategory struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

type storageUser struct {
	Username string `json:"username"`
	Videos   int    `json:"videos"`
	Bytes    int64  `json:"bytes"`
}

type storageReport struct {
	Video      storageCategory   `json:"video"`
	Images     storageCategory   `json:"images"`
	Categories []storageCategory `json:"categories"`
	// The accounts using the most video storage, from the sizes recorded on
	// their posts that still exist.
	TopVideoUsers []storageUser `json:"top_video_users"`
	QuotaMB       int64         `json:"video_quota_mb"`
	MaxSeconds    int           `json:"video_max_seconds"`
	ComputedAt    time.Time     `json:"computed_at"`
}

// Walking the upload directory costs a stat per file, so a result is reused for
// a minute rather than recomputed on every look at the page.
var (
	storageMu     sync.Mutex
	storageCached *storageReport
)

const storageTTL = time.Minute

func (s *Service) computeStorage() *storageReport {
	root := s.media.UploadDir()
	entries, _ := os.ReadDir(root)

	rep := &storageReport{Categories: []storageCategory{}, TopVideoUsers: []storageUser{}, ComputedAt: time.Now()}
	for _, e := range entries {
		if !e.IsDir() || isRemoteMediaCacheDir(e.Name()) {
			continue
		}
		cat := storageCategory{Name: e.Name()}
		filepath.Walk(filepath.Join(root, e.Name()), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			cat.Bytes += info.Size()
			// A video is one .mp4; its thumbnail is a second file that belongs
			// to the same video and must not double the count.
			if e.Name() != "videos" || strings.HasSuffix(info.Name(), ".mp4") {
				cat.Count++
			}
			return nil
		})
		rep.Categories = append(rep.Categories, cat)
		if e.Name() == "videos" {
			rep.Video = cat
		} else {
			rep.Images.Count += cat.Count
			rep.Images.Bytes += cat.Bytes
		}
	}
	rep.Video.Name, rep.Images.Name = "video", "images"
	sort.Slice(rep.Categories, func(i, j int) bool { return rep.Categories[i].Bytes > rep.Categories[j].Bytes })

	rows, err := s.db.Query(`
		SELECT u.username, COUNT(*), COALESCE(SUM(p.video_bytes), 0)
		  FROM posts p JOIN users u ON u.id = p.author_id
		 WHERE p.deleted_at IS NULL AND p.video_bytes > 0
		 GROUP BY u.username ORDER BY SUM(p.video_bytes) DESC LIMIT 5`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var u storageUser
			if rows.Scan(&u.Username, &u.Videos, &u.Bytes) == nil {
				rep.TopVideoUsers = append(rep.TopVideoUsers, u)
			}
		}
	}

	rep.QuotaMB = settingInt(s.getSetting("video_quota_mb"), 0)
	rep.MaxSeconds = int(settingInt(s.getSetting("video_max_seconds"), 120))
	return rep
}

func (s *Service) getSetting(key string) string {
	var v string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = $1`, key).Scan(&v)
	return v
}

func settingInt(v string, def int64) int64 {
	var n int64
	for _, c := range strings.TrimSpace(v) {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int64(c-'0')
	}
	if strings.TrimSpace(v) == "" {
		return def
	}
	return n
}

// GetMediaStorage reports upload storage with video separate from images.
// ?refresh=1 skips the one-minute cache.
func (s *Service) GetMediaStorage(w http.ResponseWriter, r *http.Request) {
	storageMu.Lock()
	defer storageMu.Unlock()
	if storageCached == nil || r.URL.Query().Get("refresh") == "1" || time.Since(storageCached.ComputedAt) > storageTTL {
		storageCached = s.computeStorage()
	}
	writeJSON(w, 200, storageCached)
}
