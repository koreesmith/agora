package atproto

import (
	"net/http"
	"strconv"

	"github.com/bluesky-social/indigo/api/bsky"

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
