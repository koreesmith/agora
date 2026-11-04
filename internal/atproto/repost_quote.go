package atproto

import (
	"context"
	"log"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// AGORA-459: reposting a post that is on Bluesky normally writes an
// app.bsky.feed.repost, a record that points at the original and has no text
// and no labels. That is right for a bare repost, but a repost with a trigger
// warning or the sharer's own words would reach Bluesky as nothing more than
// "Reposted by you": the warning and the comment lost. Those go out as a quote
// post instead (an app.bsky.feed.post whose embed is the original), whose text
// opens with the warning line and then the sharer's comment.

// quoteRepostText is the text of that post.
func quoteRepostText(contentWarning, commentary string) string {
	return truncateForBluesky(withWarning(contentWarning, strings.TrimSpace(commentary)), "")
}

// broadcastQuoteRepost writes the quote post for repostID, a share of the
// Bluesky post at targetURI/targetCid. It returns true if it handled the
// share (including when it was already sent), and false when the share has no
// warning and no comment, or may not be sent as words: then the caller sends the
// plain repost record, as before. The repost must itself be public, like
// BroadcastPost requires, because this publishes the sharer's own words.
func (s *Service) broadcastQuoteRepost(userID, repostID, targetURI, targetCid string) bool {
	if !s.atprotoEnabled() {
		return false
	}
	ctx := context.Background()

	var username, commentary, contentWarning, did, storedPriv, repoHead, repoRev, visibility string
	var profilePrivate, isRemote, atprotoEnabled, federateATProto bool
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT u.username, u.profile_private, u.is_remote, u.atproto_enabled,
		       u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev,
		       p.visibility, p.content, p.content_warning, p.created_at, p.federate_atproto
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL AND p.repost_of_id IS NOT NULL
	`, repostID, userID).Scan(&username, &profilePrivate, &isRemote, &atprotoEnabled,
		&did, &storedPriv, &repoHead, &repoRev, &visibility, &commentary, &contentWarning, &createdAt, &federateATProto)
	if err != nil || strings.TrimSpace(commentary) == "" && strings.TrimSpace(contentWarning) == "" {
		return false
	}
	if visibility != "public" || profilePrivate || isRemote || !atprotoEnabled || !federateATProto {
		return false
	}

	defer s.lockRepo(userID)()

	var already int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM atproto_posts WHERE post_id = $1`, repostID).Scan(&already)
	if already > 0 {
		return true
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve identity for user %s: %v", userID, err)
		return false
	}
	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)

	text := quoteRepostText(contentWarning, commentary)
	rec := &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          text,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
		Embed: &bsky.FeedPost_Embed{EmbedRecord: &bsky.EmbedRecord{
			LexiconTypeID: "app.bsky.embed.record",
			Record:        &comatproto.RepoStrongRef{Uri: targetURI, Cid: targetCid},
		}},
		Labels: labelsForContentWarning(contentWarning),
		Facets: linkFacetsForURLs(text),
	}
	recordCid, rkey, err := repo.CreateRecord(ctx, "app.bsky.feed.post", rec)
	if err != nil {
		log.Printf("atproto: could not write quote post for repost %s: %v", repostID, err)
		return false
	}

	path := "app.bsky.feed.post/" + rkey
	link := lexutil.LexLink(recordCid)
	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "create", Path: path, Cid: &link}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		log.Printf("atproto: could not commit quote post for repost %s: %v", repostID, err)
		return false
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO atproto_posts (post_id, user_id, rkey, record_cid) VALUES ($1, $2, $3, $4)
		ON CONFLICT (post_id) DO UPDATE SET rkey = $3, record_cid = $4
	`, repostID, userID, rkey, recordCid.String()); err != nil {
		log.Printf("atproto: could not persist record mapping for repost %s: %v", repostID, err)
		return true // the post is out; losing the mapping only costs taking it down later
	}
	log.Printf("atproto: federated repost %s of Bluesky post %s as a quote post %s", repostID, targetURI, path)
	return true
}
