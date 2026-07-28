package atproto

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/agora-social/agora/internal/config"
)

// TestIngestFollowedRepost is a regression test for AGORA-266:
// ingestAuthorFeed used to skip every feed item with a non-nil Reason,
// meaning a followed DID's repost of someone else's post never reached the
// local feed at all. ingestFollowedRepost is the fallback that handles the
// reasonRepost case: it should ingest the reposted post (attributed to its
// original author) and attach a repost row attributed to the reposting DID.
func TestIngestFollowedRepost(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db, cfg: &config.Config{InstanceDomain: "http://localhost:8080"}}
	unique := time.Now().UnixNano()

	originalDID := fmt.Sprintf("did:plc:agora266original%d", unique)
	reposterDID := fmt.Sprintf("did:plc:agora266reposter%d", unique)
	postURI := fmt.Sprintf("at://%s/app.bsky.feed.post/%d", originalDID, unique)
	repostURI := fmt.Sprintf("at://%s/app.bsky.feed.repost/%d", reposterDID, unique)

	t.Cleanup(func() {
		db.Exec(`DELETE FROM posts WHERE remote_post_id IN ($1, $2)`, postURI, repostURI)
		db.Exec(`DELETE FROM users WHERE atproto_remote_did IN ($1, $2)`, originalDID, reposterDID)
	})

	originalHandle := fmt.Sprintf("original%d.bsky.social", unique)
	reposterHandle := fmt.Sprintf("reposter%d.bsky.social", unique)

	post := &bsky.FeedDefs_PostView{
		Uri: postURI,
		Cid: "bafyoriginal",
		Author: &bsky.ActorDefs_ProfileViewBasic{
			Did:    originalDID,
			Handle: originalHandle,
		},
		Record: &lexutil.LexiconTypeDecoder{Val: &bsky.FeedPost{Text: "an original post"}},
	}
	reason := &bsky.FeedDefs_ReasonRepost{
		By: &bsky.ActorDefs_ProfileViewBasic{
			Did:    reposterDID,
			Handle: reposterHandle,
		},
		Uri: &repostURI,
	}

	s.ingestFollowedRepost(context.Background(), reposterDID, reason, post)

	var originalPostID, repostAuthorID string
	var repostOfID *string
	if err := db.QueryRow(`SELECT id, author_id, repost_of_id FROM posts WHERE remote_post_id = $1`, repostURI).
		Scan(&originalPostID, &repostAuthorID, &repostOfID); err != nil {
		t.Fatalf("expected repost row to be inserted: %v", err)
	}
	if repostOfID == nil {
		t.Fatalf("expected repost_of_id to be set")
	}

	var quotedPostID, quotedAuthorDID string
	if err := db.QueryRow(`
		SELECT p.id, u.atproto_remote_did FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1
	`, *repostOfID).Scan(&quotedPostID, &quotedAuthorDID); err != nil {
		t.Fatalf("expected the reposted post itself to be ingested: %v", err)
	}
	if quotedAuthorDID != originalDID {
		t.Fatalf("expected reposted post's author to be the original poster %s, got %s", originalDID, quotedAuthorDID)
	}

	var reposterDBDID string
	if err := db.QueryRow(`SELECT atproto_remote_did FROM users WHERE id = $1`, repostAuthorID).Scan(&reposterDBDID); err != nil {
		t.Fatalf("expected repost author to resolve to a user: %v", err)
	}
	if reposterDBDID != reposterDID {
		t.Fatalf("expected repost to be attributed to reposter %s, got %s", reposterDID, reposterDBDID)
	}

	// Redelivery (e.g. the next poll cycle re-fetching the same feed) must
	// not duplicate either row.
	s.ingestFollowedRepost(context.Background(), reposterDID, reason, post)
	var repostCount int
	db.QueryRow(`SELECT count(*) FROM posts WHERE remote_post_id = $1`, repostURI).Scan(&repostCount)
	if repostCount != 1 {
		t.Fatalf("expected exactly 1 repost row after redelivery, got %d", repostCount)
	}
}
