package federation

import (
	"testing"
	"time"
)

// AGORA-331: a locally uploaded avatar is stored as a relative /uploads/...
// path (media.SaveUpload), and the legacy profile_update broadcast sent that
// straight from the database. The receiving instance stored it verbatim and
// then requested it from its own domain, so a federated friend's avatar broke
// the first time they edited their profile.
//
// AGORA-327 removed the broadcast, but the receiver is the side that can
// actually repair it, because it knows which domain the path belongs to. That
// guard is what this test covers. AGORA-330 removed the handler that a second
// test here drove, along with the rest of the legacy transport; the helper it
// was really about is still exercised below, and syncStaleRemoteUsers is now
// the path that repairs an already-corrupted stub. An instance on an older build will
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
