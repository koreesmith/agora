// Package mediacache keeps local copies of remote media (AGORA-380).
//
// Remote avatars and covers used to be hot-linked straight from the server
// they came from, which breaks in ways Agora can't see: Mastodon serves a new
// URL each time an avatar changes and deletes the old file, Threads avatars
// come off Instagram's CDN through signed links that expire, and some CDNs
// refuse to be embedded on another site at all. A local copy sidesteps all of
// it. It's the same approach Mastodon itself takes.
//
// The cache is bounded. Each file has a size cap, the whole cache has a total
// cap, and Prune removes copies nothing points at any more and then evicts the
// oldest until the total fits. An evicted copy's users fall back to the
// original remote URL rather than to nothing, and the next profile refresh
// caches it again.
package mediacache

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// Kinds of media the cache holds. Each kind lists the users columns that may
// point at a copy of it, which is what decides whether a copy is still in use.
const (
	KindAvatar = "avatar"
	KindCover  = "cover"
)

// subdir is where copies live under the upload directory, served at
// /uploads/remote/... by the existing uploads file server.
const subdir = "remote"

// URLPrefix is the local URL prefix every cached copy has.
const URLPrefix = "/uploads/" + subdir + "/"

var (
	ErrDisabled = errors.New("remote media cache is disabled")
	ErrTooLarge = errors.New("remote media exceeds the per-file size limit")
	ErrNotImage = errors.New("remote media is not a supported image type")
)

// Default limits, used when a setting is missing or not a positive number.
const (
	defaultMaxMB     = 1024
	defaultMaxFileMB = 5
)

// Settings are the admin-controlled limits, stored in instance_settings.
type Settings struct {
	Enabled   bool  `json:"enabled"`
	MaxMB     int64 `json:"max_mb"`
	MaxFileMB int64 `json:"max_file_mb"`
}

// Stats describe what the cache currently holds.
type Stats struct {
	Files  int64 `json:"files"`
	Bytes  int64 `json:"bytes"`
	Orphan int64 `json:"orphaned_files"`
}

// PruneResult reports what a Prune or Clear removed.
type PruneResult struct {
	Orphans    int   `json:"orphans_removed"`
	Evicted    int   `json:"evicted"`
	BytesFreed int64 `json:"bytes_freed"`
	BytesAfter int64 `json:"bytes_after"`
}

type Cache struct {
	db     *store.DB
	dir    string
	client *http.Client

	// fixed, when set, replaces the stored settings. Only tests set it, so
	// they can use tiny limits without touching the shared settings rows.
	fixed *Settings
}

// pruneMu keeps a scheduled prune, an admin "Prune now" and an over-cap prune
// triggered by Store from running over each other. Package-level because
// federation and admin each hold their own Cache over the same directory.
var pruneMu sync.Mutex

// New returns a cache rooted at uploadDir. client fetches remote media and
// must be one that refuses private and loopback addresses (federation's
// fedHTTPClient); it may be nil for a caller that only prunes or reads stats.
func New(db *store.DB, uploadDir string, client *http.Client) *Cache {
	return &Cache{db: db, dir: uploadDir, client: client}
}

// IsCached reports whether u is a URL this cache produced.
func IsCached(u string) bool { return strings.HasPrefix(u, URLPrefix) }

func (c *Cache) setting(key string) string {
	var v string
	c.db.QueryRow(`SELECT value FROM instance_settings WHERE key = $1`, key).Scan(&v)
	return v
}

func positiveInt(s string, def int64) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// Settings reads the current limits, falling back to defaults.
func (c *Cache) Settings() Settings {
	if c.fixed != nil {
		return *c.fixed
	}
	return Settings{
		Enabled:   c.setting("remote_media_cache_enabled") != "false",
		MaxMB:     positiveInt(c.setting("remote_media_cache_max_mb"), defaultMaxMB),
		MaxFileMB: positiveInt(c.setting("remote_media_cache_max_file_mb"), defaultMaxFileMB),
	}
}

// SaveSettings validates and stores new limits.
func (c *Cache) SaveSettings(s Settings) error {
	if s.MaxMB <= 0 || s.MaxFileMB <= 0 {
		return fmt.Errorf("limits must be positive")
	}
	if s.MaxFileMB > s.MaxMB {
		return fmt.Errorf("the per-file limit can't exceed the total limit")
	}
	for k, v := range map[string]string{
		"remote_media_cache_enabled":     strconv.FormatBool(s.Enabled),
		"remote_media_cache_max_mb":      strconv.FormatInt(s.MaxMB, 10),
		"remote_media_cache_max_file_mb": strconv.FormatInt(s.MaxFileMB, 10),
	} {
		if _, err := c.db.Exec(`
			INSERT INTO instance_settings (key, value, updated_at) VALUES ($1, $2, NOW())
			ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
		`, k, v); err != nil {
			return err
		}
	}
	return nil
}

// allowedImage maps a sniffed content type to the extension a copy is saved
// under. SVG is deliberately absent: it can carry script, and a copy is served
// from this instance's own origin.
var allowedImage = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// sniff decides the content type from the bytes themselves rather than the
// remote's Content-Type header. AVIF isn't recognized by
// http.DetectContentType, so it's identified by its ftyp box.
func sniff(b []byte) (contentType, ext string, ok bool) {
	if len(b) >= 12 && string(b[4:12]) == "ftypavif" {
		return "image/avif", ".avif", true
	}
	ct := http.DetectContentType(b)
	ext, ok = allowedImage[ct]
	return ct, ext, ok
}

// Store returns a local URL for sourceURL, downloading it if there's no copy
// yet. A failure leaves nothing behind; the caller keeps using sourceURL.
func (c *Cache) Store(sourceURL, kind string) (string, error) {
	if !strings.HasPrefix(sourceURL, "https://") {
		return "", fmt.Errorf("source url must be https")
	}
	settings := c.Settings()
	if !settings.Enabled {
		return "", ErrDisabled
	}

	var localURL string
	if err := c.db.QueryRow(`SELECT local_url FROM remote_media_cache WHERE source_url = $1`, sourceURL).Scan(&localURL); err == nil {
		if _, statErr := os.Stat(c.pathFor(localURL)); statErr == nil {
			c.db.Exec(`UPDATE remote_media_cache SET fetched_at = NOW() WHERE source_url = $1`, sourceURL)
			return localURL, nil
		}
		// The row outlived its file (removed by hand, or a restore without
		// the uploads volume). Fetch it again rather than serve a 404.
	}

	if c.client == nil {
		return "", fmt.Errorf("cache has no http client")
	}
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch returned %d", resp.StatusCode)
	}

	maxBytes := settings.MaxFileMB << 20
	if resp.ContentLength > maxBytes {
		return "", ErrTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > maxBytes {
		return "", ErrTooLarge
	}
	contentType, ext, ok := sniff(body)
	if !ok {
		return "", ErrNotImage
	}

	sum := sha256.Sum256(body)
	name := hex.EncodeToString(sum[:])
	localURL = URLPrefix + name[:2] + "/" + name + ext
	path := c.pathFor(localURL)
	if _, err := os.Stat(path); err != nil {
		if err := writeAtomic(path, body); err != nil {
			return "", err
		}
	}

	if _, err := c.db.Exec(`
		INSERT INTO remote_media_cache (source_url, local_url, kind, content_type, bytes, fetched_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (source_url) DO UPDATE
		  SET local_url = $2, kind = $3, content_type = $4, bytes = $5, fetched_at = NOW()
	`, sourceURL, localURL, kind, contentType, len(body)); err != nil {
		return "", err
	}

	if c.totalBytes() > settings.MaxMB<<20 {
		go c.Prune()
	}
	return localURL, nil
}

func writeAtomic(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// pathFor maps a local URL back to its file. Only URLs this cache produced
// are accepted, so a crafted value can't reach outside the cache directory.
func (c *Cache) pathFor(localURL string) string {
	rel := strings.TrimPrefix(localURL, URLPrefix)
	if !IsCached(localURL) || strings.Contains(rel, "..") {
		return filepath.Join(c.dir, subdir, "invalid")
	}
	return filepath.Join(c.dir, subdir, filepath.FromSlash(rel))
}

// totalBytes counts each distinct file once, since several source URLs can
// share one content-addressed copy.
func (c *Cache) totalBytes() int64 {
	var n sql.NullInt64
	c.db.QueryRow(`SELECT SUM(bytes) FROM (SELECT DISTINCT ON (local_url) bytes FROM remote_media_cache) f`).Scan(&n)
	return n.Int64
}

// inUse is the SQL condition for a copy that some profile still points at.
const inUse = `EXISTS (SELECT 1 FROM users u WHERE u.avatar_url = c.local_url OR u.cover_url = c.local_url)`

// Stats reports the cache's current size.
func (c *Cache) Stats() Stats {
	var st Stats
	c.db.QueryRow(`SELECT COUNT(DISTINCT local_url) FROM remote_media_cache`).Scan(&st.Files)
	st.Bytes = c.totalBytes()
	c.db.QueryRow(`SELECT COUNT(DISTINCT c.local_url) FROM remote_media_cache c WHERE NOT ` + inUse).Scan(&st.Orphan)
	return st
}

// Prune removes copies no profile points at any more, then, while the cache
// is still over its total limit, evicts the least recently fetched copies.
// Eviction is stopped at 90% of the limit so the next few downloads don't
// immediately trigger another round.
func (c *Cache) Prune() (PruneResult, error) {
	pruneMu.Lock()
	defer pruneMu.Unlock()

	var res PruneResult

	rows, err := c.db.Query(`SELECT DISTINCT c.local_url FROM remote_media_cache c WHERE NOT ` + inUse)
	if err != nil {
		return res, err
	}
	var orphans []string
	for rows.Next() {
		var u string
		rows.Scan(&u)
		orphans = append(orphans, u)
	}
	rows.Close()
	for _, u := range orphans {
		freed, err := c.remove(u)
		if err != nil {
			log.Printf("mediacache: removing orphan %s: %v", u, err)
			continue
		}
		res.Orphans++
		res.BytesFreed += freed
	}

	target := (c.Settings().MaxMB << 20) * 9 / 10
	for c.totalBytes() > target {
		var oldest string
		if err := c.db.QueryRow(`
			SELECT local_url FROM remote_media_cache
			GROUP BY local_url ORDER BY MAX(fetched_at) ASC LIMIT 1
		`).Scan(&oldest); err != nil {
			break
		}
		freed, err := c.remove(oldest)
		if err != nil {
			log.Printf("mediacache: evicting %s: %v", oldest, err)
			break
		}
		res.Evicted++
		res.BytesFreed += freed
	}

	res.BytesAfter = c.totalBytes()
	return res, nil
}

// Clear removes every copy, pointing each profile back at its remote URL.
func (c *Cache) Clear() (PruneResult, error) {
	pruneMu.Lock()
	defer pruneMu.Unlock()

	var res PruneResult
	rows, err := c.db.Query(`SELECT DISTINCT local_url FROM remote_media_cache`)
	if err != nil {
		return res, err
	}
	var all []string
	for rows.Next() {
		var u string
		rows.Scan(&u)
		all = append(all, u)
	}
	rows.Close()
	for _, u := range all {
		freed, err := c.remove(u)
		if err != nil {
			log.Printf("mediacache: clearing %s: %v", u, err)
			continue
		}
		res.Evicted++
		res.BytesFreed += freed
	}
	res.BytesAfter = c.totalBytes()
	return res, nil
}

// remove drops one copy: any profile still pointing at it goes back to the
// remote URL it was made from, then the rows and the file are deleted.
func (c *Cache) remove(localURL string) (int64, error) {
	var sourceURL string
	var size int64
	if err := c.db.QueryRow(`
		SELECT source_url, bytes FROM remote_media_cache WHERE local_url = $1
		ORDER BY fetched_at DESC LIMIT 1
	`, localURL).Scan(&sourceURL, &size); err != nil {
		return 0, err
	}
	tx, err := c.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET avatar_url = $2 WHERE avatar_url = $1`, localURL, sourceURL); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE users SET cover_url = $2 WHERE cover_url = $1`, localURL, sourceURL); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM remote_media_cache WHERE local_url = $1`, localURL); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if err := os.Remove(c.pathFor(localURL)); err != nil && !os.IsNotExist(err) {
		return size, err
	}
	return size, nil
}

// PruneInterval is how often the scheduled prune runs.
const PruneInterval = time.Hour
