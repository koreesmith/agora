package atproto

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/auth"
)

// defaultAppviewHost is the Bluesky AppView Agora resolves handles/profiles
// against (AGORA-195) — admin-overridable via the same instance_settings
// key/value pattern relay.go's atproto_relay_host uses.
const defaultAppviewHost = "public.api.bsky.app"

func (s *Service) appviewHost() string {
	var host string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_appview_host'`).Scan(&host)
	if host == "" {
		return defaultAppviewHost
	}
	return host
}

func (s *Service) appviewClient() *xrpc.Client {
	return &xrpc.Client{Client: relayHTTPClient, Host: "https://" + s.appviewHost()}
}

// blueskyPreview is what both ResolveBlueskyHandle and FollowBlueskyAccount
// need — a single app.bsky.actor.getProfile call resolves a handle straight
// to a DID plus live profile fields in one round trip (the AppView already
// indexes the whole network), simpler than a separate
// com.atproto.identity.resolveHandle call first.
type blueskyPreview struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Description string `json:"description"`
}

func (s *Service) resolveBlueskyActor(ctx context.Context, actor string) (*blueskyPreview, error) {
	profile, err := bsky.ActorGetProfile(ctx, s.appviewClient(), actor)
	if err != nil {
		return nil, err
	}
	p := &blueskyPreview{DID: profile.Did, Handle: profile.Handle}
	if profile.DisplayName != nil {
		p.DisplayName = *profile.DisplayName
	}
	if profile.Avatar != nil {
		p.AvatarURL = *profile.Avatar
	}
	if profile.Description != nil {
		p.Description = *profile.Description
	}
	return p, nil
}

// ResolveBlueskyHandle serves the "search a Bluesky handle" step (AGORA-195)
// — the AT Proto counterpart to federation's APLookup, resolving a handle or
// DID to a live preview before the caller decides to follow.
func (s *Service) ResolveBlueskyHandle(w http.ResponseWriter, r *http.Request) {
	actor := r.URL.Query().Get("handle")
	if actor == "" {
		writeError(w, 400, "handle required")
		return
	}
	preview, err := s.resolveBlueskyActor(r.Context(), actor)
	if err != nil {
		writeError(w, 404, "could not resolve that Bluesky handle")
		return
	}
	writeJSON(w, 200, preview)
}

// FollowBlueskyAccount writes an app.bsky.graph.follow record into the
// caller's own repo (AGORA-195) — an AT Proto follow is a public repo write,
// not an inbox-delivered activity, so there's no Accept/Reject to wait on;
// it's visible on Bluesky's side (and this endpoint returns) as soon as the
// commit lands. Re-resolves the actor server-side rather than trusting a
// client-supplied DID/handle pairing, same defense-in-depth reasoning
// FollowFediverseAccount's inbox re-derivation uses.
func (s *Service) FollowBlueskyAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Actor string `json:"actor"` // a handle or DID
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Actor == "" {
		writeError(w, 400, "actor required")
		return
	}

	id, profile, err := s.followBlueskyActor(r.Context(), userID, req.Actor)
	if err != nil {
		writeError(w, err.code, err.msg)
		return
	}
	writeJSON(w, 201, map[string]string{"id": id, "did": profile.DID, "handle": profile.Handle})
}

// followErr carries the HTTP status a caller should surface alongside the
// error, since followBlueskyActor is shared between an HTTP handler
// (FollowBlueskyAccount) and an internal caller (MigrateBridgedFollow,
// AGORA-196) that doesn't have a request/response of its own.
type followErr struct {
	code int
	msg  string
}

func (e *followErr) Error() string { return e.msg }

// followBlueskyActor is FollowBlueskyAccount's core, extracted so
// MigrateBridgedFollow (AGORA-196) can follow natively without going through
// an HTTP round trip. Re-resolves the actor and identity itself rather than
// trusting a caller-supplied DID, same defense-in-depth reasoning
// FollowBlueskyAccount's HTTP path already used.
func (s *Service) followBlueskyActor(ctx context.Context, userID, actor string) (string, *blueskyPreview, *followErr) {
	if !s.atprotoEnabled() {
		return "", nil, &followErr{404, "AT Proto not enabled"}
	}
	var username, did, storedPriv, repoHead, repoRev string
	var atprotoEnabled bool
	if err := s.db.QueryRow(`
		SELECT username, atproto_did, atproto_private_key, atproto_repo_head, atproto_repo_rev, atproto_enabled
		FROM users WHERE id = $1 AND deletion_scheduled_at IS NULL
	`, userID).Scan(&username, &did, &storedPriv, &repoHead, &repoRev, &atprotoEnabled); err != nil || !atprotoEnabled {
		return "", nil, &followErr{404, "AT Proto not enabled"}
	}

	profile, err := s.resolveBlueskyActor(ctx, actor)
	if err != nil {
		return "", nil, &followErr{404, "could not resolve that Bluesky account"}
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		return "", nil, &followErr{500, "could not resolve identity"}
	}

	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)
	rec := &bsky.GraphFollow{
		LexiconTypeID: "app.bsky.graph.follow",
		Subject:       profile.DID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	recordCid, rkey, err := repo.CreateRecord(ctx, "app.bsky.graph.follow", rec)
	if err != nil {
		return "", nil, &followErr{500, "could not write follow record"}
	}

	path := "app.bsky.graph.follow/" + rkey
	link := lexutil.LexLink(recordCid)
	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "create", Path: path, Cid: &link}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		return "", nil, &followErr{500, "could not commit follow"}
	}

	var id string
	if err := s.db.QueryRow(`
		INSERT INTO at_following (local_user_id, remote_did, remote_handle, display_name, avatar_url, rkey, record_cid)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (local_user_id, remote_did) DO UPDATE
		SET remote_handle = $3, display_name = $4, avatar_url = $5, rkey = $6, record_cid = $7
		RETURNING id
	`, userID, profile.DID, profile.Handle, profile.DisplayName, profile.AvatarURL, rkey, recordCid.String()).Scan(&id); err != nil {
		return "", nil, &followErr{500, "could not save follow"}
	}

	return id, profile, nil
}

// UnfollowBlueskyAccount deletes the app.bsky.graph.follow record from the
// caller's repo — an AT Proto unfollow is a record deletion, not an Undo
// activity.
func (s *Service) UnfollowBlueskyAccount(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var did, storedPriv, repoHead, repoRev, rkey string
	if err := s.db.QueryRow(`
		SELECT u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev, af.rkey
		FROM at_following af JOIN users u ON u.id = af.local_user_id
		WHERE af.id = $1 AND af.local_user_id = $2
	`, id, userID).Scan(&did, &storedPriv, &repoHead, &repoRev, &rkey); err != nil {
		writeError(w, 404, "not found")
		return
	}

	ctx := r.Context()
	priv, err := s.getOrCreateSigningKey(userID, storedPriv)
	if err != nil {
		writeError(w, 500, "could not resolve signing key")
		return
	}
	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)
	path := "app.bsky.graph.follow/" + rkey
	if err := repo.DeleteRecord(ctx, path); err != nil {
		writeError(w, 500, "could not delete follow record")
		return
	}
	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "delete", Path: path}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		writeError(w, 500, "could not commit unfollow")
		return
	}

	s.db.Exec(`DELETE FROM at_following WHERE id = $1`, id)
	writeJSON(w, 200, map[string]string{"message": "unfollowed"})
}

// ListBlueskyFollowing returns the caller's native Bluesky follows.
func (s *Service) ListBlueskyFollowing(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	// LEFT JOINs the cached remote-user stub (AGORA-197's ingestion populates
	// it on first ingested post) so the FeedBuilderModal's atproto_account
	// picker has a users.id to store as the filter value — same shape
	// federation's ListFollowing already establishes for fediverse_account.
	rows, err := s.db.Query(`
		SELECT af.id, af.remote_did, af.remote_handle, af.display_name, af.avatar_url, af.created_at,
		       COALESCE(u.id::text, '')
		FROM at_following af
		LEFT JOIN users u ON u.atproto_remote_did = af.remote_did
		WHERE af.local_user_id = $1 ORDER BY af.created_at DESC
	`, userID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	type entry struct {
		ID          string `json:"id"`
		DID         string `json:"did"`
		Handle      string `json:"handle"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		CreatedAt   string `json:"created_at"`
		UserID      string `json:"user_id,omitempty"`
	}
	var list []entry
	for rows.Next() {
		var e entry
		var createdAt time.Time
		if rows.Scan(&e.ID, &e.DID, &e.Handle, &e.DisplayName, &e.AvatarURL, &createdAt, &e.UserID) == nil {
			e.CreatedAt = createdAt.UTC().Format(time.RFC3339)
			list = append(list, e)
		}
	}
	if list == nil {
		list = []entry{}
	}
	writeJSON(w, 200, map[string]any{"following": list})
}
