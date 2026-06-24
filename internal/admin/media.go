package admin

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agora-social/agora/internal/auth"
)

type orphanedFile struct {
	Path    string    `json:"path"`
	URL     string    `json:"url"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// ScanOrphans walks the upload directory and returns files not referenced by any
// database row. Safe to call repeatedly — makes no changes to disk.
func (s *Service) ScanOrphans(w http.ResponseWriter, r *http.Request) {
	referenced, err := s.collectReferencedURLs()
	if err != nil {
		writeError(w, 500, "db error")
		return
	}

	var orphans []orphanedFile
	var totalBytes int64

	filepath.Walk(s.media.UploadDir(), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(s.media.UploadDir(), path)
		url := "/uploads/" + filepath.ToSlash(rel)
		if !referenced[url] {
			orphans = append(orphans, orphanedFile{
				Path:    rel,
				URL:     url,
				Size:    info.Size(),
				ModTime: info.ModTime(),
			})
			totalBytes += info.Size()
		}
		return nil
	})

	if orphans == nil {
		orphans = []orphanedFile{}
	}
	writeJSON(w, 200, map[string]any{
		"orphans":     orphans,
		"count":       len(orphans),
		"total_bytes": totalBytes,
	})
}

// DeleteOrphans removes every upload file not referenced in the database and
// writes an audit log entry.
func (s *Service) DeleteOrphans(w http.ResponseWriter, r *http.Request) {
	actorID := auth.UserIDFromCtx(r.Context())

	referenced, err := s.collectReferencedURLs()
	if err != nil {
		writeError(w, 500, "db error")
		return
	}

	var deleted []string
	var failed []string
	var totalBytes int64

	filepath.Walk(s.media.UploadDir(), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(s.media.UploadDir(), path)
		url := "/uploads/" + filepath.ToSlash(rel)
		if !referenced[url] {
			totalBytes += info.Size()
			if removeErr := os.Remove(path); removeErr != nil {
				failed = append(failed, rel)
			} else {
				deleted = append(deleted, rel)
			}
		}
		return nil
	})

	s.db.Exec(
		`INSERT INTO audit_log (actor_id, action, target_type, details) VALUES ($1, 'delete_orphaned_media', 'media', $2)`,
		actorID, fmt.Sprintf("deleted %d files (%d bytes)", len(deleted), totalBytes),
	)

	if deleted == nil {
		deleted = []string{}
	}
	if failed == nil {
		failed = []string{}
	}
	writeJSON(w, 200, map[string]any{
		"deleted":     deleted,
		"count":       len(deleted),
		"total_bytes": totalBytes,
		"failed":      failed,
	})
}

// collectReferencedURLs queries every table that stores upload paths and returns
// the set of /uploads/... URLs currently in use.
func (s *Service) collectReferencedURLs() (map[string]bool, error) {
	referenced := make(map[string]bool)

	queries := []string{
		`SELECT avatar_url          FROM users             WHERE avatar_url != ''`,
		`SELECT cover_url           FROM users             WHERE cover_url != ''`,
		`SELECT image_url           FROM posts             WHERE image_url != ''`,
		`SELECT video_url           FROM posts             WHERE video_url != ''`,
		`SELECT video_thumb_url     FROM posts             WHERE video_thumb_url != ''`,
		`SELECT url                 FROM post_photos`,
		`SELECT url                 FROM album_photos`,
		`SELECT cover_url           FROM albums            WHERE cover_url != ''`,
		`SELECT avatar_url          FROM pages             WHERE avatar_url != ''`,
		`SELECT cover_url           FROM pages             WHERE cover_url != ''`,
		`SELECT avatar_url          FROM community_groups  WHERE avatar_url != ''`,
		`SELECT cover_url           FROM community_groups  WHERE cover_url != ''`,
		`SELECT image_url           FROM messages          WHERE image_url != ''`,
		`SELECT value               FROM instance_settings WHERE key = 'logo_url' AND value != ''`,
	}

	for _, q := range queries {
		rows, err := s.db.Query(q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var url string
			rows.Scan(&url)
			if strings.HasPrefix(url, "/uploads/") {
				referenced[url] = true
			}
		}
		rows.Close()
	}

	return referenced, nil
}
