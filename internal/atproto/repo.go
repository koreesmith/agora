package atproto

import (
	"context"
	"log"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	arepo "github.com/bluesky-social/indigo/repo"
	"github.com/ipfs/go-cid"
)

// getOrCreateRepo opens a user's existing repo from their persisted commit
// head, or creates a fresh empty one — lazily, the same as the signing key
// and DID, the first time any AT Proto write is needed for that user. A
// stored head that fails to open (corrupt row, block missing) falls back to
// a fresh repo rather than hard-failing the caller; the next commit simply
// starts a new history rather than resuming an unreadable one.
//
// Returns the backing pgBlockstore alongside the repo so commitAndPersist can
// read back exactly which blocks this commit's writes touched (AGORA-191),
// via its recording mode.
func (s *Service) getOrCreateRepo(ctx context.Context, userID, did, repoHead string) (*arepo.Repo, *pgBlockstore) {
	bs := &pgBlockstore{db: s.db, userID: userID}
	if repoHead != "" {
		if root, err := cid.Decode(repoHead); err == nil {
			if r, err := arepo.OpenRepo(ctx, bs, root); err == nil {
				return r, bs
			}
			log.Printf("atproto: could not reopen repo for user %s at %s, starting fresh: unreadable head", userID, repoHead)
		}
	}
	return arepo.NewRepo(ctx, did, bs), bs
}

// commitAndPersist signs the repo's pending writes, saves the resulting
// commit CID/rev as the user's new repo head, and emits the commit on the
// firehose (AGORA-191) — the AT Proto repo write path's equivalent of a
// database transaction commit plus a change-notification event.
func (s *Service) commitAndPersist(
	ctx context.Context, userID, did string, repo *arepo.Repo, bs *pgBlockstore,
	priv *atcrypto.PrivateKeyK256, sinceRev string, ops []*comatproto.SyncSubscribeRepos_RepoOp,
) error {
	bs.startRecording()
	signer := func(_ context.Context, _ string, data []byte) ([]byte, error) {
		return priv.HashAndSign(data)
	}
	commitCid, rev, err := repo.Commit(ctx, signer)
	if err != nil {
		bs.stopRecording()
		return err
	}
	recorded := bs.stopRecording()

	if _, err := s.db.ExecContext(ctx, `
		UPDATE users SET atproto_repo_head = $1, atproto_repo_rev = $2 WHERE id = $3
	`, commitCid.String(), rev, userID); err != nil {
		return err
	}

	s.emitCommit(ctx, did, commitCid, rev, sinceRev, recorded, ops)
	return nil
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
// Avatar/banner blobs (AGORA-233) are read from the same local uploads
// buildImageEmbed already knows how to turn into blobs — this just points
// that at users.avatar_url/cover_url instead of a post's images.
func (s *Service) SyncProfile(userID string) {
	if !s.atprotoEnabled() {
		return
	}
	ctx := context.Background()

	var username, displayName, bio, avatarURL, coverURL, did, storedPriv, repoHead, repoRev string
	var isRemote, profilePrivate, atprotoEnabled bool
	err := s.db.QueryRowContext(ctx, `
		SELECT username, display_name, bio, avatar_url, cover_url, atproto_did, atproto_private_key,
		       atproto_repo_head, atproto_repo_rev, is_remote, profile_private, atproto_enabled
		FROM users WHERE id = $1 AND deletion_scheduled_at IS NULL
	`, userID).Scan(&username, &displayName, &bio, &avatarURL, &coverURL, &did, &storedPriv,
		&repoHead, &repoRev, &isRemote, &profilePrivate, &atprotoEnabled)
	if err != nil || isRemote || profilePrivate || !atprotoEnabled {
		return
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve identity for user %s: %v", userID, err)
		return
	}

	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)

	rec := &bsky.ActorProfile{LexiconTypeID: "app.bsky.actor.profile"}
	if displayName != "" {
		rec.DisplayName = &displayName
	}
	if bio != "" {
		rec.Description = &bio
	}
	if avatarURL != "" {
		if blob, err := s.uploadImageBlob(ctx, bs, avatarURL); err != nil {
			log.Printf("atproto: could not upload avatar blob for user %s: %v", userID, err)
		} else {
			rec.Avatar = blob
		}
	}
	if coverURL != "" {
		if blob, err := s.uploadImageBlob(ctx, bs, coverURL); err != nil {
			log.Printf("atproto: could not upload cover blob for user %s: %v", userID, err)
		} else {
			rec.Banner = blob
		}
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
	action := "create"
	writeRecord := repo.PutRecord
	if _, _, err := repo.GetRecordBytes(ctx, profilePath); err == nil {
		action = "update"
		writeRecord = repo.UpdateRecord
	}
	recordCid, err := writeRecord(ctx, profilePath, rec)
	if err != nil {
		log.Printf("atproto: could not write profile record for user %s: %v", userID, err)
		return
	}

	link := lexutil.LexLink(recordCid)
	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: action, Path: profilePath, Cid: &link}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		log.Printf("atproto: could not commit profile sync for user %s: %v", userID, err)
		return
	}

	log.Printf("atproto: synced profile record for user %s", userID)
}
