package federation

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/mediacache"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-380: an Update whose object is the sending actor itself is a profile
// change, and is the only kind that may refresh that actor's cached profile.
func TestIsSelfActorUpdate(t *testing.T) {
	const actor = "https://mastodon.example/users/alice"
	for name, tc := range map[string]struct {
		object string
		want   bool
	}{
		"own Person":            {`{"id":"` + actor + `","type":"Person"}`, true},
		"own Service (bot)":     {`{"id":"` + actor + `","type":"Service"}`, true},
		"someone else's Person": {`{"id":"https://mastodon.example/users/bob","type":"Person"}`, false},
		"a Note":                {`{"id":"` + actor + `/statuses/1","type":"Note","attributedTo":"` + actor + `"}`, false},
		"bare id string":        {`"` + actor + `"`, false},
	} {
		if got := isSelfActorUpdate(json.RawMessage(tc.object), actor); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

// A refreshed remote profile ends up pointing at a local copy of its avatar,
// keeps that copy across a refresh that reports the same avatar, and moves to
// a new copy when the avatar actually changes.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestUpsertCachesRemoteAvatar(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	images := map[string][]byte{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := images[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	image := func(name string) string {
		b := make([]byte, 1024)
		rand.Read(b)
		copy(b, "\x89PNG\r\n\x1a\n")
		images["/"+name] = b
		return srv.URL + "/" + name
	}

	// fedHTTPClient refuses loopback addresses by design, so the test server
	// is reached through its own client instead.
	s := &Service{db: db, mcache: mediacache.New(db, t.TempDir(), srv.Client())}

	n := time.Now().UnixNano()
	actorURL := fmt.Sprintf("https://mastodon.example/users/agora380_%d", n)
	first, second := image(fmt.Sprintf("a_%d.png", n)), image(fmt.Sprintf("b_%d.png", n))
	t.Cleanup(func() {
		db.Exec(`DELETE FROM users WHERE ap_actor_url = $1`, actorURL)
		db.Exec(`DELETE FROM remote_media_cache WHERE source_url IN ($1, $2)`, first, second)
	})

	profile := &remoteActorProfile{
		Inbox:             actorURL + "/inbox",
		PreferredUsername: fmt.Sprintf("agora380_%d", n),
		IconURL:           first,
	}
	avatar := func() string {
		var v string
		db.QueryRow(`SELECT avatar_url FROM users WHERE ap_actor_url = $1`, actorURL).Scan(&v)
		return v
	}
	// cacheActorImages runs in the background after each upsert.
	waitForCopy := func(t *testing.T) string {
		t.Helper()
		for i := 0; i < 100; i++ {
			if v := avatar(); mediacache.IsCached(v) {
				return v
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("avatar never moved to a local copy, still %q", avatar())
		return ""
	}

	if _, err := s.upsertRemoteAPUser(actorURL, profile); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	firstCopy := waitForCopy(t)

	// Same avatar reported again: the local copy stays put, it doesn't flip
	// back to the remote URL in between.
	if _, err := s.upsertRemoteAPUser(actorURL, profile); err != nil {
		t.Fatalf("upsert (same avatar): %v", err)
	}
	if got := avatar(); got != firstCopy {
		t.Errorf("refresh with the same avatar changed it to %q, want the copy %q", got, firstCopy)
	}

	// The account changed its avatar: it moves to a copy of the new one.
	profile.IconURL = second
	if _, err := s.upsertRemoteAPUser(actorURL, profile); err != nil {
		t.Fatalf("upsert (new avatar): %v", err)
	}
	if got := waitForCopy(t); got == firstCopy || !strings.HasPrefix(got, mediacache.URLPrefix) {
		t.Errorf("avatar after change = %q, want a new local copy", got)
	}
}

// AGORA-382: the AGORA-331 startup repair turned every relative avatar on a
// remote stub into https://{remote_instance}/{path}, including a cached copy
// (/uploads/remote/...), which lives here and not on the remote server. Every
// restart broke every cached avatar. Migrate must leave a cached copy alone,
// put back the ones it already broke, and still repair a genuinely relative
// legacy avatar the way AGORA-331 meant to.
func TestMigrateLeavesCachedAvatarsAlone(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	n := time.Now().UnixNano()
	hash := func(prefix string) string {
		return fmt.Sprintf("%s%062x", prefix, n)
	}
	stub := func(name, avatar string, apActor bool) string {
		t.Helper()
		actor := ""
		if apActor {
			actor = fmt.Sprintf("https://masto.example/users/%s_%d", name, n)
		}
		var id string
		// remote_user_id set on every stub, as upsertRemoteAPUser does for an
		// ActivityPub stub too: that's what let AGORA-331 reach them.
		if err := db.QueryRow(`
			INSERT INTO users (username, email, password_hash, is_remote, remote_user_id, remote_instance, ap_actor_url, avatar_url, remote_synced_at)
			VALUES ($1, $1, '', true, $2, 'masto.example', $3, $4, NOW()) RETURNING id
		`, fmt.Sprintf("%s_%d@masto.example", name, n), name, actor, avatar).Scan(&id); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })
		return id
	}
	read := func(id string) (avatar string, synced bool) {
		var s sql.NullTime
		db.QueryRow(`SELECT avatar_url, remote_synced_at FROM users WHERE id = $1`, id).Scan(&avatar, &s)
		return avatar, s.Valid
	}

	healthyCopy := "/uploads/remote/aa/" + hash("aa") + ".png"
	keptCopy := "/uploads/remote/bb/" + hash("bb") + ".jpg"
	prunedCopy := "/uploads/remote/cc/" + hash("cc") + ".jpg"
	db.Exec(`INSERT INTO remote_media_cache (source_url, local_url, kind, content_type, bytes) VALUES ($1, $2, 'avatar', 'image/jpeg', 1)`,
		fmt.Sprintf("https://masto.example/system/kept_%d.jpg", n), keptCopy)
	t.Cleanup(func() { db.Exec(`DELETE FROM remote_media_cache WHERE local_url = $1`, keptCopy) })

	healthy := stub("healthy", healthyCopy, true)
	brokenKept := stub("brokenkept", "https://masto.example"+keptCopy, true)
	brokenPruned := stub("brokenpruned", "https://masto.example"+prunedCopy, true)
	legacy := stub("legacy", "/uploads/avatars/legacy.jpg", false)

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if got, _ := read(healthy); got != healthyCopy {
		t.Errorf("cached avatar rewritten to %q, want it left at %q", got, healthyCopy)
	}
	if got, _ := read(brokenKept); got != keptCopy {
		t.Errorf("broken avatar with its copy still cached = %q, want %q", got, keptCopy)
	}
	if got, synced := read(brokenPruned); got != "" || synced {
		t.Errorf("broken avatar with no copy left = %q (synced %v), want cleared and queued for refresh", got, synced)
	}
	if got, _ := read(legacy); got != "https://masto.example/uploads/avatars/legacy.jpg" {
		t.Errorf("legacy relative avatar = %q, want it absolutized as before", got)
	}
}
