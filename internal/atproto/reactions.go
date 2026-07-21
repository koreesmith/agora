package atproto

import (
	"context"
	"log"

	"github.com/bluesky-social/indigo/api/bsky"
)

const reactionPollLimit = 100

// pollInboundReactions checks every Agora post broadcast to Bluesky
// (atproto_posts) for likes/reposts, diffing the current liker/reposter DID
// set against what's already recorded (AGORA-200) — the reaction
// counterpart to pollInboundReplies, chosen for the same AppView-polling
// reason ingest.go's own doc comment gives. Unlike a reply, a Like/Repost
// has no per-record strong ref available via the AppView (getLikes/
// getRepostedBy return only actor+timestamp), so removal can only be
// detected by diffing against the current set, not by an individual
// record's own identity.
func (s *Service) pollInboundReactions(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ap.post_id, u.atproto_did, ap.rkey, ap.record_cid
		FROM atproto_posts ap
		JOIN posts p ON p.id = ap.post_id
		JOIN users u ON u.id = ap.user_id
		WHERE p.deleted_at IS NULL AND p.visibility = 'public'
		  AND u.profile_private = false AND u.atproto_enabled = true
	`)
	if err != nil {
		return
	}
	type target struct{ postID, uri, cid string }
	var targets []target
	for rows.Next() {
		var postID, did, rkey, recordCid string
		if rows.Scan(&postID, &did, &rkey, &recordCid) == nil {
			targets = append(targets, target{postID, "at://" + did + "/app.bsky.feed.post/" + rkey, recordCid})
		}
	}
	rows.Close()

	for _, t := range targets {
		s.pollLikesFor(ctx, t.postID, t.uri, t.cid)
		s.pollRepostsFor(ctx, t.postID, t.uri, t.cid)
	}
}

func actorFields(a *bsky.ActorDefs_ProfileView) (handle, displayName, avatarURL string) {
	handle = a.Handle
	if a.DisplayName != nil {
		displayName = *a.DisplayName
	}
	if a.Avatar != nil {
		avatarURL = *a.Avatar
	}
	return
}

// pollLikesFor diffs app.bsky.feed.getLikes' current liker set for one
// broadcast post against reactions already attributed to a Bluesky stub
// user, inserting new likes (writing straight to reactions, not the legacy
// likes table — AGORA-157's lesson, same as handleInboundLike already
// applies on the AP side) and removing ones no longer present.
func (s *Service) pollLikesFor(ctx context.Context, postID, uri, recordCid string) {
	out, err := bsky.FeedGetLikes(ctx, s.appviewClient(), recordCid, "", reactionPollLimit, uri)
	if err != nil {
		return
	}
	current := map[string]*bsky.ActorDefs_ProfileView{}
	for _, l := range out.Likes {
		if l != nil && l.Actor != nil {
			current[l.Actor.Did] = l.Actor
		}
	}

	existing := map[string]string{} // did -> user_id
	if rows, err := s.db.QueryContext(ctx, `
		SELECT u.atproto_remote_did, u.id
		FROM reactions r JOIN users u ON u.id = r.user_id
		WHERE r.post_id = $1 AND r.reaction_type = 'like' AND u.atproto_remote_did != ''
	`, postID); err == nil {
		for rows.Next() {
			var did, uid string
			if rows.Scan(&did, &uid) == nil {
				existing[did] = uid
			}
		}
		rows.Close()
	}

	var postAuthorID string
	var parentID *string
	s.db.QueryRowContext(ctx, `SELECT author_id, parent_id FROM posts WHERE id = $1`, postID).Scan(&postAuthorID, &parentID)

	for did, actor := range current {
		if _, ok := existing[did]; ok {
			continue
		}
		handle, displayName, avatarURL := actorFields(actor)
		// AGORA-205: same enforcement point ingestAuthorFeed/ingestThreadReplies use.
		if s.isBlueskyActorBlocked(did, handle) {
			continue
		}
		authorID, err := s.getOrCreateRemoteATUser(did, handle, displayName, avatarURL, "")
		if err != nil {
			continue
		}
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO reactions (user_id, post_id, reaction_type) VALUES ($1, $2, 'like')
			ON CONFLICT (user_id, post_id) DO NOTHING
		`, authorID, postID)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // already recorded — expected on re-poll, not an error
		}
		if s.notif != nil && postAuthorID != authorID {
			notifType := "post_like"
			if parentID != nil {
				notifType = "comment_like"
			}
			s.notif.Create(postAuthorID, authorID, notifType, postID, "")
		}
		log.Printf("atproto: ingested like on %s from %s", postID, handle)
	}

	for did, uid := range existing {
		if _, ok := current[did]; ok {
			continue
		}
		s.db.ExecContext(ctx, `DELETE FROM reactions WHERE user_id = $1 AND post_id = $2 AND reaction_type = 'like'`, uid, postID)
	}
}

// pollRepostsFor mirrors pollLikesFor for app.bsky.feed.getRepostedBy,
// representing a Bluesky repost as a posts row with repost_of_id set —
// Agora's existing repost/announce representation (handleInboundAnnounce's
// AP equivalent). Since getRepostedBy carries no per-record ref either, a
// synthetic (postID, DID)-keyed remote_post_id fills the role activityID
// plays for AP announces: unique enough for the existing
// (remote_post_id, remote_instance) index to dedup redelivery safely.
func (s *Service) pollRepostsFor(ctx context.Context, postID, uri, recordCid string) {
	out, err := bsky.FeedGetRepostedBy(ctx, s.appviewClient(), recordCid, "", reactionPollLimit, uri)
	if err != nil {
		return
	}
	current := map[string]*bsky.ActorDefs_ProfileView{}
	for _, a := range out.RepostedBy {
		if a != nil {
			current[a.Did] = a
		}
	}

	existing := map[string]string{} // did -> repost post id
	if rows, err := s.db.QueryContext(ctx, `
		SELECT u.atproto_remote_did, p.id
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.repost_of_id = $1 AND p.is_remote = true AND p.remote_instance = 'bsky.app'
		  AND p.deleted_at IS NULL AND u.atproto_remote_did != ''
	`, postID); err == nil {
		for rows.Next() {
			var did, rid string
			if rows.Scan(&did, &rid) == nil {
				existing[did] = rid
			}
		}
		rows.Close()
	}

	var postAuthorID string
	s.db.QueryRowContext(ctx, `SELECT author_id FROM posts WHERE id = $1`, postID).Scan(&postAuthorID)

	for did, actor := range current {
		if _, ok := existing[did]; ok {
			continue
		}
		handle, displayName, avatarURL := actorFields(actor)
		// AGORA-205: same enforcement point ingestAuthorFeed/ingestThreadReplies use.
		if s.isBlueskyActorBlocked(did, handle) {
			continue
		}
		authorID, err := s.getOrCreateRemoteATUser(did, handle, displayName, avatarURL, "")
		if err != nil {
			continue
		}
		syntheticID := "bsky-repost:" + postID + ":" + did
		var repostID string
		err = s.db.QueryRowContext(ctx, `
			INSERT INTO posts (author_id, visibility, repost_of_id, is_remote, remote_post_id, remote_instance)
			VALUES ($1, 'public', $2, true, $3, 'bsky.app')
			ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO NOTHING
			RETURNING id
		`, authorID, postID, syntheticID).Scan(&repostID)
		if err != nil {
			continue // ErrNoRows on redelivery/already-ingested — expected, not an error
		}
		if s.notif != nil && postAuthorID != authorID {
			s.notif.Create(postAuthorID, authorID, "post_repost", postID, "")
		}
		log.Printf("atproto: ingested repost of %s from %s", postID, handle)
	}

	for did, rid := range existing {
		if _, ok := current[did]; ok {
			continue
		}
		s.db.ExecContext(ctx, `UPDATE posts SET deleted_at = NOW() WHERE id = $1`, rid)
	}
}
