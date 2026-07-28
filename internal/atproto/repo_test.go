package atproto

import (
	"bytes"
	"context"
	"testing"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"

	"github.com/agora-social/agora/internal/config"
)

// TestCommitAndPersistFirehoseChain locks in the two guarantees that broke
// Bluesky post propagation once profile syncs started sharing (and racing on)
// the repo:
//
//  1. Every #commit's CAR is self-contained — it carries the record block for
//     each create/update op inline. The record is written through to the
//     blockstore before commitAndPersist starts recording, so without the
//     explicit backfill it was absent from the firehose CAR and a subscriber
//     couldn't index the op from the event alone.
//  2. Consecutive commits form an unbroken chain: commit N+1's `since` equals
//     commit N's `rev`, and the stored head/rev always matches what was just
//     emitted. A gap here is exactly what made a relay stop applying a repo's
//     commits.
func TestCommitAndPersistFirehoseChain(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	var prevEnabled string
	db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_enabled'`).Scan(&prevEnabled)
	db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_enabled', 'true') ON CONFLICT (key) DO UPDATE SET value = 'true'`)
	t.Cleanup(func() {
		db.Exec(`UPDATE instance_settings SET value = $1 WHERE key = 'atproto_enabled'`, prevEnabled)
	})

	const username = "agora_commitchain_test_user"
	const did = "did:web:agora_commitchain_test_user"
	db.Exec(`DELETE FROM users WHERE username = $1`, username)
	var userID string
	if err := db.QueryRow(`
		INSERT INTO users (username, email, password_hash, profile_private, atproto_enabled, atproto_did)
		VALUES ($1, $2, '', false, true, $3)
		RETURNING id
	`, username, username+"@example.invalid", did).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE username = $1`, username) })

	// Don't leave this test's events in the shared firehose log.
	var baselineSeq int64
	db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM atproto_firehose_events`).Scan(&baselineSeq)
	t.Cleanup(func() { db.Exec(`DELETE FROM atproto_firehose_events WHERE seq > $1`, baselineSeq) })

	s := NewService(db, &config.Config{InstanceDomain: "http://localhost:8080"}, nil)
	priv, err := atcrypto.GeneratePrivateKeyK256()
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}

	// commit writes one app.bsky.feed.post record and returns the record CID
	// plus the #commit event that was emitted for it.
	commit := func(text string) (cid.Cid, *comatproto.SyncSubscribeRepos_Commit) {
		t.Helper()
		var repoHead, repoRev string
		if err := db.QueryRow(`SELECT atproto_repo_head, atproto_repo_rev FROM users WHERE id = $1`, userID).
			Scan(&repoHead, &repoRev); err != nil {
			t.Fatalf("read head: %v", err)
		}

		unlock := s.lockRepo(userID)
		defer unlock()

		repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)
		recCid, rkey, err := repo.CreateRecord(ctx, "app.bsky.feed.post", &bsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          text,
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			t.Fatalf("create record: %v", err)
		}

		var seqBefore int64
		db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM atproto_firehose_events`).Scan(&seqBefore)

		link := lexutil.LexLink(recCid)
		ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "create", Path: "app.bsky.feed.post/" + rkey, Cid: &link}}
		if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
			t.Fatalf("commitAndPersist: %v", err)
		}

		var data []byte
		if err := db.QueryRow(`SELECT data FROM atproto_firehose_events WHERE seq > $1 ORDER BY seq DESC LIMIT 1`, seqBefore).Scan(&data); err != nil {
			t.Fatalf("commit emitted no firehose event: %v", err)
		}
		var evt events.XRPCStreamEvent
		if err := evt.Deserialize(bytes.NewReader(data)); err != nil {
			t.Fatalf("deserialize firehose event: %v", err)
		}
		if evt.RepoCommit == nil {
			t.Fatalf("emitted event is not a #commit")
		}
		return recCid, evt.RepoCommit
	}

	// carHasBlock reports whether the record block is inside the commit's CAR —
	// the self-containment guarantee.
	carHasBlock := func(commitBlocks []byte, want cid.Cid) bool {
		cr, err := car.NewCarReader(bytes.NewReader(commitBlocks))
		if err != nil {
			t.Fatalf("read commit CAR: %v", err)
		}
		for {
			blk, err := cr.Next()
			if err != nil {
				return false
			}
			if blk.Cid().Equals(want) {
				return true
			}
		}
	}

	rec1, commit1 := commit("first post")
	if !carHasBlock(commit1.Blocks, rec1) {
		t.Errorf("commit 1 CAR is missing the post record block %s — a subscriber can't index it from the firehose alone", rec1)
	}
	if commit1.Since != nil {
		t.Errorf("first-ever commit should have no `since`, got %q", *commit1.Since)
	}

	// The stored head/rev must match what was emitted.
	var head1, rev1 string
	db.QueryRow(`SELECT atproto_repo_head, atproto_repo_rev FROM users WHERE id = $1`, userID).Scan(&head1, &rev1)
	if head1 != commit1.Commit.String() || rev1 != commit1.Rev {
		t.Errorf("stored head/rev (%s/%s) != emitted (%s/%s)", head1, rev1, commit1.Commit.String(), commit1.Rev)
	}

	rec2, commit2 := commit("second post")
	if !carHasBlock(commit2.Blocks, rec2) {
		t.Errorf("commit 2 CAR is missing the post record block %s", rec2)
	}
	// The chain must be continuous: commit 2's `since` is commit 1's `rev`.
	if commit2.Since == nil {
		t.Fatalf("commit 2 has no `since` — chain is broken")
	}
	if *commit2.Since != commit1.Rev {
		t.Errorf("commit 2 since = %q, want commit 1 rev %q — firehose chain has a gap", *commit2.Since, commit1.Rev)
	}
	if commit2.Rev <= commit1.Rev {
		t.Errorf("commit 2 rev %q is not after commit 1 rev %q", commit2.Rev, commit1.Rev)
	}
}
