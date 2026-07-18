package atproto

import (
	"context"
	"log"

	"github.com/bluesky-social/indigo/api/bsky"
)

// threadPollDepth caps how many nested reply levels app.bsky.feed.getPostThread
// returns per call — comfortably deeper than Agora's own 2-level comment cap
// (root -> comment -> reply) so ingestThreadReplies always sees everything
// it's able to attach locally in one round trip.
const threadPollDepth = 6

// maxCommentDepth mirrors federation's resolveReplyTarget 2-level cap (root
// -> comment -> reply) — a Bluesky sub-thread nested deeper than that has
// nowhere to attach in Agora's UI.
const maxCommentDepth = 2

// pollInboundReplies checks every top-level Agora post that was broadcast to
// Bluesky (an atproto_posts row) for new Bluesky replies, ingesting any not
// already seen as comments (AGORA-199) — the reply-thread counterpart to
// ingestAuthorFeed's top-level-post ingestion. Polls app.bsky.feed.getPostThread
// per broadcast post rather than consuming the network firehose, for the same
// reason ingest.go's own doc comment gives for author-feed polling: this
// scales with how many posts Agora itself federated, not with the size of
// the whole Bluesky network.
func (s *Service) pollInboundReplies(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ap.post_id, u.atproto_did, ap.rkey
		FROM atproto_posts ap
		JOIN posts p ON p.id = ap.post_id
		JOIN users u ON u.id = ap.user_id
		WHERE p.parent_id IS NULL AND p.deleted_at IS NULL
	`)
	if err != nil {
		return
	}
	type rootPost struct{ postID, uri string }
	var roots []rootPost
	for rows.Next() {
		var postID, did, rkey string
		if rows.Scan(&postID, &did, &rkey) == nil {
			roots = append(roots, rootPost{postID, "at://" + did + "/app.bsky.feed.post/" + rkey})
		}
	}
	rows.Close()
	if len(roots) == 0 {
		return
	}

	known := s.loadKnownBlueskyURIs(ctx)

	for _, rp := range roots {
		out, err := bsky.FeedGetPostThread(ctx, s.appviewClient(), threadPollDepth, 0, rp.uri)
		if err != nil || out.Thread == nil || out.Thread.FeedDefs_ThreadViewPost == nil {
			continue
		}
		known[rp.uri] = rp.postID
		s.ingestThreadReplies(ctx, out.Thread.FeedDefs_ThreadViewPost, rp.postID, 0, known)
	}
}

// loadKnownBlueskyURIs builds a URI -> local post/comment id map covering
// both Agora content broadcast to Bluesky (atproto_posts) and content
// already ingested from Bluesky (posts.remote_post_id) — so
// ingestThreadReplies can recognize a thread node it already knows about
// (including Agora's own replies reflected back by the AppView) and keep
// walking down using its real local id, instead of re-ingesting it as a new
// remote stub comment.
func (s *Service) loadKnownBlueskyURIs(ctx context.Context) map[string]string {
	known := map[string]string{}
	if rows, err := s.db.QueryContext(ctx, `
		SELECT id, remote_post_id FROM posts
		WHERE is_remote = true AND remote_instance = 'bsky.app' AND remote_post_id != '' AND deleted_at IS NULL
	`); err == nil {
		for rows.Next() {
			var id, uri string
			if rows.Scan(&id, &uri) == nil {
				known[uri] = id
			}
		}
		rows.Close()
	}
	if rows, err := s.db.QueryContext(ctx, `
		SELECT ap.post_id, u.atproto_did, ap.rkey
		FROM atproto_posts ap JOIN users u ON u.id = ap.user_id
	`); err == nil {
		for rows.Next() {
			var id, did, rkey string
			if rows.Scan(&id, &did, &rkey) == nil {
				known["at://"+did+"/app.bsky.feed.post/"+rkey] = id
			}
		}
		rows.Close()
	}
	return known
}

// ingestThreadReplies walks a thread view's nested replies, ingesting any
// not already seen as a local comment, attaching each to the local id of its
// immediate parent as it descends.
func (s *Service) ingestThreadReplies(ctx context.Context, node *bsky.FeedDefs_ThreadViewPost, localParentID string, depth int, known map[string]string) {
	if node == nil || depth >= maxCommentDepth {
		return
	}
	for _, elem := range node.Replies {
		if elem == nil || elem.FeedDefs_ThreadViewPost == nil {
			continue
		}
		reply := elem.FeedDefs_ThreadViewPost
		post := reply.Post
		if post == nil {
			continue
		}

		if localID, ok := known[post.Uri]; ok {
			s.ingestThreadReplies(ctx, reply, localID, depth+1, known)
			continue
		}

		rec, ok := post.Record.Val.(*bsky.FeedPost)
		if !ok || rec == nil {
			continue
		}
		var handle, displayName, avatarURL, did string
		if post.Author != nil {
			did = post.Author.Did
			handle = post.Author.Handle
			if post.Author.DisplayName != nil {
				displayName = *post.Author.DisplayName
			}
			if post.Author.Avatar != nil {
				avatarURL = *post.Author.Avatar
			}
		}
		if did == "" {
			continue
		}
		authorID, err := s.getOrCreateRemoteATUser(did, handle, displayName, avatarURL)
		if err != nil {
			continue
		}

		var parentAuthorID string
		s.db.QueryRowContext(ctx, `SELECT author_id FROM posts WHERE id = $1`, localParentID).Scan(&parentAuthorID)

		var commentID string
		err = s.db.QueryRowContext(ctx, `
			INSERT INTO posts (author_id, content, visibility, parent_id, is_remote, remote_post_id, remote_instance, remote_post_cid)
			VALUES ($1, $2, 'public', $3, true, $4, 'bsky.app', $5)
			ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO NOTHING
			RETURNING id
		`, authorID, rec.Text, localParentID, post.Uri, post.Cid).Scan(&commentID)
		if err != nil {
			continue // ErrNoRows on redelivery/already-ingested — expected, not an error
		}
		known[post.Uri] = commentID

		var imageURLs []string
		if post.Embed != nil && post.Embed.EmbedImages_View != nil {
			for _, img := range post.Embed.EmbedImages_View.Images {
				if img.Fullsize != "" {
					imageURLs = append(imageURLs, img.Fullsize)
				}
			}
		}
		s.storeInboundImages(commentID, imageURLs)

		if s.notif != nil && parentAuthorID != "" && parentAuthorID != authorID {
			s.notif.Create(parentAuthorID, authorID, "post_comment", localParentID, "")
		}

		log.Printf("atproto: ingested reply %s from %s (%s)", commentID, handle, post.Uri)

		s.ingestThreadReplies(ctx, reply, commentID, depth+1, known)
	}
}
