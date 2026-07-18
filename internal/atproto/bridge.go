package atproto

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/auth"
)

// bridgyDomains are known Bridgy Fed bridge domains a Bluesky account shows
// up under as a fediverse actor (AGORA-196) — bsky.brid.gy is the
// production one; matching by suffix also catches any subdomain variant.
var bridgyDomains = []string{"brid.gy"}

func isBridgyDomain(domain string) bool {
	domain = strings.ToLower(domain)
	for _, d := range bridgyDomains {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

type bridgedFollow struct {
	ID             string `json:"id"`
	Handle         string `json:"handle"`
	RemoteInstance string `json:"remote_instance"`
}

// ListBridgedBlueskyFollows is the dry-run/preview step (AGORA-196) —
// identifies which of the caller's existing fediverse follows (ap_following,
// AGORA-146) are actually Bluesky accounts reached through the Bridgy Fed
// bridge rather than real fediverse accounts, without changing anything.
// Bridgy Fed's bridged-actor convention is what makes this detectable at
// all: the fediverse username it presents IS the underlying Bluesky handle
// (e.g. "alice.bsky.social"), just federated under a bsky.brid.gy actor —
// see fediverseMentionRe's own comment in internal/feed/feed.go.
func (s *Service) ListBridgedBlueskyFollows(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	rows, err := s.db.Query(`
		SELECT af.id, u.username, u.remote_instance
		FROM ap_following af
		JOIN users u ON u.ap_actor_url = af.followed_actor_url
		WHERE af.follower_user_id = $1 AND af.accepted = true
	`, userID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	var list []bridgedFollow
	for rows.Next() {
		var id, username, instance string
		if rows.Scan(&id, &username, &instance) != nil {
			continue
		}
		if !isBridgyDomain(instance) {
			continue
		}
		list = append(list, bridgedFollow{ID: id, Handle: username, RemoteInstance: instance})
	}
	if list == nil {
		list = []bridgedFollow{}
	}
	writeJSON(w, 200, map[string]any{"bridged_follows": list})
}

// MigrateBridgedFollow reconciles one bridged Bluesky follow into a native
// one (AGORA-196): resolves the real Bluesky handle and writes the
// equivalent app.bsky.graph.follow record via the same path a fresh native
// follow uses, and only removes the old bridged ap_following row once that
// succeeds — a failed resolve/write leaves the original bridged follow
// untouched rather than losing it, satisfying the ticket's "not a one-way
// destructive script" requirement without needing a separate undo command.
// Migrates one follow per call rather than all-at-once so the frontend can
// show per-item progress/failure instead of an opaque bulk operation.
func (s *Service) MigrateBridgedFollow(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	apFollowingID := chi.URLParam(r, "id")

	var handle, instance string
	if err := s.db.QueryRow(`
		SELECT u.username, u.remote_instance
		FROM ap_following af JOIN users u ON u.ap_actor_url = af.followed_actor_url
		WHERE af.id = $1 AND af.follower_user_id = $2
	`, apFollowingID, userID).Scan(&handle, &instance); err != nil {
		writeError(w, 404, "bridged follow not found")
		return
	}
	if !isBridgyDomain(instance) {
		writeError(w, 400, "not a bridged Bluesky follow")
		return
	}

	id, profile, ferr := s.followBlueskyActor(r.Context(), userID, handle)
	if ferr != nil {
		writeError(w, 502, "could not migrate: "+ferr.msg)
		return
	}

	// Only remove the bridged row now that the native follow is confirmed —
	// the old fediverse-side follow relationship with the bridge actor is
	// left as-is remotely (no Undo sent); locally we simply stop reading
	// through it, since the native path is now the source of truth for this
	// account. Ingestion/notifications from the bridged path naturally stop
	// once this row is gone, so no duplicate content follows.
	s.db.Exec(`DELETE FROM ap_following WHERE id = $1 AND follower_user_id = $2`, apFollowingID, userID)
	writeJSON(w, 200, map[string]any{"message": "migrated", "id": id, "did": profile.DID, "handle": profile.Handle})
}
