package federation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// Discover suggesting users from directly federated Agora instances
// (AGORA-364). Discover's own local-only suggestions live in
// internal/users; this is the cross-instance half it calls into.
//
// These are light previews, not local user rows: creating one for every name
// on every peer just to render a suggestion list would mean an actor fetch
// per row for people nobody asked about. A viewer who actually adds one goes
// through LookupUser (username@instance) first, exactly the path a manually
// typed handle search already takes, which is what actually resolves and
// persists the account.

// PeerUserSuggestion is one row of a peer's own sample, as GET
// /federation/search on that peer already reports it.
type PeerUserSuggestion struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Instance    string `json:"instance"`
}

const (
	// peerSuggestionTimeout is deliberately shorter than fedHTTPClient's own
	// 10s: Discover is a background suggestion list, not something a viewer
	// should sit waiting on for one slow or unreachable peer.
	peerSuggestionTimeout     = 4 * time.Second
	peerSuggestionConcurrency = 5
	peerSuggestionsPerPeer    = 10
	peerSuggestionsTotal      = 30
)

// SuggestPeerUsers asks every actively-peered Agora instance for a sample of
// its own local users. A peer that's slow, unreachable, or has its own
// discovery switched off is skipped rather than failing the whole call;
// Discover has plenty to show from elsewhere either way.
func SuggestPeerUsers(db *store.DB) []PeerUserSuggestion {
	rows, err := db.Query(`SELECT domain, instance_url FROM federated_instances WHERE status = 'active'`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type peer struct{ domain, url string }
	var peers []peer
	for rows.Next() {
		var p peer
		if err := rows.Scan(&p.domain, &p.url); err == nil && p.url != "" {
			peers = append(peers, p)
		}
	}
	if len(peers) == 0 {
		return nil
	}

	var (
		mu  sync.Mutex
		all []PeerUserSuggestion
		wg  sync.WaitGroup
		sem = make(chan struct{}, peerSuggestionConcurrency)
	)

	for _, p := range peers {
		wg.Add(1)
		go func(p peer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			for _, u := range fetchPeerSample(p.domain, p.url) {
				mu.Lock()
				all = append(all, u)
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	if len(all) > peerSuggestionsTotal {
		all = all[:peerSuggestionsTotal]
	}
	return all
}

// fetchPeerSample hits one peer's GET /federation/search with no q, which
// that endpoint already treats as "some of your local users" rather than a
// keyword match.
func fetchPeerSample(domain, instanceURL string) []PeerUserSuggestion {
	ctx, cancel := context.WithTimeout(context.Background(), peerSuggestionTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(instanceURL, "/")+"/api/federation/search?q=", nil)
	if err != nil {
		return nil
	}
	resp, err := fedHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var body struct {
		Users []PeerUserSuggestion `json:"users"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil
	}

	out := make([]PeerUserSuggestion, 0, peerSuggestionsPerPeer)
	for i, u := range body.Users {
		if i >= peerSuggestionsPerPeer {
			break
		}
		if u.Instance == "" {
			u.Instance = domain
		}
		out = append(out, u)
	}
	return out
}
