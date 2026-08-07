package federation

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-331: a locally uploaded avatar is stored as a relative /uploads/...
// path (media.SaveUpload), and the legacy profile_update broadcast sent that
// straight from the database. The receiving instance stored it verbatim and
// then requested it from its own domain, so a federated friend's avatar broke
// the first time they edited their profile.
//
// AGORA-327 removed the broadcast, but the receiver is the side that can
// actually repair it, because it knows which domain the path belongs to. That
// guard is what these tests cover: an instance still on an older build will
// keep sending relative paths for as long as it takes them to upgrade.
//
// This is the second time this defect class has shipped (AGORA-312 fixed it on
// GetUser), which is why the guard is tested rather than assumed.

func TestRemoteAbsoluteURL(t *testing.T) {
	const instance = "peer.example"

	cases := []struct {
		name, in, want string
	}{
		{"relative path is resolved against the sending instance",
			"/uploads/avatar/x.jpg", "https://peer.example/uploads/avatar/x.jpg"},
		{"relative path without a leading slash still resolves",
			"uploads/avatar/x.jpg", "https://peer.example/uploads/avatar/x.jpg"},
		{"an already absolute https URL is left alone",
			"https://cdn.example/x.jpg", "https://cdn.example/x.jpg"},
		{"an already absolute http URL is left alone",
			"http://cdn.example/x.jpg", "http://cdn.example/x.jpg"},
		// An account with no avatar must stay that way rather than acquiring a
		// broken one pointing at the sending instance's bare domain.
		{"empty stays empty", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := remoteAbsoluteURL(c.in, instance); got != c.want {
				t.Errorf("remoteAbsoluteURL(%q, %q) = %q, want %q", c.in, instance, got, c.want)
			}
		})
	}

	t.Run("a syntactically invalid instance host is not built into a URL", func(t *testing.T) {
		got := remoteAbsoluteURL("/uploads/avatar/x.jpg", "bad host/with spaces")
		if got != "/uploads/avatar/x.jpg" {
			t.Errorf("got %q, want the input unchanged rather than a malformed URL", got)
		}
	})
}

// TestInboundProfileUpdateAbsolutizesAvatar drives the real handler, since the
// bug was not in the helper (which did not exist) but in the handler storing
// what it was given.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestInboundProfileUpdateAbsolutizesAvatar(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}
	instance := fmt.Sprintf("agora331-%d.example", time.Now().UnixNano())

	id := s.getOrCreateRemoteUser("alice", instance)
	if id == "" {
		t.Fatal("could not create the remote stub")
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, id) })

	obj, _ := json.Marshal(map[string]string{
		"handle":       "alice",
		"display_name": "Alice",
		"avatar_url":   "/uploads/avatar/new.jpg",
		"bio":          "hi",
	})
	s.handleInboundProfileUpdate(Activity{Type: "profile_update", InstanceID: instance, Object: obj})

	var avatar string
	db.QueryRow(`SELECT avatar_url FROM users WHERE id = $1`, id).Scan(&avatar)

	want := "https://" + instance + "/uploads/avatar/new.jpg"
	if avatar != want {
		t.Errorf("avatar_url = %q, want %q.\nA relative path stored here is requested from THIS instance's domain and 404s.", avatar, want)
	}
}

// TestPublishedAtClampIsOneSided covers AGORA-332. The asymmetry is the whole
// point: clamping the past would corrupt backfilled archives, which is a real
// use case, to defend against old posts sorting downward, which is not an
// attack. A symmetric range check would pass a naive test and break backfill.
func TestPublishedAtClampIsOneSided(t *testing.T) {
	now := time.Now()

	t.Run("a far-future timestamp is clamped to now", func(t *testing.T) {
		got := clampPublished(now.Add(365 * 24 * time.Hour))
		if got.After(now.Add(maxPublishedSkew)) {
			t.Errorf("got %v, which still sorts above every genuine post", got)
		}
	})

	t.Run("a timestamp inside the skew tolerance is untouched", func(t *testing.T) {
		in := now.Add(maxPublishedSkew / 2)
		if got := clampPublished(in); !got.Equal(in) {
			t.Errorf("got %v, want %v: a peer with a slightly fast clock is not an attacker", got, in)
		}
	})

	t.Run("an old timestamp is preserved exactly", func(t *testing.T) {
		in := now.Add(-5 * 365 * 24 * time.Hour)
		if got := clampPublished(in); !got.Equal(in) {
			t.Errorf("got %v, want %v: clamping the past corrupts backfilled archives", got, in)
		}
	})

	t.Run("parseAPTime applies the clamp", func(t *testing.T) {
		future := now.Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)
		if got := parseAPTime(future); got.After(now.Add(maxPublishedSkew)) {
			t.Errorf("parseAPTime(%q) = %v, unclamped", future, got)
		}
	})

	t.Run("parseAPTime still falls back to now on a malformed value", func(t *testing.T) {
		got := parseAPTime("not a timestamp")
		if got.Before(now.Add(-time.Minute)) || got.After(now.Add(time.Minute)) {
			t.Errorf("got %v, want approximately now", got)
		}
	})
}
