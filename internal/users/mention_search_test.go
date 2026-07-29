package users

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/store"
)

// stubAtprotoSyncer is a minimal atprotoSyncer for exercising
// UnifiedMentionSearch's Bluesky block (AGORA-274) without a real AppView
// call — only SearchActorsForMention is used by that code path.
type stubAtprotoSyncer struct {
	handles, displayNames, avatarURLs []string
	disabled                          bool
}

func (s *stubAtprotoSyncer) SyncProfile(userID string) {}
func (s *stubAtprotoSyncer) FollowsMe(viewerUserID, theirDID string) bool { return false }
func (s *stubAtprotoSyncer) GetRemoteActorStats(did string) (followers, following, posts int, bio string, ok bool) {
	return 0, 0, 0, "", false
}
func (s *stubAtprotoSyncer) SearchActorsForMention(ctx context.Context, viewerID, q string, limit int) (handles, displayNames, avatarURLs []string, disabled bool) {
	return s.handles, s.displayNames, s.avatarURLs, s.disabled
}

// AGORA-274: UnifiedMentionSearch's Bluesky block should surface live
// network-wide search hits from atprotoSyncer, tagged is_remote with
// remote_instance "bsky.app" so the frontend renders/formats them the same
// as any other known Bluesky account.
func TestUnifiedMentionSearchIncludesLiveBlueskyActors(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, atproto: &stubAtprotoSyncer{
		handles:      []string{"alice.bsky.social"},
		displayNames: []string{"Alice"},
		avatarURLs:   []string{"https://cdn.example/alice.jpg"},
	}}

	suffix := time.Now().UnixNano()
	localUsername := fmt.Sprintf("agora274_local_%d", suffix)
	var callerID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id
	`, localUsername, localUsername+"@example.com").Scan(&callerID); err != nil {
		t.Fatalf("insert caller: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, callerID) })

	ctx := context.WithValue(context.Background(), ctxkeys.UserID, callerID)
	req := httptest.NewRequest("GET", "/mention-search?q=alice", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.UnifiedMentionSearch(w, req)

	var parsed struct {
		Users []struct {
			Username       string `json:"username"`
			DisplayName    string `json:"display_name"`
			AvatarURL      string `json:"avatar_url"`
			IsRemote       bool   `json:"is_remote"`
			RemoteInstance string `json:"remote_instance"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, u := range parsed.Users {
		if u.Username == "alice.bsky.social" {
			if !u.IsRemote {
				t.Errorf("expected is_remote=true for alice.bsky.social")
			}
			if u.RemoteInstance != "bsky.app" {
				t.Errorf("remote_instance = %q, want bsky.app", u.RemoteInstance)
			}
			if u.DisplayName != "Alice" {
				t.Errorf("display_name = %q, want Alice", u.DisplayName)
			}
			return
		}
	}
	t.Errorf("live Bluesky actor alice.bsky.social not found in results: %s", w.Body.Bytes())
}

// AGORA-274: when atprotoSyncer reports disabled (instance/viewer opted out
// of Bluesky), no live actors should be appended — mirrors the HTTP
// handler's own "disabled means no results, not an error" convention.
func TestUnifiedMentionSearchOmitsBlueskyWhenDisabled(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, atproto: &stubAtprotoSyncer{
		handles:  []string{"alice.bsky.social"},
		disabled: true,
	}}

	suffix := time.Now().UnixNano()
	localUsername := fmt.Sprintf("agora274b_local_%d", suffix)
	var callerID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id
	`, localUsername, localUsername+"@example.com").Scan(&callerID); err != nil {
		t.Fatalf("insert caller: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, callerID) })

	ctx := context.WithValue(context.Background(), ctxkeys.UserID, callerID)
	req := httptest.NewRequest("GET", "/mention-search?q=alice", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.UnifiedMentionSearch(w, req)

	var parsed struct {
		Users []struct {
			Username string `json:"username"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, u := range parsed.Users {
		if u.Username == "alice.bsky.social" {
			t.Errorf("did not expect alice.bsky.social when atproto reports disabled: %s", w.Body.Bytes())
		}
	}
}

// AGORA-163: the @ mention dropdown never surfaced fediverse accounts the
// user follows — UnifiedMentionSearch's "users" query hard-filtered
// is_remote = false and never looked at ap_following at all. Requires the
// local agora-postgres-test instance (localhost:15433); skips if unreachable.
func TestUnifiedMentionSearchIncludesFollowedFediverseAccounts(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	suffix := time.Now().UnixNano()
	localUsername := fmt.Sprintf("agora163_local_%d", suffix)
	var callerID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id
	`, localUsername, localUsername+"@example.com").Scan(&callerID); err != nil {
		t.Fatalf("insert caller: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, callerID) })

	remoteHandle := fmt.Sprintf("someone_%d", suffix)
	remoteDomain := "mastodon.example"
	remoteUsername := remoteHandle + "@" + remoteDomain
	actorURL := "https://" + remoteDomain + "/users/" + remoteHandle
	var remoteID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, is_remote, remote_instance, ap_actor_url)
		VALUES ($1, $2, 'x', true, $3, $4) RETURNING id
	`, remoteUsername, remoteUsername+"@remote.example", remoteDomain, actorURL).Scan(&remoteID); err != nil {
		t.Fatalf("insert remote stub: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, remoteID) })

	if _, err := db.Exec(`
		INSERT INTO ap_following (follower_user_id, followed_actor_url, followed_inbox_url)
		VALUES ($1, $2, $3)
	`, callerID, actorURL, actorURL+"/inbox"); err != nil {
		t.Fatalf("insert ap_following: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM ap_following WHERE follower_user_id = $1`, callerID) })

	ctx := context.WithValue(context.Background(), ctxkeys.UserID, callerID)

	assertRemoteHitPresent := func(t *testing.T, body []byte) {
		t.Helper()
		var parsed struct {
			Users []struct {
				Username       string `json:"username"`
				IsRemote       bool   `json:"is_remote"`
				RemoteInstance string `json:"remote_instance"`
			} `json:"users"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		for _, u := range parsed.Users {
			if u.Username == remoteUsername {
				if !u.IsRemote {
					t.Errorf("expected is_remote=true for %s", remoteUsername)
				}
				if u.RemoteInstance != remoteDomain {
					t.Errorf("remote_instance = %q, want %q", u.RemoteInstance, remoteDomain)
				}
				return
			}
		}
		t.Errorf("followed fediverse account %s not found in results: %s", remoteUsername, body)
	}

	t.Run("empty query shows recently followed fediverse accounts", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/mention-search", nil).WithContext(ctx)
		w := httptest.NewRecorder()
		s.UnifiedMentionSearch(w, req)
		assertRemoteHitPresent(t, w.Body.Bytes())
	})

	t.Run("prefix search matches the followed account's handle", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/mention-search?q="+remoteHandle[:6], nil).WithContext(ctx)
		w := httptest.NewRecorder()
		s.UnifiedMentionSearch(w, req)
		assertRemoteHitPresent(t, w.Body.Bytes())
	})
}
