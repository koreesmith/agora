package atproto

import (
	"context"
	"log"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/ipfs/go-cid"
)

// blueskyRef resolves a local post/comment id to a Bluesky-visible strong
// ref (uri+cid) if it has one — either ingested from Bluesky (AGORA-197's
// remote_post_id/remote_post_cid) or broadcast there by Agora itself (an
// atproto_posts row, AGORA-190). Returns ok=false for a purely local post/
// comment with no Bluesky presence at all.
func (s *Service) blueskyRef(postID string) (uri, recordCid string, ok bool) {
	var isRemote bool
	var remoteInstance, remotePostID, remotePostCid string
	if err := s.db.QueryRow(`
		SELECT is_remote, remote_instance, remote_post_id, remote_post_cid
		FROM posts WHERE id = $1 AND deleted_at IS NULL
	`, postID).Scan(&isRemote, &remoteInstance, &remotePostID, &remotePostCid); err != nil {
		return "", "", false
	}
	if isRemote && remoteInstance == "bsky.app" && remotePostID != "" && remotePostCid != "" {
		return remotePostID, remotePostCid, true
	}

	var did, rkey, cidStr string
	if err := s.db.QueryRow(`
		SELECT u.atproto_did, ap.rkey, ap.record_cid
		FROM atproto_posts ap JOIN users u ON u.id = ap.user_id
		WHERE ap.post_id = $1
	`, postID).Scan(&did, &rkey, &cidStr); err != nil || did == "" || rkey == "" || cidStr == "" {
		return "", "", false
	}
	return "at://" + did + "/app.bsky.feed.post/" + rkey, cidStr, true
}

// rootPostIDFor walks parent_id up to the top-level post, mirroring
// federation's own resolveReplyTarget walk (activitypub.go) — Agora's
// comment tree is capped at two levels deep (root -> comment -> reply), so
// this loop terminates in at most a couple hops, but is written as a bounded
// loop rather than assuming an exact depth.
func (s *Service) rootPostIDFor(postID string) string {
	current := postID
	for i := 0; i < 10; i++ {
		var parentID *string
		if err := s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, current).Scan(&parentID); err != nil {
			return ""
		}
		if parentID == nil {
			return current
		}
		current = *parentID
	}
	return current
}

// DeliverReply writes a new app.bsky.feed.post record with a reply field set
// (AGORA-199) — the AT Proto counterpart to federation's DeliverReply. AT
// Proto requires both the immediate parent's and the thread root's strong
// refs in every reply record, unlike ActivityPub's single inReplyTo — so
// unlike DeliverReply, this resolves two targets, not one, and
// conservatively skips entirely if either is unresolvable rather than
// fabricating a root ref from the parent's.
func (s *Service) DeliverReply(userID, commentID, replyToID string) {
	if !s.atprotoEnabled() {
		return
	}
	ctx := context.Background()

	var username, content, did, storedPriv, repoHead, repoRev string
	var visibility string
	var profilePrivate, isRemote, atprotoEnabled bool
	var createdAt time.Time
	if err := s.db.QueryRowContext(ctx, `
		SELECT u.username, u.profile_private, u.is_remote, u.atproto_enabled,
		       u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev,
		       p.visibility, p.content, p.created_at
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, commentID, userID).Scan(&username, &profilePrivate, &isRemote, &atprotoEnabled,
		&did, &storedPriv, &repoHead, &repoRev, &visibility, &content, &createdAt); err != nil ||
		visibility != "public" || profilePrivate || isRemote || !atprotoEnabled {
		return
	}

	parentURI, parentCid, parentOK := s.blueskyRef(replyToID)
	if !parentOK {
		return
	}
	rootID := s.rootPostIDFor(replyToID)
	rootURI, rootCid, rootOK := s.blueskyRef(rootID)
	if !rootOK {
		return
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve identity for user %s: %v", userID, err)
		return
	}

	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)

	rec := &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          content,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
		Embed:         s.buildImageEmbed(ctx, bs, commentID),
		Reply: &bsky.FeedPost_ReplyRef{
			Parent: &comatproto.RepoStrongRef{Uri: parentURI, Cid: parentCid},
			Root:   &comatproto.RepoStrongRef{Uri: rootURI, Cid: rootCid},
		},
	}
	recordCid, rkey, err := repo.CreateRecord(ctx, "app.bsky.feed.post", rec)
	if err != nil {
		log.Printf("atproto: could not write reply record for comment %s: %v", commentID, err)
		return
	}

	path := "app.bsky.feed.post/" + rkey
	link := lexutil.LexLink(recordCid)
	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "create", Path: path, Cid: &link}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		log.Printf("atproto: could not commit reply %s: %v", commentID, err)
		return
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO atproto_posts (post_id, user_id, rkey, record_cid) VALUES ($1, $2, $3, $4)
		ON CONFLICT (post_id) DO UPDATE SET rkey = $3, record_cid = $4
	`, commentID, userID, rkey, recordCid.String()); err != nil {
		log.Printf("atproto: could not persist record mapping for reply %s: %v", commentID, err)
		return
	}

	log.Printf("atproto: federated reply %s as %s", commentID, path)
}

// DeliverReplyUpdate mirrors BroadcastPostUpdate but recomputes and
// preserves the reply field (AGORA-199) — BroadcastPostUpdate itself can't
// be reused here since it rebuilds the record with no Reply ref at all,
// which would silently drop the thread link on every edit.
func (s *Service) DeliverReplyUpdate(userID, commentID, replyToID string) {
	if !s.atprotoEnabled() {
		return
	}
	ctx := context.Background()

	var username, content, did, storedPriv, repoHead, repoRev, rkey, oldCidStr string
	var visibility string
	var profilePrivate, isRemote, atprotoEnabled bool
	var createdAt time.Time
	if err := s.db.QueryRowContext(ctx, `
		SELECT u.username, u.profile_private, u.is_remote, u.atproto_enabled,
		       u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev,
		       p.visibility, p.content, p.created_at, ap.rkey, ap.record_cid
		FROM posts p
		JOIN users u ON u.id = p.author_id
		JOIN atproto_posts ap ON ap.post_id = p.id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, commentID, userID).Scan(&username, &profilePrivate, &isRemote, &atprotoEnabled,
		&did, &storedPriv, &repoHead, &repoRev, &visibility, &content, &createdAt, &rkey, &oldCidStr); err != nil ||
		visibility != "public" || profilePrivate || isRemote || !atprotoEnabled {
		return // never federated in the first place (no Bluesky target when created) — nothing to update
	}

	parentURI, parentCid, parentOK := s.blueskyRef(replyToID)
	if !parentOK {
		return
	}
	rootID := s.rootPostIDFor(replyToID)
	rootURI, rootCid, rootOK := s.blueskyRef(rootID)
	if !rootOK {
		return
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve identity for user %s: %v", userID, err)
		return
	}

	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)

	rec := &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          content,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
		Embed:         s.buildImageEmbed(ctx, bs, commentID),
		Reply: &bsky.FeedPost_ReplyRef{
			Parent: &comatproto.RepoStrongRef{Uri: parentURI, Cid: parentCid},
			Root:   &comatproto.RepoStrongRef{Uri: rootURI, Cid: rootCid},
		},
	}
	path := "app.bsky.feed.post/" + rkey
	recordCid, err := repo.UpdateRecord(ctx, path, rec)
	if err != nil {
		log.Printf("atproto: could not update reply record for comment %s: %v", commentID, err)
		return
	}

	link := lexutil.LexLink(recordCid)
	op := &comatproto.SyncSubscribeRepos_RepoOp{Action: "update", Path: path, Cid: &link}
	if oldCid, err := cid.Decode(oldCidStr); err == nil {
		prev := lexutil.LexLink(oldCid)
		op.Prev = &prev
	}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, []*comatproto.SyncSubscribeRepos_RepoOp{op}); err != nil {
		log.Printf("atproto: could not commit reply update %s: %v", commentID, err)
		return
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE atproto_posts SET record_cid = $1 WHERE post_id = $2`, recordCid.String(), commentID); err != nil {
		log.Printf("atproto: could not persist updated record mapping for reply %s: %v", commentID, err)
		return
	}

	log.Printf("atproto: updated federated reply %s at %s", commentID, path)
}
