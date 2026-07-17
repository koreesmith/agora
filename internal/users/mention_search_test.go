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
