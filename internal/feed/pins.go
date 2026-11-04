package feed

import (
	"log"
	"net/http"

	"github.com/agora-social/agora/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

// AGORA-447: pinned posts. A person pins a few of their own posts to the top of
// their profile. Pinning is quiet (no notification, nothing federated), and a
// pin never changes who can see the post: the profile shows each viewer only the
// pins they could already see.

// MaxPinnedPosts is how many posts one account can have pinned at once.
const MaxPinnedPosts = 3

func registerPinRoutes(r chi.Router, s *Service) {
	r.Post("/posts/{id}/pin", s.PinPost)
	r.Delete("/posts/{id}/pin", s.UnpinPost)
}

// PinPost pins one of the viewer's own posts. Idempotent. 403 for someone else's
// post, 400 for a kind of post that can't be pinned, 409 at the limit.
func (s *Service) PinPost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var authorID string
	var parentID, repostOf, wallUser, communityGroup, pageID *string
	err := s.db.QueryRow(`
		SELECT author_id, parent_id, repost_of_id, wall_user_id, community_group_id, page_id
		  FROM posts WHERE id = $1::uuid AND deleted_at IS NULL`, postID,
	).Scan(&authorID, &parentID, &repostOf, &wallUser, &communityGroup, &pageID)
	if err != nil {
		writeError(w, 404, "post not found")
		return
	}
	if authorID != userID {
		writeError(w, 403, "you can only pin your own posts")
		return
	}
	// Top-level posts on your own profile: not a comment, a repost, a post written
	// on someone's wall, or a post made in a group or as a page.
	if parentID != nil || repostOf != nil || wallUser != nil || communityGroup != nil || pageID != nil {
		writeError(w, 400, "this kind of post can't be pinned. Only your own posts on your profile can")
		return
	}

	// One statement, so two quick pins can't both slip under the limit. Posts that
	// have been deleted don't count, so deleting a pinned post frees its slot.
	res, err := s.db.Exec(`
		INSERT INTO pinned_posts (user_id, post_id)
		SELECT $1::uuid, $2::uuid
		 WHERE (SELECT COUNT(*) FROM pinned_posts pp JOIN posts p ON p.id = pp.post_id
		         WHERE pp.user_id = $1::uuid AND p.deleted_at IS NULL) < $3
		ON CONFLICT (user_id, post_id) DO NOTHING`, userID, postID, MaxPinnedPosts)
	if err != nil {
		writeError(w, 500, "could not pin this post")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var already bool
		s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pinned_posts WHERE user_id = $1 AND post_id = $2)`, userID, postID).Scan(&already)
		if !already {
			writeError(w, 409, "You can pin up to 3 posts. Unpin one first.")
			return
		}
	}
	writeJSON(w, 200, map[string]any{"pinned": true})
}

// UnpinPost removes a pin. Idempotent: unpinning what isn't pinned is fine.
func (s *Service) UnpinPost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM pinned_posts WHERE user_id = $1 AND post_id = $2::uuid`, userID, postID)
	writeJSON(w, 200, map[string]any{"pinned": false})
}

// profilePinnedPosts returns the author's pinned posts that this viewer may see,
// newest pin first, built from the same query and audience rules as the profile's
// own posts (profileQuery), flagged pinned. A pin on a post the viewer can't see
// leaves no trace: not the post, not a gap.
func (s *Service) profilePinnedPosts(profileQuery string, viewerParam any, viewerID, authorID string) []Post {
	rows, err := s.db.Query(profileQuery+`
		  AND p.id IN (SELECT post_id FROM pinned_posts WHERE user_id = $2)
		ORDER BY (SELECT pinned_at FROM pinned_posts WHERE user_id = $2 AND post_id = p.id) DESC, p.id DESC
		LIMIT $3`, viewerParam, authorID, MaxPinnedPosts)
	if err != nil {
		log.Printf("feed: profilePinnedPosts: %v", err)
		return []Post{}
	}
	defer rows.Close()
	posts := scanPosts(rows)
	s.enrichReactions(posts, viewerID)
	s.enrichCustomHandles(posts)
	s.enrichNestedQuotes(posts)
	s.enrichPolls(posts, viewerID)
	s.enrichPhotos(posts)
	s.enrichExternalOnly(posts)
	for i := range posts {
		posts[i].Pinned = true
	}
	return posts
}

// markPinned (AGORA-447) flags the posts in a page that their author has pinned,
// in one query over the already-scanned posts (the same shape as markVideoMeta
// and for the same reason), so the post menu can offer Pin or Unpin and a profile
// can leave a pinned post out of the list below its pinned section.
func (s *Service) markPinned(posts []Post) {
	ids := make([]string, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		return
	}
	rows, err := s.db.Query(`
		SELECT pp.post_id FROM pinned_posts pp
		  JOIN posts p ON p.id = pp.post_id AND p.author_id = pp.user_id AND p.deleted_at IS NULL
		 WHERE pp.post_id = ANY($1::uuid[])`, pq.Array(ids))
	if err != nil {
		log.Printf("feed: markPinned: %v", err)
		return
	}
	defer rows.Close()
	pinned := map[string]bool{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			pinned[id] = true
		}
	}
	for i := range posts {
		posts[i].Pinned = pinned[posts[i].ID]
	}
}
