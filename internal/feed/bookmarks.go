package feed

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/auth"
)

// AGORA-429: bookmarks, a private list of posts the viewer saved to come back
// to. Not federated, not visible to anyone else, and the author is never told.
//
// Only a post the viewer can currently see may be saved or listed. The same
// rules GetPost applies decide that, written as one SQL clause so the list can
// filter a page's worth at once: a deleted post, one from an account the
// viewer has blocked (or that blocked them), and one the viewer has since lost
// access to (unfriended, left the group) all drop out of the list without an
// error. The bookmark row stays, so access regained brings the post back.

// bookmarkVisibleClause is "the viewer ($1) may read post p by author u".
// Mirrors GetPost: the author's own posts always; otherwise a private profile
// needs a friendship, friends/group posts need a friendship, a friend-list post
// needs an audience row, and a community group post needs membership.
const bookmarkVisibleClause = `(
	p.author_id = $1
	OR (
		(
			NOT u.profile_private
			OR EXISTS (SELECT 1 FROM friendships f
			           WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
			               OR (f.requester_id = p.author_id AND f.addressee_id = $1))
			             AND f.status = 'accepted')
		)
		AND (
			p.visibility = 'public'
			OR (p.visibility IN ('friends', 'group')
			    AND EXISTS (SELECT 1 FROM friendships f
			                WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
			                    OR (f.requester_id = p.author_id AND f.addressee_id = $1))
			                  AND f.status = 'accepted'))
			OR (p.visibility = 'private'
			    AND EXISTS (SELECT 1 FROM post_audience pa WHERE pa.post_id = p.id AND pa.user_id = $1))
		)
		AND (
			p.community_group_id IS NULL
			OR EXISTS (SELECT 1 FROM community_group_members cgm
			           WHERE cgm.group_id = p.community_group_id AND cgm.user_id = $1)
		)
	)
)`

const bookmarkNotBlockedClause = `NOT EXISTS (
	SELECT 1 FROM blocks b
	WHERE (b.blocker_id = $1 AND b.blocked_id = p.author_id)
	   OR (b.blocker_id = p.author_id AND b.blocked_id = $1)
)`

// AddBookmark saves a post. Idempotent: a double tap is not an error.
func (s *Service) AddBookmark(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}
	postID := chi.URLParam(r, "id")

	// A comment is a post row with a parent; only top-level posts are saved.
	var ok bool
	if err := s.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM posts p JOIN users u ON u.id = p.author_id
			WHERE p.id = $2 AND p.deleted_at IS NULL AND p.parent_id IS NULL
			  AND `+bookmarkVisibleClause+`
			  AND `+bookmarkNotBlockedClause+`
		)
	`, userID, postID).Scan(&ok); err != nil || !ok {
		writeError(w, 404, "post not found")
		return
	}

	if _, err := s.db.Exec(`
		INSERT INTO bookmarks (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, userID, postID); err != nil {
		writeError(w, 500, "could not save post")
		return
	}
	writeJSON(w, 200, map[string]any{"bookmarked": true})
}

// RemoveBookmark unsaves a post. Also idempotent, and it does not check the post
// still exists or is still visible: removing the bookmark should always work.
func (s *Service) RemoveBookmark(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}
	if _, err := s.db.Exec(`DELETE FROM bookmarks WHERE user_id = $1 AND post_id = $2`, userID, chi.URLParam(r, "id")); err != nil {
		writeError(w, 500, "could not remove bookmark")
		return
	}
	writeJSON(w, 200, map[string]any{"bookmarked": false})
}

// ListBookmarks returns the viewer's saved posts, most recently saved first,
// in the same shape as a timeline so clients render them with the usual post
// card. Paged with limit/offset like every other list; filtering happens in
// the query, before paging, so a page is never short because of hidden rows.
func (s *Service) ListBookmarks(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}
	limit, offset := pageParams(r)

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
		       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
		       p.created_at, p.published_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
		       rp.content, rp.image_url, rp.created_at, rp.published_at,
		       rp.link_url, rp.link_title, rp.link_description, rp.link_image, rp.link_domain,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
		       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
		       p.video_url, p.video_thumb_url,
		       COALESCE(u.emojis::text,'{}'), COALESCE(p.emojis::text,'{}'), COALESCE(rp_u.emojis::text,'{}'), COALESCE(rp.emojis::text,'{}')
		FROM bookmarks bm
		JOIN posts p ON p.id = bm.post_id
		JOIN users u ON u.id = p.author_id
		LEFT JOIN posts rp   ON rp.id = p.repost_of_id
		LEFT JOIN users rp_u ON rp_u.id = rp.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
		LEFT JOIN pages pg ON pg.id = p.page_id
		WHERE bm.user_id = $1 AND p.deleted_at IS NULL AND p.parent_id IS NULL
		  AND `+bookmarkVisibleClause+`
		  AND `+bookmarkNotBlockedClause+`
		ORDER BY bm.created_at DESC, p.id DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	if posts == nil {
		posts = []Post{}
	}
	s.enrichReactions(posts, userID)
	s.enrichCustomHandles(posts)
	s.enrichNestedQuotes(posts)
	s.enrichPolls(posts, userID)
	s.enrichPhotos(posts)
	s.enrichExternalOnly(posts)
	writeJSON(w, 200, map[string]any{"posts": posts})
}
