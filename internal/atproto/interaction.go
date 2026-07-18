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

// DeliverLike writes an app.bsky.feed.like record (AGORA-201) — the AT
// Proto counterpart to federation's DeliverLike. No-ops if the target has
// no Bluesky presence at all (blueskyRef, shared with reply.go's outbound
// reply-target resolution).
func (s *Service) DeliverLike(userID, postID string) {
	s.deliverInteraction(userID, postID, "like")
}

// DeliverUnlike deletes a previously-written app.bsky.feed.like record.
// Deliberately does not gate on the liker's own atproto_enabled — same
// reasoning DeliverUnlike's AP counterpart gives: retracting a previous
// action should still propagate even after opting out.
func (s *Service) DeliverUnlike(userID, postID string) {
	s.undoInteraction(userID, postID, "like")
}

// DeliverAnnounce writes an app.bsky.feed.repost record (AGORA-201) — the AT
// Proto counterpart to federation's DeliverAnnounce. repostID (Agora's own
// local repost post id) isn't used here — unlike AP's Announce activity,
// an AT Proto repost record has no separate activity id of its own to
// construct, so only originalPostID matters.
func (s *Service) DeliverAnnounce(userID, repostID, originalPostID string) {
	s.deliverInteraction(userID, originalPostID, "repost")
}

// DeliverUnannounce mirrors DeliverUnlike for a repost.
func (s *Service) DeliverUnannounce(userID, repostID, originalPostID string) {
	s.undoInteraction(userID, originalPostID, "repost")
}

func (s *Service) deliverInteraction(userID, postID, kind string) {
	if !s.atprotoEnabled() {
		return
	}
	ctx := context.Background()

	targetURI, targetCid, ok := s.blueskyRef(postID)
	if !ok {
		return
	}

	var username, did, storedPriv, repoHead, repoRev string
	var atprotoEnabled bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT username, atproto_enabled, atproto_did, atproto_private_key, atproto_repo_head, atproto_repo_rev
		FROM users WHERE id = $1 AND deletion_scheduled_at IS NULL
	`, userID).Scan(&username, &atprotoEnabled, &did, &storedPriv, &repoHead, &repoRev); err != nil || !atprotoEnabled {
		return
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve identity for user %s: %v", userID, err)
		return
	}

	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)
	subject := &comatproto.RepoStrongRef{Uri: targetURI, Cid: targetCid}
	now := time.Now().UTC().Format(time.RFC3339)

	var nsid string
	var recordCid cid.Cid
	var rkey string
	switch kind {
	case "like":
		nsid = "app.bsky.feed.like"
		recordCid, rkey, err = repo.CreateRecord(ctx, nsid, &bsky.FeedLike{
			LexiconTypeID: nsid, CreatedAt: now, Subject: subject,
		})
	case "repost":
		nsid = "app.bsky.feed.repost"
		recordCid, rkey, err = repo.CreateRecord(ctx, nsid, &bsky.FeedRepost{
			LexiconTypeID: nsid, CreatedAt: now, Subject: subject,
		})
	default:
		return
	}
	if err != nil {
		log.Printf("atproto: could not write %s record for post %s: %v", kind, postID, err)
		return
	}

	path := nsid + "/" + rkey
	link := lexutil.LexLink(recordCid)
	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "create", Path: path, Cid: &link}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		log.Printf("atproto: could not commit %s for post %s: %v", kind, postID, err)
		return
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO atproto_reactions (post_id, user_id, kind, rkey, record_cid)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (post_id, user_id, kind) DO UPDATE SET rkey = $4, record_cid = $5
	`, postID, userID, kind, rkey, recordCid.String()); err != nil {
		log.Printf("atproto: could not persist %s record mapping for post %s: %v", kind, postID, err)
		return
	}

	log.Printf("atproto: federated %s on %s as %s", kind, postID, path)
}

func (s *Service) undoInteraction(userID, postID, kind string) {
	ctx := context.Background()

	var did, storedPriv, repoHead, repoRev, rkey string
	if err := s.db.QueryRowContext(ctx, `
		SELECT u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev, ar.rkey
		FROM atproto_reactions ar JOIN users u ON u.id = ar.user_id
		WHERE ar.post_id = $1 AND ar.user_id = $2 AND ar.kind = $3
	`, postID, userID, kind).Scan(&did, &storedPriv, &repoHead, &repoRev, &rkey); err != nil {
		return // never federated in the first place — nothing to undo
	}

	priv, err := s.getOrCreateSigningKey(userID, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve signing key for user %s: %v", userID, err)
		return
	}

	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)
	path := "app.bsky.feed." + kind + "/" + rkey
	if err := repo.DeleteRecord(ctx, path); err != nil {
		log.Printf("atproto: could not delete %s record for post %s: %v", kind, postID, err)
		return
	}

	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "delete", Path: path}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		log.Printf("atproto: could not commit %s undo for post %s: %v", kind, postID, err)
		return
	}

	s.db.ExecContext(ctx, `DELETE FROM atproto_reactions WHERE post_id = $1 AND user_id = $2 AND kind = $3`, postID, userID, kind)
	log.Printf("atproto: undid %s on %s at %s", kind, postID, path)
}
