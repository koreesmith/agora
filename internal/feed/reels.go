package feed

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/agora-social/agora/internal/auth"
	"github.com/go-chi/chi/v5"
)

// AGORA-438: the Reels feed. A reel is an ordinary post with a video, so this
// is a filter over posts the server already has, newest first, with no
// ranking. Two tabs:
//
//   - following: videos from people the viewer follows or is friends with, on
//     Agora, the Fediverse and Bluesky. The same people and the same audience
//     rules as the home feed, so a follow whose "show in feed" switch is off
//     stays out here too.
//   - discover: public videos from this instance and its peered Agora
//     instances. This is the same pool as Explore (see PublicFeed and
//     AGORA-376): posts pulled in from the wider Fediverse and Bluesky belong
//     to whoever followed them, not to a showcase of this instance.
//
// Both skip videos the viewer has already watched, which is what keeps a
// chronological feed from showing the same clips every time it is opened.

func registerReelsRoutes(r chi.Router, s *Service) {
	r.Get("/reels", s.GetReels)
	r.Post("/reels/{id}/viewed", s.MarkReelViewed)
}

const (
	reelsDefaultLimit = 10
	reelsMaxLimit     = 50
)

// reelColumns and reelJoins are the same post columns and joins every feed
// query selects, in the order scanPosts reads them.
const reelColumns = `
	SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
	       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
	       cg.name, cg.slug,
	       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
	       p.created_at, p.published_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
	       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
	       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
	       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
	       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
	       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
	       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
	       rp.content, rp.image_url, rp.created_at, rp.published_at,
	       rp.link_url, rp.link_title, rp.link_description, rp.link_image, rp.link_domain,
	       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
	       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
	       p.video_url, p.video_thumb_url,
	       COALESCE(u.emojis::text,'{}'), COALESCE(p.emojis::text,'{}'), COALESCE(rp_u.emojis::text,'{}'), COALESCE(rp.emojis::text,'{}')
	FROM posts p
	JOIN users u ON u.id = p.author_id
	LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
	LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
	LEFT JOIN community_groups cg ON cg.id = p.community_group_id
	LEFT JOIN users wu ON wu.id = p.wall_user_id
	LEFT JOIN pages pg ON pg.id = p.page_id`

// What every reel must satisfy, whichever tab it is on. $1 is the viewer, $3
// and $4 the cursor (null for the first page), $5 whether to include videos
// already watched.
const reelCommon = `
	WHERE p.parent_id IS NULL
	  AND p.deleted_at IS NULL
	  AND p.video_url <> ''
	  AND p.wall_user_id IS NULL
	  -- AGORA-342: a remote friend-list post is stored private and surfaces
	  -- only to the people it was addressed to.
	  AND (
	    p.visibility != 'private'
	    OR EXISTS (SELECT 1 FROM post_audience pa WHERE pa.post_id = p.id AND pa.user_id = $1)
	  )
	  AND NOT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id = $1 AND blocked_id = p.author_id) OR (blocker_id = p.author_id AND blocked_id = $1))
	  AND NOT EXISTS (SELECT 1 FROM hidden_posts WHERE user_id = $1 AND post_id = p.id)
	  ` + keywordMuteClause + `
	  AND ($5 OR NOT EXISTS (SELECT 1 FROM reel_views rv WHERE rv.user_id = $1 AND rv.post_id = p.id))
	  AND ($3::timestamptz IS NULL OR (p.published_at, p.id) < ($3::timestamptz, $4::uuid))`

// reelFollowing is the home feed's "whose posts do I see" test, narrowed to
// videos: yourself, your friends, pages you subscribe to, and Fediverse and
// Bluesky accounts you follow with "show in feed" on.
const reelFollowing = `
	  AND (
	    p.author_id = $1
	    OR EXISTS(
	      SELECT 1 FROM friendships f
	      WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
	          OR (f.addressee_id = $1 AND f.requester_id = p.author_id))
	      AND f.status = 'accepted'
	    )
	    OR (
	      p.page_id IS NOT NULL
	      AND EXISTS(SELECT 1 FROM page_subscribers ps WHERE ps.page_id = p.page_id AND ps.user_id = $1)
	    )
	    OR EXISTS(
	      SELECT 1 FROM ap_following af
	      JOIN users ru ON ru.ap_actor_url = af.followed_actor_url
	      WHERE af.follower_user_id = $1 AND af.accepted = true AND af.show_in_feed = true
	        AND ru.id = p.author_id
	    )
	    OR EXISTS(
	      SELECT 1 FROM at_following af
	      JOIN users ru ON ru.atproto_remote_did = af.remote_did
	      WHERE af.local_user_id = $1 AND af.show_in_feed = true
	        AND ru.id = p.author_id
	    )
	  )
	  AND (
	    p.visibility != 'group'
	    OR p.author_id = $1
	    OR (
	      p.group_id IS NOT NULL
	      AND EXISTS (
	        SELECT 1 FROM friend_group_members fgm
	        JOIN friend_groups fg ON fg.id = fgm.group_id
	        WHERE fgm.group_id = p.group_id
	          AND fgm.friend_id = $1
	          AND fg.user_id = p.author_id
	      )
	    )
	    OR p.page_id IS NOT NULL
	  )
	  AND (
	    p.community_group_id IS NULL
	    OR EXISTS (
	      SELECT 1 FROM community_group_members cgm
	      WHERE cgm.group_id = p.community_group_id AND cgm.user_id = $1
	    )
	  )`

// reelDiscover is Explore's pool (PublicFeed) narrowed to videos, without the
// viewer's own: public posts from this instance and its peered Agora
// instances, never from private profiles or accounts scheduled for deletion.
const reelDiscover = `
	  AND p.visibility = 'public'
	  AND p.author_id <> $1
	  AND NOT u.profile_private
	  AND u.deletion_scheduled_at IS NULL
	  AND p.community_group_id IS NULL
	  AND p.page_id IS NULL
	  AND (p.is_remote = false OR EXISTS (
	    SELECT 1 FROM federated_instances fi
	     WHERE LOWER(fi.domain) = LOWER(p.remote_instance)
	       AND fi.status != 'blocked'
	  ))`

// reelsDiscoverEnabled reports whether the admin has the Discover tab on. It
// is on unless explicitly set to "false", so an instance that upgrades into
// Reels gets it without having to opt in.
func (s *Service) reelsDiscoverEnabled() bool {
	var v string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'reels_discover_enabled'`).Scan(&v)
	return v != "false"
}

// reelCursor is "published_at|id" of the last post of a page, base64-encoded so
// clients treat it as opaque. Keyset rather than offset pagination because
// watched videos drop out of the list between requests, which would shift an
// offset and skip or repeat clips.
func encodeReelCursor(publishedAt, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(publishedAt + "|" + id))
}

func decodeReelCursor(c string) (time.Time, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", false
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return time.Time{}, "", false
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, "", false
	}
	return t, id, true
}

// GetReels serves one page of a Reels tab. Response:
//
//	{ "posts": [...], "next_cursor": "…", "exhausted": false }
//
// next_cursor is empty on the last page, and exhausted is true when there is
// nothing further to watch: the client's cue for "You're all caught up", with
// include_viewed=true to look back over what was already watched.
func (s *Service) GetReels(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	q := r.URL.Query()

	tab := q.Get("tab")
	if tab == "" {
		tab = "following"
	}
	var tabClause string
	switch tab {
	case "following":
		tabClause = reelFollowing
	case "discover":
		if !s.reelsDiscoverEnabled() {
			writeError(w, 404, "discover is turned off on this instance")
			return
		}
		tabClause = reelDiscover
	default:
		writeError(w, 400, "tab must be following or discover")
		return
	}

	limit := reelsDefaultLimit
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > reelsMaxLimit {
		limit = reelsMaxLimit
	}

	var cursorTime, cursorID any // NULL on the first page
	if c := q.Get("cursor"); c != "" {
		t, id, ok := decodeReelCursor(c)
		if !ok {
			writeError(w, 400, "invalid cursor")
			return
		}
		cursorTime, cursorID = t, id
	}
	includeViewed := q.Get("include_viewed") == "true"

	// One more than asked for, to learn whether there is another page.
	rows, err := s.db.Query(reelColumns+reelCommon+tabClause+`
		ORDER BY p.published_at DESC, p.id DESC
		LIMIT $2`, userID, limit+1, cursorTime, cursorID, includeViewed)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	nextCursor := ""
	if len(posts) > limit {
		posts = posts[:limit]
		last := posts[len(posts)-1]
		nextCursor = encodeReelCursor(last.PublishedAt, last.ID)
	}

	s.enrichReactions(posts, userID)
	s.enrichCustomHandles(posts)
	s.enrichNestedQuotes(posts)
	s.enrichPolls(posts, userID)
	s.enrichPhotos(posts)

	writeJSON(w, 200, map[string]any{
		"posts":       posts,
		"next_cursor": nextCursor,
		"exhausted":   nextCursor == "",
	})
}

// MarkReelViewed records that the viewer watched a reel, so it stops coming
// back. The client calls it after a few seconds of playback or on completion.
// Idempotent, and only for posts the viewer could actually see as a reel (a
// post that doesn't exist, isn't a video, or isn't visible to them is a 404),
// so it can't be used to probe which post ids exist.
func (s *Service) MarkReelViewed(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var ok bool
	err := s.db.QueryRow(`
		SELECT EXISTS (
		  SELECT 1 FROM posts p
		  WHERE p.id = $2::uuid AND p.parent_id IS NULL AND p.deleted_at IS NULL AND p.video_url <> ''
		    AND (
		      p.visibility = 'public'
		      OR p.author_id = $1
		      OR EXISTS (SELECT 1 FROM post_audience pa WHERE pa.post_id = p.id AND pa.user_id = $1)
		      OR EXISTS (
		        SELECT 1 FROM friendships f
		        WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
		            OR (f.addressee_id = $1 AND f.requester_id = p.author_id))
		          AND f.status = 'accepted'
		      )
		    )
		    AND NOT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id = $1 AND blocked_id = p.author_id) OR (blocker_id = p.author_id AND blocked_id = $1))
		)`, userID, postID).Scan(&ok)
	if err != nil || !ok {
		writeError(w, 404, "reel not found")
		return
	}

	s.db.Exec(`
		INSERT INTO reel_views (user_id, post_id) VALUES ($1, $2)
		ON CONFLICT (user_id, post_id) DO UPDATE SET viewed_at = NOW()
	`, userID, postID)
	writeJSON(w, 200, map[string]any{"viewed": true})
}
