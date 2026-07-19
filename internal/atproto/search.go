package atproto

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/agora-social/agora/internal/auth"
)

// Network-wide Bluesky account search (AGORA-215) — genuinely different
// from anything on the Fediverse side of the unified-search epic (AGORA-212):
// there's no fediverse-wide search API, only ever content this instance
// already knows about, whereas app.bsky.actor.searchActors queries the
// entire Bluesky network through the same public AppView client AGORA-195's
// resolveBlueskyActor already uses for exact-handle resolution.

const defaultActorSearchLimit = 25

// SearchBlueskyActors serves a fuzzy, network-wide Bluesky account search —
// the AT Proto counterpart of internal/search.SearchUsers, but never limited
// to accounts this instance has already cached, unlike that endpoint's own
// (accidental) inclusion of remote stub rows.
func (s *Service) SearchBlueskyActors(w http.ResponseWriter, r *http.Request) {
	// AGORA-193: same instance-wide + per-account gating every other AT
	// Proto endpoint applies — a disabled toggle means no results, not an
	// error, since a search box shouldn't surface a scary-looking failure
	// just because the viewer (or the instance) opted out of Bluesky.
	if !s.atprotoEnabled() {
		writeJSON(w, 200, map[string]any{"actors": []any{}, "disabled": true})
		return
	}
	viewerID := auth.UserIDFromCtx(r.Context())
	var viewerAtprotoEnabled bool
	s.db.QueryRow(`SELECT atproto_enabled FROM users WHERE id = $1`, viewerID).Scan(&viewerAtprotoEnabled)
	if !viewerAtprotoEnabled {
		writeJSON(w, 200, map[string]any{"actors": []any{}, "disabled": true})
		return
	}

	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		writeJSON(w, 200, map[string]any{"actors": []any{}})
		return
	}
	limit := int64(defaultActorSearchLimit)
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 100 {
		limit = int64(n)
	}

	out, err := bsky.ActorSearchActors(r.Context(), s.appviewClient(), "", limit, q, "")
	if err != nil {
		writeError(w, 502, "could not search Bluesky")
		return
	}

	type actorResult struct {
		DID         string `json:"did"`
		Handle      string `json:"handle"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Description string `json:"description"`
	}

	results := []actorResult{}
	for _, a := range out.Actors {
		if a == nil {
			continue
		}
		// AGORA-205: a blocked DID/PDS-host domain is filtered out entirely
		// rather than shown-but-unfollowable — matching how a blocked actor
		// is invisible to the exact-resolve path (followBlueskyActor)
		// rather than surfaced with a disabled follow button.
		if s.isBlueskyActorBlocked(a.Did, a.Handle) {
			continue
		}
		res := actorResult{DID: a.Did, Handle: a.Handle}
		if a.DisplayName != nil {
			res.DisplayName = *a.DisplayName
		}
		if a.Avatar != nil {
			res.AvatarURL = *a.Avatar
		}
		if a.Description != nil {
			res.Description = *a.Description
		}
		results = append(results, res)
	}
	writeJSON(w, 200, map[string]any{"actors": results})
}

const defaultPostSearchLimit = 25

// SearchBlueskyPosts serves a network-wide Bluesky post/hashtag search
// (AGORA-216) — the read-only counterpart of AGORA-197's own ingestion.
// A search result is shown on demand only: unlike ingestAuthorFeed, this
// never writes to the local posts table and never creates a remote user
// stub, since a viewer merely looking at search results shouldn't leave any
// standing local trace the way an actual follow/subscription does.
func (s *Service) SearchBlueskyPosts(w http.ResponseWriter, r *http.Request) {
	if !s.atprotoEnabled() {
		writeJSON(w, 200, map[string]any{"posts": []any{}, "disabled": true})
		return
	}
	viewerID := auth.UserIDFromCtx(r.Context())
	var viewerAtprotoEnabled bool
	s.db.QueryRow(`SELECT atproto_enabled FROM users WHERE id = $1`, viewerID).Scan(&viewerAtprotoEnabled)
	if !viewerAtprotoEnabled {
		writeJSON(w, 200, map[string]any{"posts": []any{}, "disabled": true})
		return
	}

	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		writeJSON(w, 200, map[string]any{"posts": []any{}})
		return
	}
	limit := int64(defaultPostSearchLimit)
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 100 {
		limit = int64(n)
	}

	// A bare "#tag" query is routed to the tag filter (AGORA-214's own
	// hashtagFromQuery convention for the local-search side of this same
	// epic), leaving q itself empty — app.bsky.feed.searchPosts requires a
	// non-empty q, so a hashtag-only search reuses the tag as q too.
	var tags []string
	if strings.HasPrefix(strings.TrimSpace(q), "#") {
		tag := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(q), "#"))
		if tag == "" {
			writeJSON(w, 200, map[string]any{"posts": []any{}})
			return
		}
		tags = []string{tag}
	}

	// AGORA-216: app.bsky.feed.searchPosts, unlike every other AT Proto read
	// this codebase makes, 403s the anonymous public AppView client — it
	// needs a real authenticated session (a dedicated bot account, session.go).
	client, err := s.authedAppviewClient(r.Context())
	if err != nil {
		log.Printf("atproto: could not get authenticated Bluesky session for search: %v", err)
		writeError(w, 502, "could not search Bluesky")
		return
	}

	out, err := bsky.FeedSearchPosts(r.Context(), client, "", "", "", "", limit, "", q, "", "", tags, "", "")
	if err != nil {
		var xerr *xrpc.Error
		if errors.As(err, &xerr) && xerr.StatusCode == http.StatusUnauthorized && client.Auth != nil {
			if refreshed, rerr := s.refreshBotSession(r.Context(), client.Auth.RefreshJwt); rerr == nil {
				out, err = bsky.FeedSearchPosts(r.Context(), refreshed, "", "", "", "", limit, "", q, "", "", tags, "", "")
			}
		}
	}
	if err != nil {
		log.Printf("atproto: Bluesky post search failed: %v", err)
		writeError(w, 502, "could not search Bluesky")
		return
	}

	type postResult struct {
		URI          string `json:"uri"`
		AuthorDID    string `json:"author_did"`
		AuthorHandle string `json:"author_handle"`
		AuthorName   string `json:"author_display_name"`
		AuthorAvatar string `json:"author_avatar_url"`
		Text         string `json:"text"`
		CreatedAt    string `json:"created_at"`
		LikeCount    int64  `json:"like_count"`
		RepostCount  int64  `json:"repost_count"`
	}

	results := []postResult{}
	for _, p := range out.Posts {
		if p == nil || p.Author == nil {
			continue
		}
		// AGORA-205: same block-list enforcement as AGORA-215's account
		// search — a blocked author's posts simply don't appear.
		if s.isBlueskyActorBlocked(p.Author.Did, p.Author.Handle) {
			continue
		}
		rec, ok := p.Record.Val.(*bsky.FeedPost)
		if !ok || rec == nil {
			continue
		}
		res := postResult{
			URI:          p.Uri,
			AuthorDID:    p.Author.Did,
			AuthorHandle: p.Author.Handle,
			Text:         rec.Text,
			CreatedAt:    rec.CreatedAt,
		}
		if p.Author.DisplayName != nil {
			res.AuthorName = *p.Author.DisplayName
		}
		if p.Author.Avatar != nil {
			res.AuthorAvatar = *p.Author.Avatar
		}
		if p.LikeCount != nil {
			res.LikeCount = *p.LikeCount
		}
		if p.RepostCount != nil {
			res.RepostCount = *p.RepostCount
		}
		results = append(results, res)
	}
	writeJSON(w, 200, map[string]any{"posts": results})
}
