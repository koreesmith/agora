package federation

import (
	"crypto/rand"
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
