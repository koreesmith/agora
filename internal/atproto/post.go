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

// BroadcastPost federates a new public post as an app.bsky.feed.post record
// (AGORA-190) — the AT Proto counterpart to federation.BroadcastPublicPost.
// Re-derives eligibility itself (defense in depth, same as BroadcastPublicPost
// does — never trusts the caller) rather than trusting the visibility the
// caller already checked.
//
// This is Create-only: editing or deleting a federated post is AGORA-202/203.
// Images are AGORA-194.
func (s *Service) BroadcastPost(userID, postID string) {
	if !s.atprotoEnabled() {
		return
	}
	ctx := context.Background()
	// Serialize with every other commit to this user's repo, held across the
	// head read below through commitAndPersist (see lockRepo/commitAndPersist).
	defer s.lockRepo(userID)()

	var username, content, contentWarning, did, storedPriv, repoHead, repoRev string
	var visibility string
	var profilePrivate, isRemote, atprotoEnabled bool
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT u.username, u.profile_private, u.is_remote, u.atproto_enabled,
		       u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev,
		       p.visibility, p.content, p.content_warning, p.created_at
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, postID, userID).Scan(&username, &profilePrivate, &isRemote, &atprotoEnabled,
		&did, &storedPriv, &repoHead, &repoRev, &visibility, &content, &contentWarning, &createdAt)
	if err != nil || visibility != "public" || profilePrivate || isRemote || !atprotoEnabled {
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
		Embed:         s.buildImageEmbed(ctx, bs, postID),
		Labels:        labelsForContentWarning(contentWarning),
	}
	recordCid, rkey, err := repo.CreateRecord(ctx, "app.bsky.feed.post", rec)
	if err != nil {
		log.Printf("atproto: could not write post record for post %s: %v", postID, err)
		return
	}

	path := "app.bsky.feed.post/" + rkey
	link := lexutil.LexLink(recordCid)
	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "create", Path: path, Cid: &link}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		log.Printf("atproto: could not commit post %s: %v", postID, err)
		return
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO atproto_posts (post_id, user_id, rkey, record_cid) VALUES ($1, $2, $3, $4)
		ON CONFLICT (post_id) DO UPDATE SET rkey = $3, record_cid = $4
	`, postID, userID, rkey, recordCid.String()); err != nil {
		log.Printf("atproto: could not persist record mapping for post %s: %v", postID, err)
		return
	}

	log.Printf("atproto: federated post %s as %s", postID, path)
}

// BroadcastPostUpdate re-writes an already-federated post's app.bsky.feed.post
// record in place (AGORA-202) — AT Proto has no separate "Update" activity
// the way AGORA-150's ActivityPub path needed; a record at an existing path
// is simply overwritten (repo.UpdateRecord, the mst.Update-keyed sibling of
// CreateRecord's insert-only Add — see SyncProfile's comment for why the two
// aren't interchangeable). No-ops silently if the post was never federated
// in the first place (no atproto_posts row — e.g. it was never public, or
// AT Proto was disabled at creation time): nothing to update.
func (s *Service) BroadcastPostUpdate(userID, postID string) {
	if !s.atprotoEnabled() {
		return
	}
	ctx := context.Background()
	// Serialize with every other commit to this user's repo, held across the
	// head read below through commitAndPersist (see lockRepo/commitAndPersist).
	defer s.lockRepo(userID)()

	var username, content, contentWarning, did, storedPriv, repoHead, repoRev, rkey, oldCidStr string
	var visibility string
	var profilePrivate, isRemote, atprotoEnabled bool
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT u.username, u.profile_private, u.is_remote, u.atproto_enabled,
		       u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev,
		       p.visibility, p.content, p.content_warning, p.created_at, ap.rkey, ap.record_cid
		FROM posts p
		JOIN users u ON u.id = p.author_id
		JOIN atproto_posts ap ON ap.post_id = p.id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, postID, userID).Scan(&username, &profilePrivate, &isRemote, &atprotoEnabled,
		&did, &storedPriv, &repoHead, &repoRev, &visibility, &content, &contentWarning, &createdAt, &rkey, &oldCidStr)
	if err != nil || visibility != "public" || profilePrivate || isRemote || !atprotoEnabled {
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
		Embed:         s.buildImageEmbed(ctx, bs, postID),
		Labels:        labelsForContentWarning(contentWarning),
	}
	path := "app.bsky.feed.post/" + rkey
	recordCid, err := repo.UpdateRecord(ctx, path, rec)
	if err != nil {
		log.Printf("atproto: could not update post record for post %s: %v", postID, err)
		return
	}

	link := lexutil.LexLink(recordCid)
	op := &comatproto.SyncSubscribeRepos_RepoOp{Action: "update", Path: path, Cid: &link}
	if oldCid, err := cid.Decode(oldCidStr); err == nil {
		prev := lexutil.LexLink(oldCid)
		op.Prev = &prev
	}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, []*comatproto.SyncSubscribeRepos_RepoOp{op}); err != nil {
		log.Printf("atproto: could not commit post update %s: %v", postID, err)
		return
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE atproto_posts SET record_cid = $1 WHERE post_id = $2
	`, recordCid.String(), postID); err != nil {
		log.Printf("atproto: could not persist updated record mapping for post %s: %v", postID, err)
		return
	}

	log.Printf("atproto: updated federated post %s at %s", postID, path)
}

// BroadcastPostDelete removes an already-federated post's repo record
// (AGORA-203) via repo.DeleteRecord — AT Proto has no separate Tombstone
// object the way AGORA-151's ActivityPub Delete does; the record is just
// gone from the repo, and the firehose commit records the deletion. Not
// gated on atprotoEnabled/atproto_enabled the way create/update are: turning
// AT Proto off should stop *new* content from publishing, but shouldn't
// strand already-published content on Bluesky forever with no way to retract
// it — removing content only reduces exposure, never increases it. No-ops
// silently if the post was never federated (no atproto_posts row).
func (s *Service) BroadcastPostDelete(userID, postID string) {
	ctx := context.Background()
	// Serialize with every other commit to this user's repo, held across the
	// head read below through commitAndPersist (see lockRepo/commitAndPersist).
	defer s.lockRepo(userID)()

	var did, storedPriv, repoHead, repoRev, rkey, oldCidStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev, ap.rkey, ap.record_cid
		FROM atproto_posts ap
		JOIN users u ON u.id = ap.user_id
		WHERE ap.post_id = $1 AND ap.user_id = $2
	`, postID, userID).Scan(&did, &storedPriv, &repoHead, &repoRev, &rkey, &oldCidStr)
	if err != nil {
		return
	}

	priv, err := s.getOrCreateSigningKey(userID, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve signing key for user %s: %v", userID, err)
		return
	}

	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)

	path := "app.bsky.feed.post/" + rkey
	if err := repo.DeleteRecord(ctx, path); err != nil {
		log.Printf("atproto: could not delete post record for post %s: %v", postID, err)
		return
	}

	op := &comatproto.SyncSubscribeRepos_RepoOp{Action: "delete", Path: path}
	if oldCid, err := cid.Decode(oldCidStr); err == nil {
		prev := lexutil.LexLink(oldCid)
		op.Prev = &prev
	}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, []*comatproto.SyncSubscribeRepos_RepoOp{op}); err != nil {
		log.Printf("atproto: could not commit post delete %s: %v", postID, err)
		return
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM atproto_posts WHERE post_id = $1`, postID); err != nil {
		log.Printf("atproto: could not remove record mapping for post %s: %v", postID, err)
		return
	}

	log.Printf("atproto: deleted federated post %s at %s", postID, path)
}
