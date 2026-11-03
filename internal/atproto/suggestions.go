package atproto

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
)

// AGORA-423: Bluesky accounts to offer in Find Friends. Three sources, all
// kept off the request path as far as possible:
//
//   - accounts the viewer's Bluesky followers include but the viewer does not
//     follow back (at_followers, already maintained by the follower poll);
//   - accounts several people on this instance follow (at_following);
//   - the AppView's own "people followed by those who follow X" suggestions
//     (app.bsky.graph.getSuggestedFollowsByActor) for a few accounts the
//     viewer follows.
//
// Only the last two need a network call, so the result is cached per viewer
// for suggestionTTL rather than recomputed on every Discover load.

const (
	suggestionTTL        = 6 * time.Hour
	suggestionSeeds      = 4
	suggestionTimeout    = 5 * time.Second
	suggestionProfileMax = 25
)

type suggestionEntry struct {
	at     time.Time
	did    []string
	handle []string
	name   []string
	avatar []string
	bio    []string
	reason []string
}

var (
	suggestionMu    sync.Mutex
	suggestionCache = map[string]*suggestionEntry{}
)

type bskyCandidate struct {
	did, handle, name, avatar, bio, reason string
	score                                  int
}

// SuggestFollows returns up to limit Bluesky accounts to suggest to viewerID,
// best first, as parallel slices (the atprotoSyncer convention in
// internal/users). Nothing is returned when AT Proto is off for the instance
// or the viewer.
func (s *Service) SuggestFollows(ctx context.Context, viewerID string, limit int) (dids, handles, names, avatars, bios, reasons []string) {
	if !s.atprotoEnabled() {
		return
	}
	var enabled bool
	s.db.QueryRow(`SELECT atproto_enabled FROM users WHERE id = $1`, viewerID).Scan(&enabled)
	if !enabled {
		return
	}

	suggestionMu.Lock()
	e := suggestionCache[viewerID]
	suggestionMu.Unlock()
	if e == nil || time.Since(e.at) >= suggestionTTL {
		e = s.buildSuggestions(ctx, viewerID)
		suggestionMu.Lock()
		suggestionCache[viewerID] = e
		suggestionMu.Unlock()
	}

	n := len(e.did)
	if limit > 0 && n > limit {
		n = limit
	}
	return e.did[:n], e.handle[:n], e.name[:n], e.avatar[:n], e.bio[:n], e.reason[:n]
}

func (s *Service) buildSuggestions(ctx context.Context, viewerID string) *suggestionEntry {
	ctx, cancel := context.WithTimeout(ctx, suggestionTimeout)
	defer cancel()

	followed := map[string]bool{}
	var seeds []string
	if rows, err := s.db.Query(`SELECT remote_did FROM at_following WHERE local_user_id = $1 ORDER BY created_at DESC`, viewerID); err == nil {
		for rows.Next() {
			var did string
			if rows.Scan(&did) == nil {
				followed[did] = true
				if len(seeds) < suggestionSeeds {
					seeds = append(seeds, did)
				}
			}
		}
		rows.Close()
	}

	cands := map[string]*bskyCandidate{}
	add := func(did, handle, name, avatar, bio, reason string, score int) {
		if did == "" || followed[did] || s.isBlueskyActorBlocked(did, handle) {
			return
		}
		c := cands[did]
		if c == nil {
			c = &bskyCandidate{did: did, handle: handle, name: name, avatar: avatar, bio: bio, reason: reason}
			cands[did] = c
		}
		if c.reason == "" || score > c.score {
			c.reason = reason
		}
		c.score += score
	}

	// People who follow the viewer on Bluesky. The follower poll keeps
	// at_followers current but stores only the DID, so profiles come from one
	// batched getProfiles call below.
	var followerDIDs []string
	if rows, err := s.db.Query(`SELECT follower_did FROM at_followers WHERE local_user_id = $1 ORDER BY first_seen_at DESC LIMIT $2`, viewerID, suggestionProfileMax); err == nil {
		for rows.Next() {
			var did string
			if rows.Scan(&did) == nil && !followed[did] {
				followerDIDs = append(followerDIDs, did)
			}
		}
		rows.Close()
	}
	if len(followerDIDs) > 0 {
		if out, err := bsky.ActorGetProfiles(ctx, s.appviewClient(), followerDIDs); err == nil {
			for _, p := range out.Profiles {
				if p == nil {
					continue
				}
				name, avatar, bio := "", "", ""
				if p.DisplayName != nil {
					name = *p.DisplayName
				}
				if p.Avatar != nil {
					avatar = *p.Avatar
				}
				if p.Description != nil {
					bio = *p.Description
				}
				add(p.Did, p.Handle, name, avatar, bio, "Follows you on Bluesky", 40)
			}
		}
	}

	// Accounts other people on this instance follow. at_following already
	// holds their display fields.
	if rows, err := s.db.Query(`
		SELECT remote_did, MAX(remote_handle), MAX(display_name), MAX(avatar_url), COUNT(DISTINCT local_user_id)
		FROM at_following
		WHERE local_user_id <> $1 AND remote_handle <> ''
		GROUP BY remote_did
		HAVING COUNT(DISTINCT local_user_id) >= 2
		ORDER BY COUNT(DISTINCT local_user_id) DESC
		LIMIT 20
	`, viewerID); err == nil {
		for rows.Next() {
			var did, handle, name, avatar string
			var n int
			if rows.Scan(&did, &handle, &name, &avatar, &n) == nil {
				add(did, handle, name, avatar, "", fmt.Sprintf("Followed by %d people here", n), 10+n)
			}
		}
		rows.Close()
	}

	// The AppView's own suggestions, seeded from a few accounts the viewer
	// follows. An account suggested off several seeds ranks higher.
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, seed := range seeds {
		wg.Add(1)
		go func(seed string) {
			defer wg.Done()
			out, err := bsky.GraphGetSuggestedFollowsByActor(ctx, s.appviewClient(), seed)
			if err != nil || out == nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, a := range out.Suggestions {
				if a == nil {
					continue
				}
				name, avatar, bio := "", "", ""
				if a.DisplayName != nil {
					name = *a.DisplayName
				}
				if a.Avatar != nil {
					avatar = *a.Avatar
				}
				if a.Description != nil {
					bio = *a.Description
				}
				add(a.Did, a.Handle, name, avatar, bio, "Similar to accounts you follow", 15)
			}
		}(seed)
	}
	wg.Wait()

	list := make([]*bskyCandidate, 0, len(cands))
	for _, c := range cands {
		list = append(list, c)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		return list[i].handle < list[j].handle
	})

	e := &suggestionEntry{at: time.Now()}
	for _, c := range list {
		e.did = append(e.did, c.did)
		e.handle = append(e.handle, c.handle)
		e.name = append(e.name, c.name)
		e.avatar = append(e.avatar, c.avatar)
		e.bio = append(e.bio, c.bio)
		e.reason = append(e.reason, c.reason)
	}
	return e
}
