package atproto

import (
	"context"
	"log"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	arepo "github.com/bluesky-social/indigo/repo"
	"github.com/ipfs/go-cid"
)

// getOrCreateRepo opens a user's existing repo from their persisted commit
// head, or creates a fresh empty one — lazily, the same as the signing key
// and DID, the first time any AT Proto write is needed for that user. A
// stored head that fails to open (corrupt row, block missing) falls back to
// a fresh repo rather than hard-failing the caller; the next commit simply
// starts a new history rather than resuming an unreadable one.
func (s *Service) getOrCreateRepo(ctx context.Context, userID, did, repoHead string) *arepo.Repo {
	bs := &pgBlockstore{db: s.db, userID: userID}
	if repoHead != "" {
		if root, err := cid.Decode(repoHead); err == nil {
			if r, err := arepo.OpenRepo(ctx, bs, root); err == nil {
				return r
			}
			log.Printf("atproto: could not reopen repo for user %s at %s, starting fresh: unreadable head", userID, repoHead)
		}
	}
	return arepo.NewRepo(ctx, did, bs)
}

// commitAndPersist signs the repo's pending writes and saves the resulting
// commit CID as the user's new repo head — the AT Proto repo write path's
// equivalent of a database transaction commit.
func (s *Service) commitAndPersist(ctx context.Context, userID string, repo *arepo.Repo, priv *atcrypto.PrivateKeyK256) error {
	signer := func(_ context.Context, _ string, data []byte) ([]byte, error) {
		return priv.HashAndSign(data)
	}
	commitCid, _, err := repo.Commit(ctx, signer)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET atproto_repo_head = $1 WHERE id = $2`, commitCid.String(), userID)
	return err
}

// SyncProfile writes (or rewrites) a user's app.bsky.actor.profile record
// from their current Agora profile and commits the repo — called on every
// profile edit (internal/users.UpdateProfile) the same way an AP actor
// document already reflects current profile state on every fetch, except a
// repo write is an explicit, discrete operation rather than something
// implicit in a GET. Also called lazily the first time a user's repo is
// created (AGORA-189), so a freshly-created repo isn't empty of even its own
// profile record.
//
// Image blobs (avatar) are deferred to AGORA-194 — this only syncs text
// fields for now.
func (s *Service) SyncProfile(userID string) {
	ctx := context.Background()

	var username, displayName, bio, did, storedPriv, repoHead string
	var isRemote, profilePrivate bool
	err := s.db.QueryRowContext(ctx, `
		SELECT username, display_name, bio, atproto_did, atproto_private_key, atproto_repo_head,
		       is_remote, profile_private
		FROM users WHERE id = $1 AND deletion_scheduled_at IS NULL
	`, userID).Scan(&username, &displayName, &bio, &did, &storedPriv, &repoHead, &isRemote, &profilePrivate)
	if err != nil || isRemote || profilePrivate {
		return
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve identity for user %s: %v", userID, err)
		return
	}

	repo := s.getOrCreateRepo(ctx, userID, did, repoHead)

	rec := &bsky.ActorProfile{LexiconTypeID: "app.bsky.actor.profile"}
	if displayName != "" {
		rec.DisplayName = &displayName
	}
	if bio != "" {
		rec.Description = &bio
	}
	// AT Proto profile records use a fixed singleton rkey ("self"), unlike
	// TID-keyed collections (posts, likes, ...) — but neither PutRecord nor
	// CreateRecord is a true upsert: both call the MST's insert-only Add,
	// which errors if the key is already set ("value already set at key").
	// UpdateRecord's mst.Update is the mirror image — it errors if the key is
	// *not* already set. So an existing profile record needs Update, and a
	// first-ever sync needs Put; check which case this is rather than
	// guessing from repoHead (a fresh repo could still already have a
	// profile record if getOrCreateRepo fell back after a corrupt head).
	const profilePath = "app.bsky.actor.profile/self"
	writeRecord := repo.PutRecord
	if _, _, err := repo.GetRecordBytes(ctx, profilePath); err == nil {
		writeRecord = repo.UpdateRecord
	}
	if _, err := writeRecord(ctx, profilePath, rec); err != nil {
		log.Printf("atproto: could not write profile record for user %s: %v", userID, err)
		return
	}

	if err := s.commitAndPersist(ctx, userID, repo, priv); err != nil {
		log.Printf("atproto: could not commit profile sync for user %s: %v", userID, err)
		return
	}

	log.Printf("atproto: synced profile record for user %s", userID)
}
