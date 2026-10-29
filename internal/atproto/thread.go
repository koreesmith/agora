package atproto

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/lib/pq"
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

// ingestedThreadPollWindow and ingestedThreadPollLimit bound which posts
// ingested from Bluesky pollInboundReplies checks for replies (AGORA-384):
// conversations are mostly over within a few days, and the newest few hundred
// cover them without polling every post ever ingested every five minutes.
const (
	ingestedThreadPollWindow = 72 * time.Hour
	ingestedThreadPollLimit  = 200
)

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

	// AGORA-384: and recent posts ingested from Bluesky. Without these, a
	// thread started by someone this instance follows only ever showed
	// replies from accounts it also follows, never anyone else's, including
	// another Agora instance's. Bounded by age and count, since unlike
	// Agora's own posts this set grows with every account followed;
	// SyncBlueskyThread covers an older thread when someone opens it.
	if rows, err := s.db.QueryContext(ctx, `
		SELECT id, remote_post_id FROM posts
		WHERE is_remote = true AND remote_instance = 'bsky.app' AND parent_id IS NULL
		  AND deleted_at IS NULL AND remote_post_id LIKE 'at://%'
		  AND published_at > NOW() - make_interval(secs => $1)
		ORDER BY published_at DESC
		LIMIT $2
	`, ingestedThreadPollWindow.Seconds(), ingestedThreadPollLimit); err == nil {
		for rows.Next() {
			var postID, uri string
			if rows.Scan(&postID, &uri) == nil {
				roots = append(roots, rootPost{postID, uri})
			}
		}
		rows.Close()
	}
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
		// AGORA-205: same enforcement point ingestAuthorFeed uses.
		if s.isBlueskyActorBlocked(did, handle) {
			continue
		}
		authorID, err := s.getOrCreateRemoteATUser(did, handle, displayName, avatarURL, "")
		if err != nil {
			continue
		}

		var parentAuthorID string
		s.db.QueryRowContext(ctx, `SELECT author_id FROM posts WHERE id = $1`, localParentID).Scan(&parentAuthorID)

		var commentID string
		err = s.db.QueryRowContext(ctx, `
			INSERT INTO posts (author_id, content, visibility, parent_id, is_remote, remote_post_id, remote_instance, remote_post_cid, content_warning, published_at)
			VALUES ($1, $2, 'public', $3, true, $4, 'bsky.app', $5, $6, $7)
			ON CONFLICT (remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != '' DO NOTHING
			RETURNING id
		`, authorID, textWithLinks(rec), localParentID, post.Uri, post.Cid, contentWarningFromLabels(rec.Labels), parseBlueskyTime(rec.CreatedAt)).Scan(&commentID)
		if err != nil {
			continue // ErrNoRows on redelivery/already-ingested — expected, not an error
		}
		known[post.Uri] = commentID

		s.storeInboundEmbed(ctx, commentID, post.Embed)
		s.storeHashtagsFromFacets(commentID, rec.Facets) // AGORA-213

		if s.notif != nil && parentAuthorID != "" && parentAuthorID != authorID {
			s.notif.Create(parentAuthorID, authorID, "post_comment", localParentID, "")
		}

		log.Printf("atproto: ingested reply %s from %s (%s)", commentID, handle, post.Uri)

		s.ingestThreadReplies(ctx, reply, commentID, depth+1, known)
	}
}

// threadSyncCooldown is how soon SyncBlueskyThread will fetch the same thread
// again, so a busy thread being reloaded doesn't hit the AppView each time.
const threadSyncCooldown = time.Minute

// threadSyncTimeout bounds the fetch SyncBlueskyThread makes while someone
// waits for comments to load; past it they get what's already here.
const threadSyncTimeout = 4 * time.Second

var (
	threadSyncMu     sync.Mutex
	threadSyncedAt   = map[string]time.Time{}
	threadSyncPruned time.Time
)

// SyncBlueskyThread (AGORA-384) pulls a Bluesky thread's replies in before its
// comments are shown, for a top-level post that came from Bluesky or that
// Agora published there. pollInboundReplies only reaches recent threads; this
// is what loads an older one when somebody actually opens it. Anything else
// returns at once, and so does a thread synced within threadSyncCooldown.
func (s *Service) SyncBlueskyThread(postID string) {
	if !claimThreadSync(postID, time.Now()) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), threadSyncTimeout)
	defer cancel()

	var uri string
	s.db.QueryRowContext(ctx, `
		SELECT p.remote_post_id FROM posts p
		WHERE p.id = $1 AND p.parent_id IS NULL AND p.deleted_at IS NULL
		  AND p.is_remote = true AND p.remote_instance = 'bsky.app' AND p.remote_post_id LIKE 'at://%'
		UNION ALL
		SELECT 'at://' || u.atproto_did || '/app.bsky.feed.post/' || ap.rkey
		  FROM atproto_posts ap JOIN posts p ON p.id = ap.post_id JOIN users u ON u.id = ap.user_id
		WHERE ap.post_id = $1 AND p.parent_id IS NULL AND p.deleted_at IS NULL
		LIMIT 1
	`, postID).Scan(&uri)
	if uri == "" {
		return
	}

	out, err := bsky.FeedGetPostThread(ctx, s.appviewClient(), threadPollDepth, 0, uri)
	if err != nil || out.Thread == nil || out.Thread.FeedDefs_ThreadViewPost == nil {
		return
	}
	known := s.knownBlueskyURIsIn(ctx, out.Thread.FeedDefs_ThreadViewPost)
	known[uri] = postID
	s.ingestThreadReplies(ctx, out.Thread.FeedDefs_ThreadViewPost, postID, 0, known)
}

// claimThreadSync reports whether postID may be synced now, recording it if
// so. Entries older than the cooldown are dropped now and then so the map
// only ever holds threads opened in the last minute or so.
func claimThreadSync(postID string, now time.Time) bool {
	threadSyncMu.Lock()
	defer threadSyncMu.Unlock()
	if last, ok := threadSyncedAt[postID]; ok && now.Sub(last) < threadSyncCooldown {
		return false
	}
	if now.Sub(threadSyncPruned) > threadSyncCooldown {
		for id, t := range threadSyncedAt {
			if now.Sub(t) >= threadSyncCooldown {
				delete(threadSyncedAt, id)
			}
		}
		threadSyncPruned = now
	}
	threadSyncedAt[postID] = now
	return true
}

// knownBlueskyURIsIn is loadKnownBlueskyURIs narrowed to the posts in one
// thread, for SyncBlueskyThread: it runs while someone waits, and loading
// every Bluesky URI this instance has ever seen would be most of the wait.
func (s *Service) knownBlueskyURIsIn(ctx context.Context, root *bsky.FeedDefs_ThreadViewPost) map[string]string {
	var uris []string
	var walk func(node *bsky.FeedDefs_ThreadViewPost, depth int)
	walk = func(node *bsky.FeedDefs_ThreadViewPost, depth int) {
		if node == nil || depth > maxCommentDepth {
			return
		}
		for _, elem := range node.Replies {
			if elem == nil || elem.FeedDefs_ThreadViewPost == nil || elem.FeedDefs_ThreadViewPost.Post == nil {
				continue
			}
			uris = append(uris, elem.FeedDefs_ThreadViewPost.Post.Uri)
			walk(elem.FeedDefs_ThreadViewPost, depth+1)
		}
	}
	walk(root, 0)

	known := map[string]string{}
	if len(uris) == 0 {
		return known
	}
	if rows, err := s.db.QueryContext(ctx, `
		SELECT id, remote_post_id FROM posts
		WHERE is_remote = true AND remote_instance = 'bsky.app' AND remote_post_id = ANY($1) AND deleted_at IS NULL
	`, pq.Array(uris)); err == nil {
		for rows.Next() {
			var id, uri string
			if rows.Scan(&id, &uri) == nil {
				known[uri] = id
			}
		}
		rows.Close()
	}
	// Agora's own replies, which the AppView reflects back as thread nodes
	// and which must be recognized rather than ingested a second time.
	if rows, err := s.db.QueryContext(ctx, `
		SELECT ap.post_id, 'at://' || u.atproto_did || '/app.bsky.feed.post/' || ap.rkey
		  FROM atproto_posts ap JOIN users u ON u.id = ap.user_id
		WHERE 'at://' || u.atproto_did || '/app.bsky.feed.post/' || ap.rkey = ANY($1)
	`, pq.Array(uris)); err == nil {
		for rows.Next() {
			var id, uri string
			if rows.Scan(&id, &uri) == nil {
				known[uri] = id
			}
		}
		rows.Close()
	}
	return known
}
