package mediacache

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func testDB(t *testing.T) *store.DB {
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

// fakePNG is a PNG signature followed by random bytes: enough for content
// sniffing, and distinct each call so content addressing doesn't merge them.
func fakePNG(size int) []byte {
	b := make([]byte, size)
	rand.Read(b)
	copy(b, "\x89PNG\r\n\x1a\n")
	return b
}

// server serves whatever body is registered for a path, over TLS since the
// cache only accepts https sources.
type server struct {
	*httptest.Server
	bodies map[string][]byte
}

func newServer(t *testing.T) *server {
	s := &server{bodies: map[string][]byte{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := s.bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) serve(body []byte) string {
	path := fmt.Sprintf("/media/%d", time.Now().UnixNano())
	s.bodies[path] = body
	return s.URL + path
}

func newCache(t *testing.T, db *store.DB, srv *server, settings Settings) *Cache {
	c := New(db, t.TempDir(), srv.Client())
	c.fixed = &settings
	return c
}

var defaults = Settings{Enabled: true, MaxMB: 100, MaxFileMB: 5}

// cleanupSources deletes the cache rows a test created, so the table doesn't
// accumulate across runs.
func cleanupSources(t *testing.T, db *store.DB, sources ...string) {
	t.Cleanup(func() {
		for _, s := range sources {
			db.Exec(`DELETE FROM remote_media_cache WHERE source_url = $1`, s)
		}
	})
}

func remoteUser(t *testing.T, db *store.DB, avatar string) string {
	t.Helper()
	name := fmt.Sprintf("mc_%d", time.Now().UnixNano())
	var id string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, avatar_url)
		VALUES ($1, $2, '', true, $3) RETURNING id
	`, name, name+"@example.com", avatar).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return id
}

func avatarOf(t *testing.T, db *store.DB, id string) string {
	t.Helper()
	var v string
	db.QueryRow(`SELECT avatar_url FROM users WHERE id = $1`, id).Scan(&v)
	return v
}

func TestStore(t *testing.T) {
	db := testDB(t)
	srv := newServer(t)
	c := newCache(t, db, srv, defaults)

	t.Run("downloads an image and serves it locally", func(t *testing.T) {
		body := fakePNG(2048)
		src := srv.serve(body)
		cleanupSources(t, db, src)

		local, err := c.Store(src, KindAvatar)
		if err != nil {
			t.Fatalf("Store: %v", err)
		}
		if !IsCached(local) || !strings.HasSuffix(local, ".png") {
			t.Fatalf("local url = %q, want a %s...png url", local, URLPrefix)
		}
		got, err := os.ReadFile(c.pathFor(local))
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("cached file missing or different: %v", err)
		}

		// A second Store of the same source is served from the cache.
		delete(srv.bodies, strings.TrimPrefix(src, srv.URL))
		again, err := c.Store(src, KindAvatar)
		if err != nil || again != local {
			t.Fatalf("second Store = %q, %v; want %q from the cache", again, err, local)
		}
	})

	t.Run("identical bytes from two sources share one file", func(t *testing.T) {
		body := fakePNG(1024)
		a, b := srv.serve(body), srv.serve(body)
		cleanupSources(t, db, a, b)
		la, errA := c.Store(a, KindAvatar)
		lb, errB := c.Store(b, KindAvatar)
		if errA != nil || errB != nil || la != lb {
			t.Fatalf("got %q (%v) and %q (%v), want one shared copy", la, errA, lb, errB)
		}
	})

	t.Run("refuses what isn't a safe image", func(t *testing.T) {
		for name, body := range map[string][]byte{
			"html": []byte("<html><script>alert(1)</script></html>"),
			"svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		} {
			src := srv.serve(body)
			cleanupSources(t, db, src)
			if _, err := c.Store(src, KindAvatar); err != ErrNotImage {
				t.Errorf("%s: err = %v, want ErrNotImage", name, err)
			}
		}
	})

	t.Run("refuses a file over the per-file limit", func(t *testing.T) {
		small := newCache(t, db, srv, Settings{Enabled: true, MaxMB: 100, MaxFileMB: 1})
		src := srv.serve(fakePNG(1<<20 + 1))
		cleanupSources(t, db, src)
		if _, err := small.Store(src, KindAvatar); err != ErrTooLarge {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})

	t.Run("does nothing while disabled", func(t *testing.T) {
		off := newCache(t, db, srv, Settings{Enabled: false, MaxMB: 100, MaxFileMB: 5})
		src := srv.serve(fakePNG(512))
		cleanupSources(t, db, src)
		if _, err := off.Store(src, KindAvatar); err != ErrDisabled {
			t.Fatalf("err = %v, want ErrDisabled", err)
		}
	})

	t.Run("only accepts https sources", func(t *testing.T) {
		if _, err := c.Store("http://example.com/a.png", KindAvatar); err == nil {
			t.Fatal("accepted an http source")
		}
	})
}

func TestPrune(t *testing.T) {
	db := testDB(t)
	srv := newServer(t)

	t.Run("removes copies no profile uses", func(t *testing.T) {
		c := newCache(t, db, srv, defaults)
		used, unused := srv.serve(fakePNG(1024)), srv.serve(fakePNG(1024))
		cleanupSources(t, db, used, unused)
		lUsed, _ := c.Store(used, KindAvatar)
		lUnused, _ := c.Store(unused, KindAvatar)
		user := remoteUser(t, db, lUsed)

		if _, err := c.Prune(); err != nil {
			t.Fatalf("Prune: %v", err)
		}
		if _, err := os.Stat(c.pathFor(lUnused)); !os.IsNotExist(err) {
			t.Error("unused copy still on disk")
		}
		if _, err := os.Stat(c.pathFor(lUsed)); err != nil {
			t.Error("copy in use was removed")
		}
		if avatarOf(t, db, user) != lUsed {
			t.Error("profile using a kept copy was changed")
		}
	})

	t.Run("evicts the oldest over the limit and falls back to the source", func(t *testing.T) {
		// 1MB limit, so prune works down to 90% of it: three 400KB copies in
		// use are over, and evicting the oldest brings it back under.
		c := newCache(t, db, srv, Settings{Enabled: true, MaxMB: 1, MaxFileMB: 1})
		var sources, locals, users []string
		for i := 0; i < 3; i++ {
			src := srv.serve(fakePNG(400 << 10))
			local, err := c.Store(src, KindAvatar)
			if err != nil {
				t.Fatalf("Store %d: %v", i, err)
			}
			sources = append(sources, src)
			locals = append(locals, local)
			users = append(users, remoteUser(t, db, local))
			time.Sleep(10 * time.Millisecond) // distinct fetched_at order
		}
		cleanupSources(t, db, sources...)

		if _, err := c.Prune(); err != nil {
			t.Fatalf("Prune: %v", err)
		}
		if c.totalBytes() > (1<<20)*9/10 {
			t.Errorf("cache is %d bytes after prune, want at most 90%% of the limit", c.totalBytes())
		}
		if got := avatarOf(t, db, users[0]); got != sources[0] {
			t.Errorf("oldest copy's profile avatar = %q, want it back on the source %q", got, sources[0])
		}
		if _, err := os.Stat(c.pathFor(locals[0])); !os.IsNotExist(err) {
			t.Error("oldest copy still on disk")
		}
		if got := avatarOf(t, db, users[2]); got != locals[2] {
			t.Errorf("newest copy was evicted: avatar = %q", got)
		}
	})
}

func TestClear(t *testing.T) {
	db := testDB(t)
	srv := newServer(t)
	c := newCache(t, db, srv, defaults)

	src := srv.serve(fakePNG(1024))
	cleanupSources(t, db, src)
	local, err := c.Store(src, KindAvatar)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	user := remoteUser(t, db, local)

	if _, err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got := avatarOf(t, db, user); got != src {
		t.Errorf("avatar after clear = %q, want the source %q", got, src)
	}
	if _, err := os.Stat(c.pathFor(local)); !os.IsNotExist(err) {
		t.Error("file still on disk after clear")
	}
}

func TestSaveSettingsValidates(t *testing.T) {
	db := testDB(t)
	c := New(db, t.TempDir(), nil)
	for name, s := range map[string]Settings{
		"zero total":          {Enabled: true, MaxMB: 0, MaxFileMB: 1},
		"zero per-file":       {Enabled: true, MaxMB: 10, MaxFileMB: 0},
		"per-file over total": {Enabled: true, MaxMB: 10, MaxFileMB: 20},
	} {
		if err := c.SaveSettings(s); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
}

func TestPathForStaysInsideCache(t *testing.T) {
	c := New(nil, "/data/uploads", nil)
	for _, u := range []string{"/uploads/avatars/x.png", URLPrefix + "../../etc/passwd"} {
		if p := c.pathFor(u); !strings.HasSuffix(p, "/remote/invalid") {
			t.Errorf("pathFor(%q) = %q, want the invalid placeholder", u, p)
		}
	}
}
