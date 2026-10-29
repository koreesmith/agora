package federation

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// AGORA-381: a private profile's actor document is redacted for everyone
// except a signed fetch by one of its accepted friends, which is the only way
// a friend's instance ever gets the avatar and cover.

type remotePeer struct {
	id, actorURL string
	key          *rsa.PrivateKey
}

// seedRemotePeer creates a remote Agora stub and, if cacheKey, the cached
// public key a signed fetch from it is verified against.
func seedRemotePeer(t *testing.T, db *store.DB, cacheKey bool) remotePeer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("peer_%d", time.Now().UnixNano())
	p := remotePeer{actorURL: "https://peer.example/federation/users/" + name, key: key}
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, ap_actor_url, ap_inbox_url, remote_instance)
		VALUES ($1, $2, '', true, $3, 'https://peer.example/federation/inbox', 'peer.example') RETURNING id
	`, name+"@peer.example", name+"@peer.example", p.actorURL).Scan(&p.id); err != nil {
		t.Fatalf("seed peer: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, p.id) })
	if cacheKey {
		der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
		pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
		db.Exec(`INSERT INTO remote_actor_keys (actor_url, public_key_pem, fetched_at) VALUES ($1, $2, NOW())
		         ON CONFLICT (actor_url) DO UPDATE SET public_key_pem = $2`, p.actorURL, pubPEM)
		t.Cleanup(func() { db.Exec(`DELETE FROM remote_actor_keys WHERE actor_url = $1`, p.actorURL) })
	}
	return p
}

func befriend(t *testing.T, db *store.DB, a, b string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'accepted')`, a, b); err != nil {
		t.Fatalf("befriend: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1 AND addressee_id = $2`, a, b) })
}

// signedGet builds a GET for username's actor document signed by p, dated at.
func signedGet(t *testing.T, p remotePeer, username string, at time.Time) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "https://agora366.example/federation/users/"+username, nil)
	r.Header.Set("Date", at.UTC().Format(http.TimeFormat))
	r.Header.Set("Digest", "SHA-256=47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=")
	headers := strings.Fields(signedHeaderList)
	hashed := sha256.Sum256([]byte(buildSigningString(r, headers)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Signature", fmt.Sprintf(`keyId="%s#main-key",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		p.actorURL, signedHeaderList, base64.StdEncoding.EncodeToString(sig)))
	return r
}

func fetchActor(t *testing.T, s *Service, r *http.Request, username string) (map[string]any, http.Header) {
	t.Helper()
	w := httptest.NewRecorder()
	s.writeActorObject(w, r, username)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var obj map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return obj, w.Header()
}

func TestPrivateActorObjectShownToFriendsOnly(t *testing.T) {
	db := agora366TestDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	localID, username := seedAgora366User(t, db, "agora381", true)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora366.example"}}

	friend := seedRemotePeer(t, db, true)
	befriend(t, db, friend.id, localID)
	stranger := seedRemotePeer(t, db, true)
	uncachedFriend := seedRemotePeer(t, db, false)
	befriend(t, db, localID, uncachedFriend.id)

	full := func(obj map[string]any) bool { return obj["name"] != nil && obj["icon"] != nil && obj["image"] != nil }

	t.Run("a friend's signed fetch gets the full profile", func(t *testing.T) {
		obj, h := fetchActor(t, s, signedGet(t, friend, username, time.Now()), username)
		if !full(obj) {
			t.Fatalf("friend got a redacted profile: %v", obj)
		}
		if !strings.Contains(h.Get("Cache-Control"), "no-store") {
			t.Errorf("Cache-Control = %q, want no-store so no proxy can reuse a friend's copy", h.Get("Cache-Control"))
		}
	})

	for name, r := range map[string]*http.Request{
		"unsigned":                              httptest.NewRequest(http.MethodGet, "https://agora366.example/federation/users/"+username, nil),
		"signed by a non-friend":                signedGet(t, stranger, username, time.Now()),
		"signed by a friend with no cached key": signedGet(t, uncachedFriend, username, time.Now()),
		"a friend's fetch replayed hours later": signedGet(t, friend, username, time.Now().Add(-3*time.Hour)),
	} {
		t.Run(name+" stays redacted", func(t *testing.T) {
			if obj, _ := fetchActor(t, s, r, username); full(obj) || obj["name"] != nil || obj["icon"] != nil {
				t.Fatalf("got profile content: %v", obj)
			}
		})
	}

	t.Run("a friend's signature presented for another path is refused", func(t *testing.T) {
		r := signedGet(t, friend, username, time.Now())
		r.URL.Path = "/federation/users/someone_else"
		if _, err := s.verifySignedFetchCached(r); err == nil {
			t.Fatal("signature verified against a different request target")
		}
	})
}

func TestPrivateProfileUpdateGoesToFriendsOnly(t *testing.T) {
	db := agora366TestDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	localID, _ := seedAgora366User(t, db, "agora381b", true)
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora366.example"}}

	friend := seedRemotePeer(t, db, false)
	befriend(t, db, localID, friend.id)
	follower := seedRemotePeer(t, db, false)
	db.Exec(`INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted) VALUES ($1, $2, $3, true)`,
		localID, follower.actorURL, "https://peer.example/federation/inbox")
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1`, localID) })

	got := s.remoteFriendInboxes(localID)
	if _, ok := got[friend.actorURL]; !ok || len(got) != 1 {
		t.Fatalf("remoteFriendInboxes = %v, want exactly the friend %s", got, friend.actorURL)
	}

	t.Run("refreshing a private friend signs as the friend", func(t *testing.T) {
		// Another local user merely follows the friend peer. Fetching the
		// friend must sign as their friend, not as that follower, or a private
		// profile comes back redacted.
		other, _ := seedAgora366User(t, db, "agora381c", false)
		db.Exec(`INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url, accepted) VALUES ($1, $2, $3, true)`,
			other, friend.actorURL, "https://peer.example/federation/inbox")
		t.Cleanup(func() { db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1`, other) })
		if got := s.signerUserIDForActorFetch(friend.actorURL); got != localID {
			t.Errorf("signer = %q, want the friend %q", got, localID)
		}
	})
}

// AGORA-381: on-demand refresh. Only a cached remote account can be
// refreshed, a second request inside the cooldown does nothing (no network),
// and only one bulk refresh runs at a time.
func TestRefreshRemoteProfileRules(t *testing.T) {
	db := agora366TestDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora366.example"}}

	localID, _ := seedAgora366User(t, db, "agora381d", false)
	if err := s.RefreshRemoteProfile(localID); err != ErrNotRefreshable {
		t.Errorf("local account: err = %v, want ErrNotRefreshable", err)
	}

	peer := seedRemotePeer(t, db, false)
	db.Exec(`UPDATE users SET remote_synced_at = NOW() WHERE id = $1`, peer.id)
	// peer.example doesn't resolve, so reaching the network here would fail;
	// nil means the cooldown short-circuited before any fetch.
	if err := s.RefreshRemoteProfile(peer.id); err != nil {
		t.Errorf("just-synced account: err = %v, want nil (within the cooldown)", err)
	}

	bulkRefreshRunning.Store(true)
	defer bulkRefreshRunning.Store(false)
	if s.RefreshAllRemoteProfiles() {
		t.Error("started a second bulk refresh while one was running")
	}
}

// AGORA-383: a new friendship with a remote user refreshes their profile, so
// a private one's picture shows up straight away instead of at the next
// hourly refresh. Accepting their request waits for our Accept to land first;
// their accepting ours needs no wait.
func TestFriendAcceptanceRefreshesProfile(t *testing.T) {
	db := agora366TestDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora366.example"}}

	type call struct {
		remoteUserID string
		delays       []time.Duration
	}
	var calls []call
	orig := scheduleFriendProfileRefresh
	scheduleFriendProfileRefresh = func(_ *Service, remoteUserID string, delays []time.Duration) {
		calls = append(calls, call{remoteUserID, delays})
	}
	t.Cleanup(func() { scheduleFriendProfileRefresh = orig })

	t.Run("they accept our request: refresh right away", func(t *testing.T) {
		calls = nil
		localID, _ := seedAgora366User(t, db, "agora383a", false)
		peer := seedRemotePeer(t, db, false)
		if _, err := db.Exec(`INSERT INTO friendships (requester_id, addressee_id, status) VALUES ($1, $2, 'pending')`, localID, peer.id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM friendships WHERE requester_id = $1`, localID) })

		s.handleInboundFriendAcceptAP(localID, peer.id)
		if len(calls) != 1 || calls[0].remoteUserID != peer.id || len(calls[0].delays) != 1 || calls[0].delays[0] != 0 {
			t.Fatalf("calls = %+v, want one immediate refresh of %s", calls, peer.id)
		}

		// A repeated Accept for a friendship already accepted changes nothing
		// and schedules nothing.
		calls = nil
		s.handleInboundFriendAcceptAP(localID, peer.id)
		if len(calls) != 0 {
			t.Errorf("duplicate Accept scheduled %+v", calls)
		}
	})

	t.Run("we accept their request: refresh once our Accept has landed", func(t *testing.T) {
		calls = nil
		localID, _ := seedAgora366User(t, db, "agora383b", false)
		peer := seedRemotePeer(t, db, false)
		t.Cleanup(func() { db.Exec(`DELETE FROM ap_delivery_queue WHERE actor_user_id = $1`, localID) })

		s.SendFriendAccept(localID, peer.id)
		if len(calls) != 1 || calls[0].remoteUserID != peer.id {
			t.Fatalf("calls = %+v, want a refresh of %s", calls, peer.id)
		}
		if d := calls[0].delays; len(d) == 0 || d[0] < 20*time.Second {
			t.Errorf("delays = %v, want the first one to wait out the 20s delivery queue", d)
		}
	})

	t.Run("an instance-initiated refresh skips the per-user cooldown", func(t *testing.T) {
		peer := seedRemotePeer(t, db, false)
		db.Exec(`UPDATE users SET remote_synced_at = NOW() WHERE id = $1`, peer.id)
		// peer.example doesn't resolve: an error means a fetch was attempted,
		// where RefreshRemoteProfile would have stopped at the cooldown.
		if err := s.refreshRemoteUser(peer.id); err == nil {
			t.Error("refreshRemoteUser returned nil for a just-synced account; it should not apply the cooldown")
		}
	})
}
