package federation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/config"
	"github.com/agora-social/agora/internal/store"
)

// Requires the local agora-postgres-test instance (localhost:15433); skips
// rather than failing the suite if it isn't reachable.
func peerSuggestionsTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("test DB not reachable, skipping: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSuggestPeerUsersNoActivePeers is the common case for most instances:
// no active federated_instances rows means no cross-instance HTTP calls at
// all, and a nil/empty result rather than an error.
func TestSuggestPeerUsersNoActivePeers(t *testing.T) {
	db := peerSuggestionsTestDB(t)

	domain := fmt.Sprintf("agora364-inactive-%d.example", time.Now().UnixNano())
	db.Exec(`INSERT INTO federated_instances (domain, instance_url, status) VALUES ($1, $2, 'blocked')`,
		domain, "https://"+domain)
	t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

	got := SuggestPeerUsers(db)
	if len(got) != 0 {
		t.Errorf("SuggestPeerUsers() = %d suggestions with no active peers, want 0", len(got))
	}
}

// TestSuggestPeerUsersSkipsUnreachablePeer is the resilience property the
// whole function exists for: an active peer that can't actually be reached
// (fedHTTPClient's SSRF guard refuses to dial it here, standing in for any
// real-world timeout or DNS failure) must not turn into an error or a hang,
// only a shorter result.
func TestSuggestPeerUsersSkipsUnreachablePeer(t *testing.T) {
	db := peerSuggestionsTestDB(t)

	domain := fmt.Sprintf("agora364-unreachable-%d.example", time.Now().UnixNano())
	db.Exec(`INSERT INTO federated_instances (domain, instance_url, status) VALUES ($1, $2, 'active')`,
		domain, "https://"+domain)
	t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

	done := make(chan []PeerUserSuggestion, 1)
	go func() { done <- SuggestPeerUsers(db) }()

	select {
	case got := <-done:
		if len(got) != 0 {
			t.Errorf("SuggestPeerUsers() = %d suggestions from an unreachable peer, want 0", len(got))
		}
	case <-time.After(peerSuggestionTimeout + 5*time.Second):
		t.Fatal("SuggestPeerUsers did not return within the expected timeout budget")
	}
}

// TestSearchAcceptsEmptyQuery is AGORA-364's other half: a peer asking for a
// sample (no q at all) must get local users back, not the 400 the endpoint
// used to return without a keyword. federation_enabled still has to be on,
// since Search is this instance's own opt-in to being asked at all.
//
// Not asserting which users come back: the shared test database already has
// hundreds of real accounts from other tests, and the sample is randomly
// ordered, so any one seeded row would only rarely land in the first 20.
// The behavior under test is just that an empty q is accepted and answered.
func TestSearchAcceptsEmptyQuery(t *testing.T) {
	db := peerSuggestionsTestDB(t)

	var prevFed string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'federation_enabled'`).Scan(&prevFed)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('federation_enabled', 'true')
		ON CONFLICT (key) DO UPDATE SET value = 'true'`)
	t.Cleanup(func() { db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'federation_enabled'`, prevFed) })

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "https://agora364.example"}}

	req := httptest.NewRequest(http.MethodGet, "/federation/search?q=", nil)
	w := httptest.NewRecorder()
	s.Search(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Users []struct {
			Username string `json:"username"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Users) == 0 {
		t.Error("Search with an empty q returned no users, want a sample of local accounts")
	}
}
