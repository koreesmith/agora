package atproto

import (
	"context"
	"log"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
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
	ctx := context.Background()

	var username, content, did, storedPriv, repoHead string
	var visibility string
	var profilePrivate, isRemote bool
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT u.username, u.profile_private, u.is_remote,
		       u.atproto_did, u.atproto_private_key, u.atproto_repo_head,
		       p.visibility, p.content, p.created_at
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, postID, userID).Scan(&username, &profilePrivate, &isRemote,
		&did, &storedPriv, &repoHead, &visibility, &content, &createdAt)
	if err != nil || visibility != "public" || profilePrivate || isRemote {
		return
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve identity for user %s: %v", userID, err)
		return
	}

	repo := s.getOrCreateRepo(ctx, userID, did, repoHead)

	rec := &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          content,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
	}
	recordCid, rkey, err := repo.CreateRecord(ctx, "app.bsky.feed.post", rec)
	if err != nil {
		log.Printf("atproto: could not write post record for post %s: %v", postID, err)
		return
	}

	if err := s.commitAndPersist(ctx, userID, repo, priv); err != nil {
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

	log.Printf("atproto: federated post %s as %s/%s", postID, "app.bsky.feed.post", rkey)
}
