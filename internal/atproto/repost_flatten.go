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

// AGORA-358: a repost of content with no Bluesky presence (a local Agora post,
// or one that arrived over ActivityPub) cannot be an app.bsky.feed.repost,
// which needs a strong ref to a record that already exists on the network.
// Rather than send nothing, the repost goes out as a new app.bsky.feed.post
// from the reposter: the original author's words as plain text plus a link
// back to the post on Agora, the same graceful degradation flattenPollForBluesky
// uses. It reads on Bluesky as a post that quotes the original, not as a
// boost, because Bluesky has no record type for that without a source record.

// flattenRepostForBluesky builds the text of that post: the reposter's own
// commentary (if any), then the original's author and content, then the link.
// The link is always kept, even when the content has to be cut to fit.
func flattenRepostForBluesky(commentary, authorName, content, permalinkURL string) string {
	var b strings.Builder
	if c := strings.TrimSpace(commentary); c != "" {
		b.WriteString(c)
		b.WriteString("\n\n")
	}
	b.WriteString("🔁 ")
	b.WriteString(authorName)
	if c := strings.TrimSpace(content); c != "" {
		b.WriteString(": ")
		b.WriteString(c)
	}
	b.WriteString("\n\n")
	b.WriteString(permalinkURL)
	return truncateForBluesky(b.String(), permalinkURL)
}

// broadcastFlattenedRepost writes the flattened post for repostID, a share of
// originalPostID that has no Bluesky counterpart. Both ends are re-checked
// here, like BroadcastPost does, because this publishes someone else's words
// from the reposter's account: the repost must itself be public, and so must
// the original, since a friends-only or friend-list post has no business
// leaving the audience its author chose. The row goes in atproto_posts under
// the repost's id, which is what lets BroadcastPostDelete take it back down.
func (s *Service) broadcastFlattenedRepost(userID, repostID, originalPostID string) {
	if !s.atprotoEnabled() {
		return
	}
	ctx := context.Background()
	defer s.lockRepo(userID)()

	var username, commentary, contentWarning, did, storedPriv, repoHead, repoRev string
	var visibility string
	var profilePrivate, isRemote, atprotoEnabled, federateATProto bool
	var createdAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT u.username, u.profile_private, u.is_remote, u.atproto_enabled,
		       u.atproto_did, u.atproto_private_key, u.atproto_repo_head, u.atproto_repo_rev,
		       p.visibility, p.content, p.content_warning, p.created_at, p.federate_atproto
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL AND p.repost_of_id = $3
	`, repostID, userID, originalPostID).Scan(&username, &profilePrivate, &isRemote, &atprotoEnabled,
		&did, &storedPriv, &repoHead, &repoRev, &visibility, &commentary, &contentWarning, &createdAt, &federateATProto)
	if err != nil || visibility != "public" || profilePrivate || isRemote || !atprotoEnabled || !federateATProto {
		return
	}

	var already int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM atproto_posts WHERE post_id = $1`, repostID).Scan(&already)
	if already > 0 {
		return
	}

	var authorName, content, origWarning string
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF(u.display_name, ''), u.username), p.content, p.content_warning
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.deleted_at IS NULL AND p.visibility = 'public'
		  AND NOT u.profile_private AND p.community_group_id IS NULL
	`, originalPostID).Scan(&authorName, &content, &origWarning)
	if err != nil {
		return
	}
	if contentWarning == "" {
		contentWarning = origWarning
	}

	did, priv, err := s.ensureIdentity(userID, username, did, storedPriv)
	if err != nil {
		log.Printf("atproto: could not resolve identity for user %s: %v", userID, err)
		return
	}

	repo, bs := s.getOrCreateRepo(ctx, userID, did, repoHead)

	permalink := strings.TrimRight(s.cfg.InstanceDomain, "/") + "/post/" + originalPostID
	// AGORA-459: the warning (the repost's own, else the original's) is the
	// first line of the text, since Bluesky shows no label for it.
	finalText := flattenRepostForBluesky(withWarning(contentWarning, commentary), authorName, content, permalink)
	rec := &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          finalText,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
		Labels:        labelsForContentWarning(contentWarning),
		Facets:        linkFacetsForURLs(finalText),
	}
	recordCid, rkey, err := repo.CreateRecord(ctx, "app.bsky.feed.post", rec)
	if err != nil {
		log.Printf("atproto: could not write flattened repost record for post %s: %v", repostID, err)
		return
	}

	path := "app.bsky.feed.post/" + rkey
	link := lexutil.LexLink(recordCid)
	ops := []*comatproto.SyncSubscribeRepos_RepoOp{{Action: "create", Path: path, Cid: &link}}
	if err := s.commitAndPersist(ctx, userID, did, repo, bs, priv, repoRev, ops); err != nil {
		log.Printf("atproto: could not commit flattened repost %s: %v", repostID, err)
		return
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO atproto_posts (post_id, user_id, rkey, record_cid) VALUES ($1, $2, $3, $4)
		ON CONFLICT (post_id) DO UPDATE SET rkey = $3, record_cid = $4
	`, repostID, userID, rkey, recordCid.String()); err != nil {
		log.Printf("atproto: could not persist record mapping for repost %s: %v", repostID, err)
		return
	}

	log.Printf("atproto: federated repost %s of non-Bluesky post %s as %s", repostID, originalPostID, path)
}
