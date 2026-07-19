package feed

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/albums"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/media"
	"github.com/agora-social/agora/internal/notifications"
	"github.com/agora-social/agora/internal/store"
)

var mentionRe   = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)
var groupTagRe  = regexp.MustCompile(`\+([a-zA-Z0-9_-]+)`) // AGORA-89
// fediverseMentionRe (AGORA-163) matches a full @handle@instance.tld mention
// shape, distinct from a bare local @username. Duplicated (not imported) in
// internal/federation/activitypub.go, which does the actual resolve/deliver
// work — this package only needs to know a match is fediverse-shaped so
// notifyMentions doesn't also treat it as a (near-certainly wrong) local
// mention. Keep both copies in sync if this pattern ever changes.
// Local part allows dots/hyphens — a Bridgy Fed bridged Bluesky actor's
// "handle" is itself a dotted AT Proto handle, e.g. @jane.bsky.social@bsky.brid.gy.
var fediverseMentionRe = regexp.MustCompile(`@([a-zA-Z0-9_.-]+)@([a-zA-Z0-9-]+\.[a-zA-Z0-9.-]+)`)

// fedSender is the subset of federation.Service used here.
type fedSender interface {
	BroadcastToFriendInstances(userID string, activity any)
	// BroadcastPublicPost/BroadcastDeletePost drive standard ActivityPub
	// delivery to a user's fediverse followers (AGORA-145) — distinct from
	// BroadcastToFriendInstances, which serves the older Agora-to-Agora protocol.
	BroadcastPublicPost(userID, postID string)
	BroadcastDeletePost(userID, postID string)
	// BroadcastUpdatePost delivers a signed Update when a federated post is
	// edited (AGORA-150).
	BroadcastUpdatePost(userID, postID string)
	// DeliverReply drives outbound ActivityPub delivery for a comment that
	// directly replies to a fediverse participant (AGORA-147).
	DeliverReply(userID, commentID, replyToID string)
	// DeliverReplyUpdate mirrors DeliverReply but for an edit (AGORA-162).
	DeliverReplyUpdate(userID, commentID, replyToID string)
	// BroadcastPagePostUpdate/Delete (AGORA-115): a page post's edit/delete
	// goes through this same generic EditPost/DeletePost, but must federate
	// under the page's own actor, not the posting member's — page posts are
	// broadcast to page_remote_subscribers, not the member's ap_followers.
	BroadcastPagePostUpdate(pageID, postID string)
	BroadcastPagePostDelete(pageID, postID string)
	// DeliverLike/DeliverUnlike (AGORA-158): outbound Like/Undo(Like) when
	// liking/unliking a remote post — the reverse of handleInboundLike
	// (AGORA-153), which only ever handled a remote actor liking one of ours.
	DeliverLike(userID, postID string)
	DeliverUnlike(userID, postID string)
	// DeliverAnnounce/DeliverUnannounce (AGORA-159): outbound Announce/
	// Undo(Announce) when reposting/un-reposting a remote post — the reverse
	// of handleInboundAnnounce (AGORA-153).
	DeliverAnnounce(userID, repostID, originalPostID string)
	DeliverUnannounce(userID, repostID, originalPostID string)
}

type Service struct {
	db     *store.DB
	notif  *notifications.Service
	media  *media.Service
	albums *albums.Service
	fed    fedSender
}

func NewService(db *store.DB, notif *notifications.Service, media *media.Service) *Service {
	return &Service{db: db, notif: notif, media: media}
}

func (s *Service) SetAlbums(a *albums.Service) { s.albums = a }
func (s *Service) SetFed(f fedSender)          { s.fed = f }

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/preview",                             s.GetLinkPreview)
	r.Get("/groups/mention-search",              s.GroupMentionSearch) // AGORA-89
	r.Get("/feed",                               s.GetFeed)
	r.Post("/posts",                             s.CreatePost)
	r.Delete("/posts/{id}",                      s.DeletePost)
	r.Patch("/posts/{id}",                       s.EditPost)
	r.Post("/posts/{id}/like",                   s.LikePost)
	r.Delete("/posts/{id}/like",                 s.UnlikePost)
	r.Post("/posts/{id}/react",                  s.ReactPost)
	r.Delete("/posts/{id}/react",                s.UnreactPost)
	r.Post("/posts/{id}/repost",                 s.Repost)
	r.Post("/posts/{id}/comments",                s.CreateComment)
	r.Delete("/posts/{id}/comments/{commentID}", s.DeleteComment)
	r.Patch("/posts/{id}/comments/{commentID}",  s.EditComment)
	r.Post("/posts/{id}/poll/vote",              s.PollVote)
	r.Delete("/posts/{id}/poll/vote",            s.PollUnvote)
	r.Post("/posts/{id}/poll/options",           s.PollAddOption)
	r.Get("/posts/{id}/poll/voters",             s.PollVoters)
	r.Get("/users/{username}/wall",              s.GetWall)
	r.Get("/users/me/wall-queue",                s.GetWallQueue)
	r.Post("/posts/{id}/wall-approve",           s.WallApprove)
	r.Post("/posts/{id}/wall-reject",            s.WallReject)
}

// RegisterPublicRoutes registers read-only routes reachable by guests
// (no auth required, though a valid token — via OptionalMiddleware — still
// personalizes results like/reaction state, blocks, etc).
func RegisterPublicRoutes(r chi.Router, s *Service) {
	r.Get("/feed/public",             s.PublicFeed)
	r.Get("/posts/{id}",              s.GetPost)
	r.Get("/posts/{id}/reactions",    s.GetReactions)
	r.Get("/posts/{id}/comments",     s.GetComments)
	r.Get("/users/{username}/posts",  s.GetUserPosts)
}

// ── Feed ──────────────────────────────────────────────────────────────────────

func (s *Service) GetFeed(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	limit, offset := pageParams(r)
	customFeedID := r.URL.Query().Get("custom_feed_id")
	if customFeedID != "" {
		s.execCustomFeed(w, userID, limit, offset, customFeedID)
		return
	}
	listID := r.URL.Query().Get("list_id")

	var rows *sql.Rows
	var err error

	if listID != "" {
		// List feed: posts from members of a specific friend list owned by this user
		rows, err = s.db.Query(`
			SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
			       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
			       cg.name, cg.slug,
			       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
			       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
			       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
			       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
			       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
			       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
			       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
			       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
			       rp.content, rp.image_url, rp.created_at,
			       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
			       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
			       p.video_url, p.video_thumb_url
			FROM posts p
			JOIN users u ON u.id = p.author_id
			LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
			LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
			LEFT JOIN community_groups cg ON cg.id = p.community_group_id
			LEFT JOIN users wu ON wu.id = p.wall_user_id
			LEFT JOIN pages pg ON pg.id = p.page_id
			WHERE p.parent_id IS NULL
			  AND p.deleted_at IS NULL
			  AND p.visibility != 'private'
			  AND (p.wall_user_id IS NULL OR p.wall_status = 'approved')
			  AND p.author_id IN (
			    SELECT friend_id FROM friend_group_members
			    WHERE group_id = $4
			    AND group_id IN (SELECT id FROM friend_groups WHERE user_id = $1)
			  )
			  AND (
			    p.visibility = 'public'
			    OR p.visibility = 'friends'
			    OR (p.visibility = 'group' AND p.group_id = $4)
			  )
			  AND (
			    p.community_group_id IS NULL
			    OR EXISTS (
			      SELECT 1 FROM community_group_members cgm
			      WHERE cgm.group_id = p.community_group_id AND cgm.user_id = $1
			    )
			  )
			ORDER BY p.created_at DESC
			LIMIT $2 OFFSET $3
		`, userID, limit, offset, listID)
	} else {
		rows, err = s.db.Query(`
			SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
			       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
			       cg.name, cg.slug,
			       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
			       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
			       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
			       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
			       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
			       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
			       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
			       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
			       rp.content, rp.image_url, rp.created_at,
			       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
			       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
			       p.video_url, p.video_thumb_url
			FROM posts p
			JOIN users u ON u.id = p.author_id
			LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
			LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
			LEFT JOIN community_groups cg ON cg.id = p.community_group_id
			LEFT JOIN users wu ON wu.id = p.wall_user_id
			LEFT JOIN pages pg ON pg.id = p.page_id
			WHERE p.parent_id IS NULL
			  AND p.deleted_at IS NULL
			  AND p.visibility != 'private'
			  AND (p.wall_user_id IS NULL OR p.wall_status = 'approved')
			  AND NOT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id = $1 AND blocked_id = p.author_id) OR (blocker_id = p.author_id AND blocked_id = $1))
			  AND (
			    p.author_id = $1
			    OR p.wall_user_id = $1
			    OR EXISTS(
			      SELECT 1 FROM friendships f
			      WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
			          OR (f.addressee_id = $1 AND f.requester_id = p.author_id))
			      AND f.status = 'accepted'
			    )
			    OR EXISTS(
			      SELECT 1 FROM friendships f
			      WHERE p.wall_user_id IS NOT NULL
			        AND ((f.requester_id = $1 AND f.addressee_id = p.wall_user_id)
			          OR (f.addressee_id = $1 AND f.requester_id = p.wall_user_id))
			      AND f.status = 'accepted'
			    )
			    OR (
			      -- AGORA-109: include posts from pages the user subscribes to
			      p.page_id IS NOT NULL
			      AND EXISTS(SELECT 1 FROM page_subscribers ps WHERE ps.page_id = p.page_id AND ps.user_id = $1)
			    )
			  )
			  AND (
			    -- Friend-list posts: only show if viewer is in that specific friend list
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
			  )
			ORDER BY p.created_at DESC
			LIMIT $2 OFFSET $3
		`, userID, limit, offset)
	}
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	s.enrichReactions(posts, userID)
	s.enrichPolls(posts, userID)
	s.enrichPhotos(posts)
	writeJSON(w, 200, map[string]any{"posts": posts})
}

func (s *Service) execCustomFeed(w http.ResponseWriter, userID string, limit, offset int, customFeedID string) {
	var smartRanking bool
	err := s.db.QueryRow(
		`SELECT smart_ranking FROM custom_feeds WHERE id = $1 AND owner_id = $2`,
		customFeedID, userID,
	).Scan(&smartRanking)
	if err != nil {
		writeError(w, 404, "feed not found")
		return
	}

	filterRows, err := s.db.Query(
		`SELECT filter_type, value FROM custom_feed_filters WHERE feed_id = $1`,
		customFeedID,
	)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer filterRows.Close()

	var friendGroupIDs, communityGroupIDs, excludeFriendIDs, excludeGroupIDs, postTypes []string
	var includePageIDs, excludePageIDs, fediverseAccountIDs []string
	var fediverseAll bool
	for filterRows.Next() {
		var ft, val string
		filterRows.Scan(&ft, &val)
		switch ft {
		case "friend_group":
			friendGroupIDs = append(friendGroupIDs, val)
		case "community_group":
			communityGroupIDs = append(communityGroupIDs, val)
		case "exclude_friend":
			excludeFriendIDs = append(excludeFriendIDs, val)
		case "exclude_group":
			excludeGroupIDs = append(excludeGroupIDs, val)
		case "post_type":
			postTypes = append(postTypes, val)
		case "include_page":
			includePageIDs = append(includePageIDs, val)
		case "exclude_page":
			excludePageIDs = append(excludePageIDs, val)
		case "fediverse_account":
			fediverseAccountIDs = append(fediverseAccountIDs, val)
		case "fediverse_all":
			fediverseAll = true
		}
	}
	filterRows.Close()

	// args[0]=userID ($1), args[1]=limit ($2), args[2]=offset ($3); extra filter values appended after
	args := []any{userID, limit, offset}
	paramIdx := 3

	nextP := func(val any) string {
		paramIdx++
		args = append(args, val)
		return fmt.Sprintf("$%d", paramIdx)
	}

	var extraClauses []string

	// Inclusion: posts must come from at least one included source (OR across groups/communities/pages)
	if len(friendGroupIDs) > 0 || len(communityGroupIDs) > 0 || len(includePageIDs) > 0 ||
		len(fediverseAccountIDs) > 0 || fediverseAll {
		var inclParts []string
		if len(friendGroupIDs) > 0 {
			phs := make([]string, len(friendGroupIDs))
			for i, id := range friendGroupIDs {
				phs[i] = nextP(id)
			}
			inclParts = append(inclParts, fmt.Sprintf(
				`p.author_id IN (
				  SELECT fgm.friend_id FROM friend_group_members fgm
				  JOIN friend_groups fg ON fg.id = fgm.group_id
				  WHERE fgm.group_id IN (%s) AND fg.user_id = $1
				)`, strings.Join(phs, ",")))
		}
		if len(communityGroupIDs) > 0 {
			phs := make([]string, len(communityGroupIDs))
			for i, id := range communityGroupIDs {
				phs[i] = nextP(id)
			}
			inclParts = append(inclParts, fmt.Sprintf(
				`p.community_group_id IN (%s)`, strings.Join(phs, ",")))
		}
		// AGORA-111: include posts from specific pages
		if len(includePageIDs) > 0 {
			phs := make([]string, len(includePageIDs))
			for i, id := range includePageIDs {
				phs[i] = nextP(id)
			}
			inclParts = append(inclParts, fmt.Sprintf(
				`p.page_id IN (%s)`, strings.Join(phs, ",")))
		}
		// AGORA-146: a specific followed fediverse account. The EXISTS check
		// re-verifies the viewer actually follows that account (not just
		// that the stored filter value names it) so a filter's value can't
		// be used to see an account the viewer doesn't follow.
		if len(fediverseAccountIDs) > 0 {
			phs := make([]string, len(fediverseAccountIDs))
			for i, id := range fediverseAccountIDs {
				phs[i] = nextP(id)
			}
			inclParts = append(inclParts, fmt.Sprintf(
				`(p.author_id IN (%s) AND EXISTS(
				  SELECT 1 FROM ap_following af JOIN users ru ON ru.ap_actor_url = af.followed_actor_url
				  WHERE af.follower_user_id = $1 AND af.accepted = true AND ru.id = p.author_id
				))`, strings.Join(phs, ",")))
		}
		// AGORA-146: every fediverse account the viewer follows.
		if fediverseAll {
			inclParts = append(inclParts,
				`(p.is_remote = true AND EXISTS(
				  SELECT 1 FROM ap_following af JOIN users ru ON ru.ap_actor_url = af.followed_actor_url
				  WHERE af.follower_user_id = $1 AND af.accepted = true AND ru.id = p.author_id
				))`)
		}
		extraClauses = append(extraClauses, "("+strings.Join(inclParts, " OR ")+")")
	}

	// Exclusions
	if len(excludeFriendIDs) > 0 {
		phs := make([]string, len(excludeFriendIDs))
		for i, id := range excludeFriendIDs {
			phs[i] = nextP(id)
		}
		extraClauses = append(extraClauses, fmt.Sprintf(
			`p.author_id NOT IN (%s)`, strings.Join(phs, ",")))
	}
	if len(excludeGroupIDs) > 0 {
		phs := make([]string, len(excludeGroupIDs))
		for i, id := range excludeGroupIDs {
			phs[i] = nextP(id)
		}
		extraClauses = append(extraClauses, fmt.Sprintf(
			`(p.community_group_id IS NULL OR p.community_group_id NOT IN (%s))`,
			strings.Join(phs, ",")))
	}
	// AGORA-111: exclude posts from specific pages
	if len(excludePageIDs) > 0 {
		phs := make([]string, len(excludePageIDs))
		for i, id := range excludePageIDs {
			phs[i] = nextP(id)
		}
		extraClauses = append(extraClauses, fmt.Sprintf(
			`(p.page_id IS NULL OR p.page_id NOT IN (%s))`,
			strings.Join(phs, ",")))
	}

	// Post type filter
	if len(postTypes) > 0 {
		var typeParts []string
		for _, pt := range postTypes {
			switch pt {
			case "repost":
				typeParts = append(typeParts, `p.repost_of_id IS NOT NULL`)
			case "media":
				typeParts = append(typeParts, `(p.image_url != '' OR EXISTS(SELECT 1 FROM post_photos WHERE post_id = p.id))`)
			case "text":
				typeParts = append(typeParts, `(p.repost_of_id IS NULL AND p.image_url = '')`)
			}
		}
		if len(typeParts) > 0 {
			extraClauses = append(extraClauses, "("+strings.Join(typeParts, " OR ")+")")
		}
	}

	extra := ""
	if len(extraClauses) > 0 {
		extra = "AND " + strings.Join(extraClauses, "\n  AND ")
	}

	query := fmt.Sprintf(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
		       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
		       rp.content, rp.image_url, rp.created_at,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
		       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
		       p.video_url, p.video_thumb_url
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
		LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
		LEFT JOIN pages pg ON pg.id = p.page_id
		WHERE p.parent_id IS NULL
		  AND p.deleted_at IS NULL
		  AND p.visibility != 'private'
		  AND (p.wall_user_id IS NULL OR p.wall_status = 'approved')
		  AND NOT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id = $1 AND blocked_id = p.author_id) OR (blocker_id = p.author_id AND blocked_id = $1))
		  AND (
		    p.author_id = $1
		    OR p.wall_user_id = $1
		    OR EXISTS(
		      SELECT 1 FROM friendships f
		      WHERE ((f.requester_id = $1 AND f.addressee_id = p.author_id)
		          OR (f.addressee_id = $1 AND f.requester_id = p.author_id))
		      AND f.status = 'accepted'
		    )
		    OR EXISTS(
		      SELECT 1 FROM friendships f
		      WHERE p.wall_user_id IS NOT NULL
		        AND ((f.requester_id = $1 AND f.addressee_id = p.wall_user_id)
		          OR (f.addressee_id = $1 AND f.requester_id = p.wall_user_id))
		      AND f.status = 'accepted'
		    )
		    -- AGORA-146: a remote followed account has no friendships row —
		    -- scoped to custom feeds only (not the main feed query), matching
		    -- the ticket's design that fediverse follows surface only through
		    -- an explicit custom-feed filter, never the public instance feed.
		    OR EXISTS(
		      SELECT 1 FROM ap_following af JOIN users ru ON ru.ap_actor_url = af.followed_actor_url
		      WHERE af.follower_user_id = $1 AND af.accepted = true AND ru.id = p.author_id
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
		  )
		  AND (
		    p.community_group_id IS NULL
		    OR EXISTS (
		      SELECT 1 FROM community_group_members cgm
		      WHERE cgm.group_id = p.community_group_id AND cgm.user_id = $1
		    )
		  )
		  %s
		ORDER BY p.created_at DESC
		LIMIT $2 OFFSET $3
	`, extra)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	s.enrichReactions(posts, userID)
	s.enrichPolls(posts, userID)
	s.enrichPhotos(posts)

	// AGORA-103: smart ranking — re-sort by interaction score × recency
	if smartRanking && len(posts) > 1 {
		posts = s.rankPosts(posts, userID)
	}

	writeJSON(w, 200, map[string]any{"posts": posts})
}

// PublicFeed serves a chronological, instance-wide feed of public posts for
// guests and members alike: authored by non-profile_private users, top-level
// (no comments), and not scoped to a wall/group/page. Unlike GetFeed this is
// not personalized to a friend graph.
func (s *Service) PublicFeed(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	limit, offset := pageParams(r)

	// viewerID feeds a uuid column below; an empty string (guest) is invalid
	// input for the uuid type, so use NULL instead.
	var viewerParam any = viewerID
	if viewerID == "" {
		viewerParam = nil
	}

	blockClause := ""
	if viewerID != "" {
		blockClause = `AND NOT EXISTS (SELECT 1 FROM blocks WHERE (blocker_id = $1 AND blocked_id = p.author_id) OR (blocker_id = p.author_id AND blocked_id = $1))`
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
		       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
		       rp.content, rp.image_url, rp.created_at,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
		       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
		       p.video_url, p.video_thumb_url
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
		LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
		LEFT JOIN pages pg ON pg.id = p.page_id
		WHERE p.parent_id IS NULL
		  AND p.deleted_at IS NULL
		  AND p.visibility = 'public'
		  AND NOT u.profile_private
		  AND u.deletion_scheduled_at IS NULL
		  AND p.wall_user_id IS NULL
		  AND p.community_group_id IS NULL
		  AND p.page_id IS NULL
		  %s
		ORDER BY p.created_at DESC
		LIMIT $2 OFFSET $3
	`, blockClause), viewerParam, limit, offset)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	s.enrichReactions(posts, viewerID)
	s.enrichPolls(posts, viewerID)
	s.enrichPhotos(posts)

	writeJSON(w, 200, map[string]any{"posts": posts})
}

// rankPosts scores and re-orders posts using the viewer's historical interaction
// data with each post's author. Score = interaction_weight × recency_decay.
// Weights: comment=5, like=3, repost=2, link_click=1, profile_view=0.5, post_view=0.1
func (s *Service) rankPosts(posts []Post, userID string) []Post {
	// Fetch weighted interaction scores per target author in one query
	rows, err := s.db.Query(`
		SELECT target_user_id,
		       SUM(CASE interaction_type
		           WHEN 'comment'      THEN 5.0
		           WHEN 'like'         THEN 3.0
		           WHEN 'repost'       THEN 2.0
		           WHEN 'link_click'   THEN 1.0
		           WHEN 'profile_view' THEN 0.5
		           WHEN 'post_view'    THEN 0.1
		           ELSE 0 END) AS score
		FROM feed_interactions
		WHERE user_id = $1
		  AND target_user_id IS NOT NULL
		  AND created_at > NOW() - INTERVAL '90 days'
		GROUP BY target_user_id
	`, userID)
	if err != nil {
		return posts // ranking failure is non-fatal; return original order
	}
	defer rows.Close()

	authorScore := map[string]float64{}
	for rows.Next() {
		var authorID string
		var score float64
		rows.Scan(&authorID, &score)
		authorScore[authorID] = score
	}

	if len(authorScore) == 0 {
		return posts // no interaction data yet; keep chronological
	}

	now := float64(time.Now().Unix())
	type scored struct {
		post  Post
		score float64
	}
	scored_posts := make([]scored, len(posts))
	for i, p := range posts {
		iScore := authorScore[p.AuthorID]
		// Recency decay: halve interaction weight every 7 days
		postTime := float64(0)
		if t, err := time.Parse(time.RFC3339, p.CreatedAt); err == nil {
			postTime = float64(t.Unix())
		}
		ageDays := (now - postTime) / 86400.0
		recencyDecay := 1.0 / (1.0 + ageDays/7.0)
		scored_posts[i] = scored{p, iScore*recencyDecay + recencyDecay}
	}

	// Sort descending by score (stable to preserve relative order of ties)
	for i := 1; i < len(scored_posts); i++ {
		for j := i; j > 0 && scored_posts[j].score > scored_posts[j-1].score; j-- {
			scored_posts[j], scored_posts[j-1] = scored_posts[j-1], scored_posts[j]
		}
	}

	result := make([]Post, len(posts))
	for i, sp := range scored_posts {
		result[i] = sp.post
	}
	return result
}

func (s *Service) GetUserPosts(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	username := chi.URLParam(r, "username")
	limit, offset := pageParams(r)

	var authorID string
	var profilePrivate, hideTimeline bool
	s.db.QueryRow(`SELECT id, profile_private, hide_timeline FROM users WHERE username = $1 AND deletion_scheduled_at IS NULL`, username).Scan(&authorID, &profilePrivate, &hideTimeline)
	if authorID == "" {
		writeError(w, 404, "user not found")
		return
	}

	// Determine relationship
	isSelf   := viewerID == authorID
	isFriend := false
	if !isSelf && viewerID != "" {
		s.db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM friendships
				WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
				AND status = 'accepted'
			)
		`, viewerID, authorID).Scan(&isFriend)
	}

	// Self always sees their own timeline regardless of any privacy setting
	if !isSelf {
		// Private profile: non-friends see nothing
		if profilePrivate && !isFriend {
			writeJSON(w, 200, map[string]any{"posts": []any{}})
			return
		}
		// Hide timeline: nobody sees the profile timeline (posts still flow to feeds by visibility)
		if hideTimeline {
			writeJSON(w, 200, map[string]any{"posts": []any{}})
			return
		}
	}

	// Block gate
	if !isSelf && viewerID != "" {
		var isBlocked bool
		s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`,
			viewerID, authorID).Scan(&isBlocked)
		if isBlocked {
			writeJSON(w, 200, map[string]any{"posts": []any{}})
			return
		}
	}

	// Build visibility filter
	var visFilter string
	switch {
	case isSelf:
		// Own profile: see everything including private posts
		visFilter = `true`
	case isFriend:
		// Friends: public + friends + friend-list posts where viewer is in the list
		visFilter = `(
			p.visibility = 'public'
			OR p.visibility = 'friends'
			OR (
				p.visibility = 'group'
				AND p.group_id IS NOT NULL
				AND EXISTS (
					SELECT 1 FROM friend_group_members fgm
					JOIN friend_groups fg ON fg.id = fgm.group_id
					WHERE fgm.group_id = p.group_id
					  AND fgm.friend_id = $1
					  AND fg.user_id = $2
				)
			)
		)`
	default:
		// Not friends, public profile: public only
		visFilter = `p.visibility = 'public'`
	}

	// viewerID is compared against uuid columns; an empty string (guest) is
	// invalid input for the uuid type, so pass NULL instead — comparisons
	// against NULL correctly evaluate to false/no-match.
	var viewerParam any = viewerID
	if viewerID == "" {
		viewerParam = nil
	}

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
			       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::timestamptz,
		       NULL::uuid, NULL::text, NULL::text, 'approved'::text,
		       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
		       p.video_url, p.video_thumb_url
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN pages pg ON pg.id = p.page_id
		WHERE p.author_id = $2 AND p.parent_id IS NULL AND p.deleted_at IS NULL
		  AND p.wall_user_id IS NULL
		  AND `+visFilter+`
		ORDER BY p.created_at DESC LIMIT $3 OFFSET $4
	`, viewerParam, authorID, limit, offset)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	s.enrichReactions(posts, viewerID)
	s.enrichPolls(posts, viewerID)
	s.enrichPhotos(posts)
	writeJSON(w, 200, map[string]any{"posts": posts})
}

// ── Post CRUD ─────────────────────────────────────────────────────────────────

func (s *Service) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Content         string   `json:"content"`
		ImageURL        string   `json:"image_url"`
		ImageURLs       []string `json:"image_urls"`
		VideoURL        string   `json:"video_url"`
		VideoThumbURL   string   `json:"video_thumb_url"`
		Visibility      string   `json:"visibility"`
		GroupID         string   `json:"group_id"`
		ContentWarning  string   `json:"content_warning"`
		LinkURL         string   `json:"link_url"`
		LinkTitle       string   `json:"link_title"`
		LinkDescription string   `json:"link_description"`
		LinkImage       string   `json:"link_image"`
		LinkDomain      string   `json:"link_domain"`
		PollOptions          []string `json:"poll_options"`
		PollExpiresHours     int      `json:"poll_expires_hours"`
		PollMultipleChoice   bool     `json:"poll_multiple_choice"`
		PollAllowsNewOptions bool     `json:"poll_allows_new_options"`
		WallUserID           string   `json:"wall_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}

	// Validate poll options: 2–6 non-empty options if provided
	var pollOpts []string
	for _, o := range req.PollOptions {
		if t := strings.TrimSpace(o); t != "" {
			pollOpts = append(pollOpts, t)
		}
	}
	if len(pollOpts) == 1 || len(pollOpts) > 6 {
		writeError(w, 400, "polls require 2–6 options")
		return
	}

	// Normalize: if image_urls provided, use those; fall back to image_url
	if len(req.ImageURLs) > 0 {
		// Cap at 10 photos (AGORA-122)
		if len(req.ImageURLs) > 10 {
			req.ImageURLs = req.ImageURLs[:10]
		}
		req.ImageURL = req.ImageURLs[0]
	} else if req.ImageURL != "" {
		req.ImageURLs = []string{req.ImageURL}
	}

	// Allow text, image, video, or poll — any one is sufficient (AGORA-119)
	if req.Content == "" && req.ImageURL == "" && req.VideoURL == "" && len(pollOpts) == 0 {
		writeError(w, 400, "post must have content, image, or video")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "friends"
	}

	// Resolve Tenor/Giphy share page URLs to direct media URLs
	if req.ImageURL != "" {
		if resolved := resolveGifURL(req.ImageURL); resolved != "" {
			req.ImageURL = resolved
			req.ImageURLs[0] = req.ImageURL
		}
	}

	var friendGroupID *string
	var communityGroupID *string
	if req.GroupID != "" && req.Visibility == "group" {
		// group_id on posts references friend_groups (friend lists)
		// community_group_id references community_groups — separate concept
		friendGroupID = &req.GroupID
	}

	// Wall post handling
	var wallUserID *string
	wallStatus := "approved"
	if req.WallUserID != "" && req.WallUserID != userID {
		// Must be friends
		var isFriend bool
		s.db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM friendships
				WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
				AND status = 'accepted'
			)
		`, userID, req.WallUserID).Scan(&isFriend)
		if !isFriend {
			writeError(w, 403, "you can only post on friends' walls")
			return
		}
		wallUserID = &req.WallUserID
		// Check if target requires approval
		var approvalRequired bool
		s.db.QueryRow(`SELECT wall_approval_required FROM users WHERE id = $1`, req.WallUserID).Scan(&approvalRequired)
		if approvalRequired {
			wallStatus = "pending"
		}

		// Block check — can't post on wall of someone who blocked you or vice versa
		var isBlocked bool
		s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`,
			userID, req.WallUserID).Scan(&isBlocked)
		if isBlocked {
			writeError(w, 403, "cannot post on this user's wall")
			return
		}
		// Wall posts are always friends visibility
		req.Visibility = "friends"
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO posts (author_id, content, image_url, video_url, video_thumb_url,
		                   visibility, community_group_id, group_id, content_warning,
		                   link_url, link_title, link_description, link_image, link_domain,
		                   wall_user_id, wall_status,
		                   poll_multiple_choice, poll_allows_new_options,
		                   poll_expires_at)
		VALUES ($1, $2, $3, $18, $19, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
		        CASE WHEN $17 > 0 THEN NOW() + ($17 * INTERVAL '1 hour') ELSE NULL END)
		RETURNING id
	`, userID, req.Content, req.ImageURL, req.Visibility, communityGroupID, friendGroupID, req.ContentWarning,
		req.LinkURL, req.LinkTitle, req.LinkDescription, req.LinkImage, req.LinkDomain,
		wallUserID, wallStatus,
		req.PollMultipleChoice, req.PollAllowsNewOptions, req.PollExpiresHours,
		req.VideoURL, req.VideoThumbURL).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not create post")
		return
	}

	// Insert poll options if provided
	for i, opt := range pollOpts {
		s.db.Exec(`INSERT INTO poll_options (post_id, text, position) VALUES ($1, $2, $3)`, id, opt, i)
	}

	// Insert photos if multiple
	if len(req.ImageURLs) > 1 {
		for i, u := range req.ImageURLs {
			s.db.Exec(`INSERT INTO post_photos (post_id, url, position) VALUES ($1, $2, $3)`, id, u, i)
		}
	}

	go s.notifyMentions(req.Content, userID, id)
	go s.notifyGroupTags(req.Content, userID, id) // AGORA-89
	if wallUserID == nil {
		go s.notifyPostFollowers(userID, id)
	} else if wallStatus == "approved" {
		// Notify the wall owner
		s.notif.Create(*wallUserID, userID, "wall_post", id, "")
	} else {
		// Notify wall owner they have a pending post to review
		s.notif.Create(*wallUserID, userID, "wall_post_pending", id, "")
	}

	// Add images to user's Timeline Photos album
	if len(req.ImageURLs) > 0 && s.albums != nil {
		for _, u := range req.ImageURLs {
			u := u
			go s.albums.AddToTimelineAlbum(userID, u)
		}
	}

	// Broadcast public posts to federated friend instances
	if req.Visibility == "public" && s.fed != nil {
		var username string
		s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
		go s.fed.BroadcastToFriendInstances(userID, map[string]any{
			"type":  "post",
			"actor": username,
			"object": map[string]any{
				"id":            id,
				"content":       req.Content,
				"image_url":     req.ImageURL,
				"visibility":    "public",
				"author_handle": username,
				"created_at":    timeNow(),
			},
		})
		// AGORA-145: also deliver to standard ActivityPub followers (Mastodon etc.)
		go s.fed.BroadcastPublicPost(userID, id)
	}

	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Service) GetPost(w http.ResponseWriter, r *http.Request) {
	id       := chi.URLParam(r, "id")
	viewerID := auth.UserIDFromCtx(r.Context())

	// First fetch the post's core fields to check access before running the full query
	var authorID, visibility string
	var communityGroupID *string
	var parentID *string
	err := s.db.QueryRow(`
		SELECT author_id, visibility, community_group_id, parent_id
		FROM posts WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&authorID, &visibility, &communityGroupID, &parentID)
	if err != nil {
		writeError(w, 404, "post not found")
		return
	}

	// If this is a comment (has parent_id), redirect caller to the parent post
	if parentID != nil {
		writeJSON(w, 200, map[string]any{"redirect_to_post": *parentID})
		return
	}

	// Author's profile-private gate applies regardless of the individual
	// post's own visibility, consistent with GetUserPosts's timeline gate.
	if authorID != viewerID {
		var authorProfilePrivate bool
		s.db.QueryRow(`SELECT profile_private FROM users WHERE id = $1`, authorID).Scan(&authorProfilePrivate)
		if authorProfilePrivate {
			var isFriend bool
			if viewerID != "" {
				s.db.QueryRow(`
					SELECT EXISTS(
						SELECT 1 FROM friendships
						WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
						AND status = 'accepted'
					)
				`, viewerID, authorID).Scan(&isFriend)
			}
			if !isFriend {
				writeJSON(w, 403, map[string]string{"error": "access_denied", "reason": "private_profile"})
				return
			}
		}
	}

	// Access control
	if authorID != viewerID {
		switch visibility {
		case "private":
			writeJSON(w, 403, map[string]string{"error": "access_denied", "reason": "private"})
			return
		case "friends", "group":
			// Check friendship
			var isFriend bool
			s.db.QueryRow(`
				SELECT EXISTS(
					SELECT 1 FROM friendships
					WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
					AND status = 'accepted'
				)
			`, viewerID, authorID).Scan(&isFriend)
			if !isFriend {
				writeJSON(w, 403, map[string]string{"error": "access_denied", "reason": "not_friends"})
				return
			}
		}

		// Group post — additionally check group membership
		if communityGroupID != nil {
			var isMember bool
			s.db.QueryRow(`
				SELECT EXISTS(
					SELECT 1 FROM community_group_members
					WHERE group_id = $1 AND user_id = $2
				)
			`, *communityGroupID, viewerID).Scan(&isMember)
			if !isMember {
				// Get group name and slug for a helpful message
				var groupName, groupSlug string
				s.db.QueryRow(`SELECT name, slug FROM community_groups WHERE id = $1`, *communityGroupID).
					Scan(&groupName, &groupSlug)
				writeJSON(w, 403, map[string]any{
					"error":      "access_denied",
					"reason":     "not_group_member",
					"group_name": groupName,
					"group_slug": groupSlug,
				})
				return
			}
		}
	}

	// viewerID feeds uuid columns below; an empty string (guest) is invalid
	// input for the uuid type, so use NULL instead.
	var viewerParam any = viewerID
	if viewerID == "" {
		viewerParam = nil
	}

	// Access granted — run the full query
	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
			   cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $2) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $2) AS reposted,
		       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
		       rp.content, rp.image_url, rp.created_at,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
		       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
		       p.video_url, p.video_thumb_url
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN posts rp   ON rp.id = p.repost_of_id
		LEFT JOIN users rp_u ON rp_u.id = rp.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
		LEFT JOIN pages pg ON pg.id = p.page_id
		WHERE p.id = $1 AND p.deleted_at IS NULL
	`, id, viewerParam)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	if len(posts) == 0 {
		writeError(w, 404, "post not found")
		return
	}
	s.enrichReactions(posts, viewerID)
	s.enrichPolls(posts, viewerID)
	s.enrichPhotos(posts)
	writeJSON(w, 200, map[string]any{"post": posts[0]})
}

func (s *Service) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	role := auth.RoleFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var authorID string
	var wallUserID, pageID, repostOfID *string
	s.db.QueryRow(`SELECT author_id, wall_user_id, page_id, repost_of_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&authorID, &wallUserID, &pageID, &repostOfID)
	if authorID == "" {
		writeError(w, 404, "post not found")
		return
	}
	isWallOwner := wallUserID != nil && *wallUserID == userID
	if authorID != userID && role != "admin" && role != "moderator" && !isWallOwner {
		writeError(w, 403, "forbidden")
		return
	}

	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, id)
	if pageID != nil {
		// Mirrors CreatePost's post_count increment — without this, a page's
		// displayed post count only ever grows, never reflecting deletions.
		s.db.Exec(`UPDATE pages SET post_count = post_count - 1 WHERE id = $1`, *pageID)
	}

	// Broadcast deletion to federated instances
	if s.fed != nil {
		if pageID != nil {
			// AGORA-115: a page post federates under the page's own actor.
			go s.fed.BroadcastPagePostDelete(*pageID, id)
		} else if repostOfID != nil {
			// AGORA-159: a repost was never its own Create — undo the
			// Announce instead of sending a Delete/Tombstone nobody
			// remote has a matching Create for. DeliverUnannounce itself
			// no-ops if the original post isn't remote.
			go s.fed.DeliverUnannounce(authorID, id, *repostOfID)
		} else {
			var username string
			s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
			go s.fed.BroadcastToFriendInstances(userID, map[string]any{
				"type":  "delete_post",
				"actor": username,
				"object": map[string]string{"id": id},
			})
			// AGORA-145: notify standard ActivityPub followers too. Uses authorID,
			// not the deleter (userID may be an admin/moderator), since the Delete
			// must come from the same actor that federated the original post.
			go s.fed.BroadcastDeletePost(authorID, id)
		}
	}

	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

// ── Likes ─────────────────────────────────────────────────────────────────────

func (s *Service) LikePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	s.db.Exec(`INSERT INTO likes (user_id, post_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, postID)

	var authorID string
	var parentID *string
	s.db.QueryRow(`SELECT author_id, parent_id FROM posts WHERE id = $1`, postID).Scan(&authorID, &parentID)
	if authorID != "" && authorID != userID {
		notifType := "post_like"
		if parentID != nil {
			notifType = "comment_like"
		}
		go s.notif.Create(authorID, userID, notifType, postID, "")
	}
	// AGORA-158: federate the like if the target is a remote post.
	if s.fed != nil {
		go s.fed.DeliverLike(userID, postID)
	}
	writeJSON(w, 200, map[string]string{"message": "liked"})
}

func (s *Service) UnlikePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, userID, postID)
	if s.fed != nil {
		go s.fed.DeliverUnlike(userID, postID)
	}
	writeJSON(w, 200, map[string]string{"message": "unliked"})
}

// ── Reactions (AGORA-25) ──────────────────────────────────────────────────────

var validReactions = map[string]bool{
	"like": true, "love": true, "laugh": true, "wow": true, "angry": true,
	"care": true, "pride": true, "thankful": true, "vomit": true,
}

func (s *Service) ReactPost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var req struct {
		Type string `json:"type"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if !validReactions[req.Type] {
		writeError(w, 400, "invalid reaction type")
		return
	}

	// Upsert — replaces any existing reaction from this user on this post
	s.db.Exec(`
		INSERT INTO reactions (user_id, post_id, reaction_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, post_id) DO UPDATE SET reaction_type = $3, created_at = NOW()
	`, userID, postID, req.Type)
	// Clear any legacy like row so counts don't double-count
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, userID, postID)

	// Notification: find post author, fire if not self
	var authorID string
	var parentID *string
	s.db.QueryRow(`SELECT author_id, parent_id FROM posts WHERE id = $1`, postID).Scan(&authorID, &parentID)
	if authorID != "" && authorID != userID {
		notifType := "post_reaction"
		notifPostID := postID
		if parentID != nil {
			notifType = "comment_reaction"
			// Navigate to the parent post, not the comment itself
			// Walk up to find the root post (handles depth-2 replies too)
			rootID := *parentID
			var grandParentID *string
			s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1`, rootID).Scan(&grandParentID)
			if grandParentID != nil {
				rootID = *grandParentID
			}
			notifPostID = rootID
		}
		go s.notif.Create(authorID, userID, notifType, notifPostID, req.Type)
	}

	// AGORA-158: standard ActivityPub only has a plain Like, no concept of
	// emoji reaction types — federate only when the reaction is exactly
	// "like", the same restriction Mastodon's own favourite maps to.
	if s.fed != nil && req.Type == "like" {
		go s.fed.DeliverLike(userID, postID)
	}

	writeJSON(w, 200, map[string]string{"message": "reacted", "type": req.Type})
}

func (s *Service) UnreactPost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM reactions WHERE user_id = $1 AND post_id = $2`, userID, postID)
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, userID, postID)
	// AGORA-158: DeliverUnlike itself no-ops safely if there was never a
	// federated like to undo (e.g. the reaction was "love", not "like").
	if s.fed != nil {
		go s.fed.DeliverUnlike(userID, postID)
	}
	writeJSON(w, 200, map[string]string{"message": "unreacted"})
}

// ── Polls (AGORA-5) ───────────────────────────────────────────────────────────

type PollOption struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Votes    int    `json:"votes"`
	Position int    `json:"position"`
}

func (s *Service) PollVote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var req struct {
		OptionID string `json:"option_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OptionID == "" {
		writeError(w, 400, "option_id required")
		return
	}

	// Verify option belongs to this post
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM poll_options WHERE id = $1 AND post_id = $2`, req.OptionID, postID).Scan(&count)
	if count == 0 {
		writeError(w, 404, "option not found")
		return
	}

	// Check poll hasn't expired
	var isExpired bool
	s.db.QueryRow(`SELECT poll_expires_at IS NOT NULL AND poll_expires_at < NOW() FROM posts WHERE id = $1`, postID).Scan(&isExpired)
	if isExpired {
		writeError(w, 403, "this poll has ended")
		return
	}

	// Check if multiple choice
	var multipleChoice bool
	s.db.QueryRow(`SELECT poll_multiple_choice FROM posts WHERE id = $1`, postID).Scan(&multipleChoice)

	if !multipleChoice {
		// Single choice: remove any existing vote on this poll first
		s.db.Exec(`
			DELETE FROM poll_votes
			WHERE user_id = $1
			  AND option_id IN (SELECT id FROM poll_options WHERE post_id = $2)
		`, userID, postID)
	}

	// Cast vote
	s.db.Exec(`INSERT INTO poll_votes (user_id, option_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, req.OptionID)

	writeJSON(w, 200, map[string]string{"message": "voted"})
}

func (s *Service) PollAddOption(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, 400, "text required")
		return
	}

	// Check post allows new options and poll hasn't expired
	var allowsNew bool
	var isExpired bool
	err := s.db.QueryRow(`
		SELECT poll_allows_new_options,
		       (poll_expires_at IS NOT NULL AND poll_expires_at < NOW())
		FROM posts WHERE id = $1
	`, postID).Scan(&allowsNew, &isExpired)
	if err != nil {
		writeError(w, 404, "post not found"); return
	}
	if !allowsNew {
		writeError(w, 403, "this poll does not allow new options"); return
	}
	if isExpired {
		writeError(w, 403, "this poll has ended"); return
	}

	// Check option count doesn't exceed 10
	var optCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM poll_options WHERE post_id = $1`, postID).Scan(&optCount)
	if optCount >= 10 {
		writeError(w, 400, "poll already has maximum options"); return
	}

	var optID string
	s.db.QueryRow(`
		INSERT INTO poll_options (post_id, text, position)
		VALUES ($1, $2, (SELECT COALESCE(MAX(position)+1, 0) FROM poll_options WHERE post_id = $1))
		RETURNING id
	`, postID, strings.TrimSpace(req.Text)).Scan(&optID)

	// Auto-vote for the user who added the option
	if optID != "" {
		s.db.Exec(`INSERT INTO poll_votes (user_id, option_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, optID)
	}

	writeJSON(w, 201, map[string]string{"message": "option added", "id": optID})
}

func (s *Service) PollUnvote(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	s.db.Exec(`
		DELETE FROM poll_votes
		WHERE user_id = $1
		  AND option_id IN (SELECT id FROM poll_options WHERE post_id = $2)
	`, userID, postID)
	writeJSON(w, 200, map[string]string{"message": "unvoted"})
}

// PollVoters returns per-option voter lists for a post's poll (AGORA-48).
func (s *Service) PollVoters(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")

	rows, err := s.db.Query(`
		SELECT po.id, po.text,
		       u.id, u.username, u.display_name, u.avatar_url
		FROM poll_options po
		JOIN poll_votes pv ON pv.option_id = po.id
		JOIN users u ON u.id = pv.user_id
		WHERE po.post_id = $1
		ORDER BY po.position, u.display_name
	`, postID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	type Voter struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	type OptionVoters struct {
		OptionID   string  `json:"option_id"`
		OptionText string  `json:"option_text"`
		Voters     []Voter `json:"voters"`
	}

	byOption := map[string]*OptionVoters{}
	order := []string{}

	for rows.Next() {
		var optID, optText string
		var v Voter
		rows.Scan(&optID, &optText, &v.ID, &v.Username, &v.DisplayName, &v.AvatarURL)
		if _, ok := byOption[optID]; !ok {
			byOption[optID] = &OptionVoters{OptionID: optID, OptionText: optText, Voters: []Voter{}}
			order = append(order, optID)
		}
		byOption[optID].Voters = append(byOption[optID].Voters, v)
	}

	result := make([]OptionVoters, 0, len(order))
	for _, id := range order {
		result = append(result, *byOption[id])
	}
	writeJSON(w, 200, map[string]any{"options": result})
}

// ── Wall (AGORA-19) ───────────────────────────────────────────────────────────

func (s *Service) GetWall(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	viewerID := auth.UserIDFromCtx(r.Context())

	var wallOwnerID string
	s.db.QueryRow(`SELECT id FROM users WHERE username = $1 AND deletion_scheduled_at IS NULL`, username).Scan(&wallOwnerID)
	if wallOwnerID == "" {
		writeError(w, 404, "user not found"); return
	}

	// Viewers see approved posts only; owner sees all (pending too)
	statusFilter := `p.wall_status = 'approved'`
	if viewerID == wallOwnerID {
		statusFilter = `p.wall_status IN ('approved','pending')`
	}

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
		       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning,
		       p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::timestamptz,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
		       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
		       p.video_url, p.video_thumb_url
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
		LEFT JOIN pages pg ON pg.id = p.page_id
		WHERE p.wall_user_id = $2
		  AND p.deleted_at IS NULL
		  AND `+statusFilter+`
		ORDER BY p.created_at DESC
		LIMIT 50
	`, viewerID, wallOwnerID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	s.enrichReactions(posts, viewerID)
	s.enrichPolls(posts, viewerID)
	s.enrichPhotos(posts)
	writeJSON(w, 200, map[string]any{"posts": posts})
}

func (s *Service) GetWallQueue(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
		       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance, p.remote_post_id,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning,
		       p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::timestamptz,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved'),
		       p.page_id, pg.slug, pg.display_name, pg.avatar_url,
		       p.video_url, p.video_thumb_url
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
		LEFT JOIN pages pg ON pg.id = p.page_id
		WHERE p.wall_user_id = $1 AND p.wall_status = 'pending' AND p.deleted_at IS NULL
		ORDER BY p.created_at ASC
	`, userID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	s.enrichPhotos(posts)
	writeJSON(w, 200, map[string]any{"posts": posts})
}

func (s *Service) WallApprove(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var wallUserID string
	s.db.QueryRow(`SELECT wall_user_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, postID).Scan(&wallUserID)
	if wallUserID != userID {
		writeError(w, 403, "forbidden"); return
	}
	s.db.Exec(`UPDATE posts SET wall_status = 'approved' WHERE id = $1`, postID)

	// Notify the author it was approved
	var authorID string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1`, postID).Scan(&authorID)
	if authorID != "" && authorID != userID {
		go s.notif.Create(authorID, userID, "wall_post_approved", postID, "")
	}
	writeJSON(w, 200, map[string]string{"message": "approved"})
}

func (s *Service) WallReject(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var wallUserID string
	s.db.QueryRow(`SELECT wall_user_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, postID).Scan(&wallUserID)
	if wallUserID != userID {
		writeError(w, 403, "forbidden"); return
	}
	// Hard delete rejected posts
	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, postID)
	writeJSON(w, 200, map[string]string{"message": "rejected"})
}

// enrichPolls loads poll options and vote counts for a slice of posts.
func (s *Service) enrichPolls(posts []Post, userID string) {
	if len(posts) == 0 {
		return
	}
	ids := make([]string, len(posts))
	idxMap := map[string]int{}
	for i, p := range posts {
		ids[i] = p.ID
		idxMap[p.ID] = i
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	// Fetch poll settings (multiple_choice, allows_new_options, expires_at)
	settingsRows, err := s.db.Query(
		fmt.Sprintf(`
			SELECT id, poll_multiple_choice, poll_allows_new_options,
			       poll_expires_at,
			       (poll_expires_at IS NOT NULL AND poll_expires_at < NOW()) AS is_expired
			FROM posts
			WHERE id IN (%s) AND EXISTS (SELECT 1 FROM poll_options WHERE post_id = posts.id)
		`, inClause),
		args...,
	)
	if err == nil {
		defer settingsRows.Close()
		for settingsRows.Next() {
			var postID string
			var multiChoice, allowsNew, isExpired bool
			var expiresAt *string
			settingsRows.Scan(&postID, &multiChoice, &allowsNew, &expiresAt, &isExpired)
			if idx, ok := idxMap[postID]; ok {
				posts[idx].PollMultipleChoice = multiChoice
				posts[idx].PollAllowsNewOptions = allowsNew
				posts[idx].PollExpiresAt = expiresAt
				if isExpired {
					posts[idx].PollExpired = true
				}
			}
		}
	}

	// Fetch options with vote counts
	rows, err := s.db.Query(
		fmt.Sprintf(`
			SELECT po.id, po.post_id, po.text, po.position,
			       -- AGORA-210: remote_votes carries an inbound AP poll's own
			       -- vote tally (nobody local necessarily voted, so there's
			       -- nothing for COUNT(poll_votes) to find); always 0 for a
			       -- locally-created poll.
			       (SELECT COUNT(*) FROM poll_votes pv WHERE pv.option_id = po.id) + po.remote_votes AS votes
			FROM poll_options po
			WHERE po.post_id IN (%s)
			ORDER BY po.post_id, po.position
		`, inClause),
		args...,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var opt PollOption
			var postID string
			rows.Scan(&opt.ID, &postID, &opt.Text, &opt.Position, &opt.Votes)
			if idx, ok := idxMap[postID]; ok {
				posts[idx].PollOptions = append(posts[idx].PollOptions, opt)
			}
		}
	}

	// Fetch the current user's vote(s) per poll
	if userID != "" {
		uph := make([]string, len(ids))
		uargs := make([]any, len(ids)+1)
		uargs[0] = userID
		for i, id := range ids {
			uph[i] = fmt.Sprintf("$%d", i+2)
			uargs[i+1] = id
		}
		vrows, err := s.db.Query(
			fmt.Sprintf(`
				SELECT po.post_id, pv.option_id
				FROM poll_votes pv
				JOIN poll_options po ON po.id = pv.option_id
				WHERE pv.user_id = $1 AND po.post_id IN (%s)
			`, strings.Join(uph, ",")),
			uargs...,
		)
		if err == nil {
			defer vrows.Close()
			for vrows.Next() {
				var postID, optionID string
				vrows.Scan(&postID, &optionID)
				if idx, ok := idxMap[postID]; ok {
					// Support multiple votes for multiple-choice polls
					if posts[idx].MyPollVote == "" {
						posts[idx].MyPollVote = optionID
					} else {
						posts[idx].MyPollVotes = append(posts[idx].MyPollVotes, optionID)
					}
				}
			}
		}
	}
}

// enrichPhotos batch-loads post_photos rows for a slice of posts (AGORA-93).
// Posts with only one photo use image_url directly; photos are only stored in
// post_photos when a post has ≥2 images.
func (s *Service) enrichPhotos(posts []Post) {
	if len(posts) == 0 {
		return
	}
	ids := make([]string, len(posts))
	idxMap := map[string]int{}
	for i, p := range posts {
		ids[i] = p.ID
		idxMap[p.ID] = i
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := s.db.Query(
		fmt.Sprintf(`SELECT post_id, url FROM post_photos WHERE post_id IN (%s) ORDER BY post_id, position ASC`,
			strings.Join(placeholders, ",")),
		args...,
	)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var postID, url string
		rows.Scan(&postID, &url)
		if idx, ok := idxMap[postID]; ok {
			posts[idx].PhotoURLs = append(posts[idx].PhotoURLs, url)
		}
	}
	// For posts with photos in the table, the image_url holds the first photo
	// (set on insert). Ensure photo_urls always includes it as the first entry
	// when the table has entries.
	for i := range posts {
		if len(posts[i].PhotoURLs) > 0 && posts[i].ImageURL != "" {
			// photo_urls from the table already has position 0 = ImageURL
		} else if posts[i].ImageURL != "" {
			// single-image post: expose via photo_urls for uniform frontend access
			posts[i].PhotoURLs = []string{posts[i].ImageURL}
		}
	}
}

type ReactionUser struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Type        string `json:"type"`
}

func (s *Service) GetReactions(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")

	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.display_name, COALESCE(u.avatar_url,''), r.reaction_type
		FROM reactions r
		JOIN users u ON u.id = r.user_id
		WHERE r.post_id = $1 AND u.deletion_scheduled_at IS NULL
		ORDER BY r.created_at ASC
	`, postID)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	var reactions []ReactionUser
	counts := map[string]int{}
	for rows.Next() {
		var ru ReactionUser
		rows.Scan(&ru.UserID, &ru.Username, &ru.DisplayName, &ru.AvatarURL, &ru.Type)
		reactions = append(reactions, ru)
		counts[ru.Type]++
	}
	if reactions == nil {
		reactions = []ReactionUser{}
	}
	writeJSON(w, 200, map[string]any{
		"reactions": reactions,
		"counts":    counts,
		"total":     len(reactions),
	})
}

// ── Reposts ───────────────────────────────────────────────────────────────────

func (s *Service) Repost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	repostOfID := chi.URLParam(r, "id")

	var req struct {
		Content        string `json:"content"`
		Visibility     string `json:"visibility"`
		GroupID        string `json:"group_id"`
		ContentWarning string `json:"content_warning"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Visibility == "" {
		req.Visibility = "friends"
	}

	// Check the original post exists and that the sharer can see it
	var origAuthorID, origVisibility string
	var origGroupID *string
	err := s.db.QueryRow(`
		SELECT author_id, visibility, community_group_id
		FROM posts WHERE id = $1 AND deleted_at IS NULL AND repost_of_id IS NULL
	`, repostOfID).Scan(&origAuthorID, &origVisibility, &origGroupID)
	if err != nil {
		writeError(w, 404, "post not found or cannot be shared"); return
	}

	// Only public posts can be shared (friends-only posts stay within their intended audience)
	if origVisibility == "private" || origVisibility == "friends" {
		writeError(w, 403, "this post cannot be shared — it is only visible to the author's friends"); return
	}

	// Group posts can only be shared within the group context — block sharing to general feed
	if origGroupID != nil {
		writeError(w, 403, "group posts cannot be shared outside the group"); return
	}

	// AGORA-226: a share's own audience is independent of the original
	// post's — the sharer picks who sees *their* commentary, mirroring
	// CreatePost's own visibility/group_id handling.
	var friendGroupID *string
	if req.GroupID != "" && req.Visibility == "group" {
		friendGroupID = &req.GroupID
	}

	var id string
	err = s.db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, group_id, content_warning, repost_of_id)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, userID, req.Content, req.Visibility, friendGroupID, req.ContentWarning, repostOfID).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not share post")
		return
	}

	if origAuthorID != "" && origAuthorID != userID {
		go s.notif.Create(origAuthorID, userID, "post_repost", repostOfID, "")
	}

	// AGORA-159: federate as an Announce if the original post is remote.
	if s.fed != nil {
		go s.fed.DeliverAnnounce(userID, id, repostOfID)
	}

	writeJSON(w, 201, map[string]string{"id": id})
}

// ── Comments ──────────────────────────────────────────────────────────────────

func (s *Service) GetComments(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	viewerID := auth.UserIDFromCtx(r.Context())
	// viewerID feeds a uuid column below; an empty string (guest) is invalid
	// input for the uuid type, so use NULL instead.
	var viewerParam any = viewerID
	if viewerID == "" {
		viewerParam = nil
	}

	type Comment struct {
		ID             string         `json:"id"`
		AuthorID       string         `json:"author_id"`
		Username       string         `json:"username"`
		DisplayName    string         `json:"display_name"`
		Pronouns       string         `json:"pronouns"`
		AvatarURL      string         `json:"avatar_url"`
		Content        string         `json:"content"`
		ImageURL       string         `json:"image_url"`
		CreatedAt      string         `json:"created_at"`
		EditedAt       *string        `json:"edited_at,omitempty"`
		ReactionCount  int            `json:"reaction_count"`
		MyReaction     string         `json:"my_reaction"`
		ReactionCounts map[string]int `json:"reaction_counts"`
		Replies        []Comment      `json:"replies"`
	}

	scanComment := func(rows interface {
		Scan(...any) error
	}) Comment {
		var c Comment
		var myReaction *string
		rows.Scan(&c.ID, &c.AuthorID, &c.Username, &c.DisplayName, &c.Pronouns, &c.AvatarURL,
			&c.Content, &c.ImageURL, &c.CreatedAt, &c.EditedAt, &c.ReactionCount, &myReaction)
		if myReaction != nil {
			c.MyReaction = *myReaction
		}
		c.ReactionCount = 0 // bulk enrichment below sets this; zero here to avoid double-count
		c.ReactionCounts = map[string]int{}
		c.Replies = []Comment{}
		return c
	}

	commentSQL := `
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.created_at, p.edited_at,
		       (SELECT COUNT(*) FROM reactions WHERE post_id = p.id) AS reaction_count,
		       (SELECT reaction_type FROM reactions WHERE post_id = p.id AND user_id = $1 LIMIT 1) AS my_reaction
		FROM posts p
		JOIN users u ON u.id = p.author_id
		WHERE p.parent_id = $2 AND p.deleted_at IS NULL AND u.deletion_scheduled_at IS NULL
		ORDER BY p.created_at ASC
	`

	// Top-level comments
	rows, err := s.db.Query(commentSQL, viewerParam, postID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		comments = append(comments, scanComment(rows))
	}
	rows.Close()

	if comments == nil {
		comments = []Comment{}
	}

	// Replies for each top-level comment, and depth-2 replies for each depth-1 reply
	for i, c := range comments {
		rrows, err := s.db.Query(commentSQL, viewerParam, c.ID)
		if err != nil { continue }
		for rrows.Next() {
			reply := scanComment(rrows)
			// Fetch depth-2 replies for this depth-1 reply
			rrrows, err2 := s.db.Query(commentSQL, viewerParam, reply.ID)
			if err2 == nil {
				for rrrows.Next() {
					reply.Replies = append(reply.Replies, scanComment(rrrows))
				}
				rrrows.Close()
			}
			if reply.Replies == nil {
				reply.Replies = []Comment{}
			}
			comments[i].Replies = append(comments[i].Replies, reply)
		}
		rrows.Close()
		if comments[i].Replies == nil {
			comments[i].Replies = []Comment{}
		}
	}

	// Bulk-enrich reaction_counts for all comments + replies (flatten, query, redistribute)
	{
		type flatEntry struct {
			topIdx   int
			replyIdx int
			r2Idx    int
			id       string
		}
		var flat []flatEntry
		for i, c := range comments {
			flat = append(flat, flatEntry{i, -1, -1, c.ID})
			for j, r := range c.Replies {
				flat = append(flat, flatEntry{i, j, -1, r.ID})
				for k, r2 := range r.Replies {
					flat = append(flat, flatEntry{i, j, k, r2.ID})
				}
			}
		}
		if len(flat) > 0 {
			ids := make([]string, len(flat))
			idxMap := map[string][]flatEntry{}
			for n, e := range flat {
				ids[n] = e.id
				idxMap[e.id] = append(idxMap[e.id], e)
			}
			placeholders := make([]string, len(ids))
			args := make([]any, len(ids))
			for n, id := range ids {
				placeholders[n] = fmt.Sprintf("$%d", n+1)
				args[n] = id
			}
			inClause := strings.Join(placeholders, ",")
			rcRows, err := s.db.Query(
				fmt.Sprintf(`SELECT post_id, reaction_type, COUNT(*) FROM reactions WHERE post_id IN (%s) GROUP BY post_id, reaction_type`, inClause),
				args...,
			)
			if err == nil {
				defer rcRows.Close()
				for rcRows.Next() {
					var pid, rtype string
					var cnt int
					rcRows.Scan(&pid, &rtype, &cnt)
					for _, e := range idxMap[pid] {
						if e.replyIdx == -1 {
							comments[e.topIdx].ReactionCounts[rtype] = cnt
							comments[e.topIdx].ReactionCount += cnt
						} else if e.r2Idx == -1 {
							comments[e.topIdx].Replies[e.replyIdx].ReactionCounts[rtype] = cnt
							comments[e.topIdx].Replies[e.replyIdx].ReactionCount += cnt
						} else {
							comments[e.topIdx].Replies[e.replyIdx].Replies[e.r2Idx].ReactionCounts[rtype] = cnt
							comments[e.topIdx].Replies[e.replyIdx].Replies[e.r2Idx].ReactionCount += cnt
						}
					}
				}
			}
		}
	}

	writeJSON(w, 200, map[string]any{"comments": comments})
}

func (s *Service) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")

	var req struct {
		Content   string `json:"content"`
		ImageURL  string `json:"image_url"`
		ReplyToID string `json:"reply_to_id"` // if set, this is a reply to a comment
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Content == "" && req.ImageURL == "") {
		writeError(w, 400, "content required")
		return
	}

	// Validate the post exists and get its author/visibility
	var visibility, postAuthorID string
	s.db.QueryRow(`SELECT visibility, author_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, postID).
		Scan(&visibility, &postAuthorID)
	if postAuthorID == "" {
		writeError(w, 404, "post not found")
		return
	}

	// Determine parent: either a reply to a comment, or a top-level comment on the post
	parentID := postID
	var replyToAuthorID string
	if req.ReplyToID != "" {
		var commentParentID string
		s.db.QueryRow(`SELECT parent_id, author_id FROM posts WHERE id = $1 AND deleted_at IS NULL`,
			req.ReplyToID).Scan(&commentParentID, &replyToAuthorID)
		if commentParentID == "" {
			writeError(w, 404, "comment not found")
			return
		}
		if commentParentID == postID {
			// Replying to a depth-0 comment — fine
			parentID = req.ReplyToID
		} else {
			// Replying to a depth-1 comment — check its parent is the post (depth-1 check)
			var grandParentID string
			s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1 AND deleted_at IS NULL`,
				commentParentID).Scan(&grandParentID)
			if grandParentID != postID {
				writeError(w, 400, "maximum reply depth reached")
				return
			}
			parentID = req.ReplyToID
		}
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO posts (author_id, content, image_url, visibility, parent_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, userID, req.Content, req.ImageURL, visibility, parentID).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not post comment")
		return
	}

	// Notify post author (if not self)
	if postAuthorID != userID {
		go s.notif.Create(postAuthorID, userID, "post_comment", postID, "")
	}
	// Notify comment author when someone replies to their comment (if different from post author and self)
	if replyToAuthorID != "" && replyToAuthorID != userID && replyToAuthorID != postAuthorID {
		go s.notif.Create(replyToAuthorID, userID, "comment_reply", postID, "")
	}
	go s.notifyMentions(req.Content, userID, postID)

	// AGORA-147/AGORA-146: deliver to the fediverse if the immediate parent
	// — either a specific remote comment (req.ReplyToID) or, just as often,
	// a remote account's own top-level post pulled in via a fediverse
	// custom feed — is itself remote-authored. DeliverReply already checks
	// remoteness generically for whatever post/comment id it's given, so
	// this doesn't need to special-case which kind of parent it is.
	if s.fed != nil {
		go s.fed.DeliverReply(userID, id, parentID)
	}

	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Service) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	role := auth.RoleFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	commentID := chi.URLParam(r, "commentID")

	var commentAuthor, parentAuthor string
	// Match direct children of the post (depth-0 comments) or children of
	// those (depth-1 replies-to-a-reply) — the full 2-level depth Agora
	// supports. Previously only depth-0 matched, so a reply-to-a-reply could
	// never be deleted through this endpoint at all.
	s.db.QueryRow(`
		SELECT author_id FROM posts
		WHERE id = $1 AND deleted_at IS NULL
		  AND (parent_id = $2 OR parent_id IN (SELECT id FROM posts WHERE parent_id = $2))
	`, commentID, postID).Scan(&commentAuthor)
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1`, postID).Scan(&parentAuthor)

	if commentAuthor == "" {
		writeError(w, 404, "comment not found")
		return
	}
	if commentAuthor != userID && parentAuthor != userID && role != "admin" && role != "moderator" {
		writeError(w, 403, "forbidden")
		return
	}

	s.db.Exec(`UPDATE posts SET deleted_at = NOW() WHERE id = $1`, commentID)

	// AGORA-151: notify fediverse participants a federated reply was removed
	if s.fed != nil {
		go s.fed.BroadcastDeletePost(commentAuthor, commentID)
	}

	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

// ── Edit ──────────────────────────────────────────────────────────────────────

func (s *Service) EditPost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var authorID string
	var repostOfID *string
	var pageID *string
	s.db.QueryRow(`SELECT author_id, repost_of_id, page_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&authorID, &repostOfID, &pageID)
	if authorID == "" {
		writeError(w, 404, "post not found"); return
	}
	if authorID != userID {
		writeError(w, 403, "forbidden"); return
	}
	if repostOfID != nil {
		writeError(w, 400, "reposts cannot be edited"); return
	}

	var req struct {
		Content        *string  `json:"content"`
		ImageURL       *string  `json:"image_url"`
		ImageURLs      []string `json:"image_urls"`
		Visibility     *string  `json:"visibility"`
		FriendListID   *string  `json:"friend_list_id"`
		ContentWarning *string  `json:"content_warning"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}
	if req.Content == nil && req.ImageURL == nil && len(req.ImageURLs) == 0 && req.Visibility == nil && req.ContentWarning == nil {
		writeError(w, 400, "nothing to update"); return
	}
	// Normalize image_urls → image_url
	if len(req.ImageURLs) > 0 {
		if len(req.ImageURLs) > 4 {
			req.ImageURLs = req.ImageURLs[:4]
		}
		first := req.ImageURLs[0]
		req.ImageURL = &first
	}

	// Validate visibility value if provided
	if req.Visibility != nil {
		v := *req.Visibility
		if v != "public" && v != "friends" && v != "group" && v != "private" {
			writeError(w, 400, "invalid visibility — must be public, friends, group, or private"); return
		}
		// Don't allow changing visibility of community group posts
		var communityGroupID *string
		s.db.QueryRow(`SELECT community_group_id FROM posts WHERE id = $1`, id).Scan(&communityGroupID)
		if communityGroupID != nil {
			writeError(w, 400, "visibility of group posts cannot be changed"); return
		}
		// When switching to group visibility, friend_list_id is required
		if v == "group" && (req.FriendListID == nil || *req.FriendListID == "") {
			writeError(w, 400, "friend_list_id required when visibility is group"); return
		}
	}

	var sets []string
	var args []any
	i := 1
	if req.Content != nil {
		sets = append(sets, fmt.Sprintf("content = $%d", i)); args = append(args, *req.Content); i++
	}
	if req.ImageURL != nil {
		sets = append(sets, fmt.Sprintf("image_url = $%d", i)); args = append(args, *req.ImageURL); i++
	}
	if req.Visibility != nil {
		sets = append(sets, fmt.Sprintf("visibility = $%d", i)); args = append(args, *req.Visibility); i++
		// Update group_id alongside visibility
		if *req.Visibility == "group" && req.FriendListID != nil {
			sets = append(sets, fmt.Sprintf("group_id = $%d", i)); args = append(args, *req.FriendListID); i++
		} else if *req.Visibility != "group" {
			// Clear friend list when switching away from group visibility
			sets = append(sets, "group_id = NULL")
		}
	}
	if req.ContentWarning != nil {
		sets = append(sets, fmt.Sprintf("content_warning = $%d", i)); args = append(args, *req.ContentWarning); i++
	}
	sets = append(sets, "edited_at = NOW()")
	args = append(args, id)
	s.db.Exec(fmt.Sprintf("UPDATE posts SET %s WHERE id = $%d", strings.Join(sets, ", "), i), args...)

	// Replace photos if image_urls was provided
	if len(req.ImageURLs) > 0 {
		s.db.Exec(`DELETE FROM post_photos WHERE post_id = $1`, id)
		if len(req.ImageURLs) > 1 {
			for pos, u := range req.ImageURLs {
				s.db.Exec(`INSERT INTO post_photos (post_id, url, position) VALUES ($1, $2, $3)`, id, u, pos)
			}
		}
	}

	// AGORA-150: notify fediverse followers a federated post was edited
	if s.fed != nil {
		if pageID != nil {
			// AGORA-115: a page post federates under the page's own actor.
			go s.fed.BroadcastPagePostUpdate(*pageID, id)
		} else {
			go s.fed.BroadcastUpdatePost(userID, id)
		}
	}

	writeJSON(w, 200, map[string]string{"message": "updated"})
}

func (s *Service) EditComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	commentID := chi.URLParam(r, "commentID")

	var authorID, parentID string
	// Same fix as DeleteComment: match depth-0 comments or depth-1 replies-to-
	// a-reply, not just direct children of the post.
	s.db.QueryRow(`
		SELECT author_id, parent_id FROM posts
		WHERE id = $1 AND deleted_at IS NULL
		  AND (parent_id = $2 OR parent_id IN (SELECT id FROM posts WHERE parent_id = $2))
	`, commentID, postID).Scan(&authorID, &parentID)
	if authorID == "" {
		writeError(w, 404, "comment not found"); return
	}
	if authorID != userID {
		writeError(w, 403, "forbidden"); return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		writeError(w, 400, "content required"); return
	}

	s.db.Exec(`UPDATE posts SET content = $1, edited_at = NOW() WHERE id = $2`, req.Content, commentID)

	// AGORA-162: mirror EditPost's federation hook — a previously-federated
	// reply must send an Update, not go stale on the remote side forever.
	if s.fed != nil {
		go s.fed.DeliverReplyUpdate(userID, commentID, parentID)
	}

	writeJSON(w, 200, map[string]string{"message": "updated"})
}

// ── Scan helpers ──────────────────────────────────────────────────────────────

type Post struct {
	ID             string  `json:"id"`
	AuthorID       string  `json:"author_id"`
	AuthorUsername string  `json:"author_username"`
	AuthorName     string  `json:"author_display_name"`
	AuthorPronouns string  `json:"author_pronouns"`
	AuthorAvatar   string  `json:"author_avatar_url"`
	Content        string  `json:"content"`
	ImageURL       string  `json:"image_url"`
	Visibility     string  `json:"visibility"`
	GroupID        *string `json:"group_id"`         // community group id
	FriendListID   *string `json:"friend_list_id"`   // friend list id (visibility=group)
	GroupName      *string `json:"group_name,omitempty"`
	GroupSlug      *string `json:"group_slug,omitempty"`
	RepostOfID     *string `json:"repost_of_id"`
	IsRemote       bool    `json:"is_remote"`
	RemoteInstance string  `json:"remote_instance,omitempty"`
	RemotePostID   string  `json:"remote_url,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	EditedAt       *string `json:"edited_at,omitempty"`
	ContentWarning string  `json:"content_warning"`
	LinkURL        string  `json:"link_url"`
	LinkTitle      string  `json:"link_title"`
	LinkDescription string `json:"link_description"`
	LinkImage      string  `json:"link_image"`
	LinkDomain     string  `json:"link_domain"`
	LikeCount      int     `json:"like_count"`
	CommentCount   int     `json:"comment_count"`
	RepostCount    int     `json:"repost_count"`
	Liked          bool    `json:"liked"`
	Reposted       bool    `json:"reposted"`
	// Reactions (AGORA-25)
	ReactionCount  int     `json:"reaction_count"`
	MyReaction     string  `json:"my_reaction"` // empty string = no reaction
	ReactionCounts map[string]int `json:"reaction_counts"`
	// Polls (AGORA-5)
	PollOptions          []PollOption `json:"poll_options,omitempty"`
	MyPollVote           string       `json:"my_poll_vote,omitempty"`
	MyPollVotes          []string     `json:"my_poll_votes,omitempty"`
	PollExpiresAt        *string      `json:"poll_expires_at,omitempty"`
	PollMultipleChoice   bool         `json:"poll_multiple_choice,omitempty"`
	PollAllowsNewOptions bool         `json:"poll_allows_new_options,omitempty"`
	PollExpired          bool         `json:"poll_expired,omitempty"`
	// Wall (AGORA-19)
	WallUserID       *string `json:"wall_user_id,omitempty"`
	WallUsername     *string `json:"wall_username,omitempty"`
	WallDisplayName  *string `json:"wall_display_name,omitempty"`
	WallStatus       string  `json:"wall_status,omitempty"`
	// Multi-photo (AGORA-93)
	PhotoURLs []string `json:"photo_urls,omitempty"`
	// Video (AGORA-119)
	VideoURL      string `json:"video_url,omitempty"`
	VideoThumbURL string `json:"video_thumb_url,omitempty"`
	// Page attribution (AGORA-109)
	PageID     *string `json:"page_id,omitempty"`
	PageSlug   *string `json:"page_slug,omitempty"`
	PageName   *string `json:"page_name,omitempty"`
	PageAvatar *string `json:"page_avatar_url,omitempty"`
	// Repost source
	RepostAuthorUsername *string `json:"repost_author_username,omitempty"`
	RepostAuthorName     *string `json:"repost_author_display_name,omitempty"`
	RepostAuthorPronouns *string `json:"repost_author_pronouns,omitempty"`
	RepostAuthorAvatar   *string `json:"repost_author_avatar_url,omitempty"`
	RepostContent        *string `json:"repost_content,omitempty"`
	RepostImageURL       *string `json:"repost_image_url,omitempty"`
	RepostCreatedAt      *string `json:"repost_created_at,omitempty"`
}

func scanPosts(rows interface {
	Next() bool
	Scan(...any) error
}) []Post {
	var posts []Post
	for rows.Next() {
		var p Post
		rows.Scan(
			&p.ID, &p.AuthorID, &p.AuthorUsername, &p.AuthorName, &p.AuthorPronouns, &p.AuthorAvatar,
			&p.Content, &p.ImageURL, &p.Visibility, &p.GroupID, &p.FriendListID, &p.GroupName, &p.GroupSlug,
			&p.RepostOfID, &p.IsRemote, &p.RemoteInstance, &p.RemotePostID,
			&p.CreatedAt, &p.UpdatedAt, &p.EditedAt, &p.ContentWarning,
			&p.LinkURL, &p.LinkTitle, &p.LinkDescription, &p.LinkImage, &p.LinkDomain,
			&p.LikeCount, &p.CommentCount, &p.RepostCount,
			&p.Liked, &p.Reposted,
			&p.RepostAuthorUsername, &p.RepostAuthorName, &p.RepostAuthorPronouns, &p.RepostAuthorAvatar,
			&p.RepostContent, &p.RepostImageURL, &p.RepostCreatedAt,
			&p.WallUserID, &p.WallUsername, &p.WallDisplayName, &p.WallStatus,
			&p.PageID, &p.PageSlug, &p.PageName, &p.PageAvatar,
			&p.VideoURL, &p.VideoThumbURL,
		)
		p.ReactionCounts = map[string]int{}
		posts = append(posts, p)
	}
	if posts == nil { return []Post{} }
	return posts
}

// enrichReactions loads reaction counts and the current user's reaction for a slice of posts.
// Legacy likes (from the likes table) are treated as 'like' reactions for backwards compatibility.
func (s *Service) enrichReactions(posts []Post, userID string) {
	if len(posts) == 0 {
		return
	}
	// Build post ID list
	ids := make([]string, len(posts))
	idxMap := map[string]int{}
	for i, p := range posts {
		ids[i] = p.ID
		idxMap[p.ID] = i
		if posts[i].ReactionCounts == nil {
			posts[i].ReactionCounts = map[string]int{}
		}
	}

	// Build $1,$2,... placeholders
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	// Aggregate reaction counts — reactions table UNION legacy likes table (as 'like')
	// Build three separate placeholder sets with correct $N numbering
	n := len(ids)
	ph1 := make([]string, n)
	ph2 := make([]string, n)
	ph3 := make([]string, n)
	for i := range ids {
		ph1[i] = fmt.Sprintf("$%d", i+1)
		ph2[i] = fmt.Sprintf("$%d", i+1+n)
		ph3[i] = fmt.Sprintf("$%d", i+1+n+n)
	}
	tripleArgs := append(append(append([]any{}, args...), args...), args...)
	rows, err := s.db.Query(
		fmt.Sprintf(`
			SELECT post_id, reaction_type, COUNT(*)
			FROM reactions
			WHERE post_id IN (%s)
			GROUP BY post_id, reaction_type
			UNION ALL
			SELECT post_id, 'like', COUNT(*)
			FROM likes
			WHERE post_id IN (%s)
			  AND post_id NOT IN (SELECT post_id FROM reactions WHERE post_id IN (%s))
			GROUP BY post_id
		`, strings.Join(ph1, ","), strings.Join(ph2, ","), strings.Join(ph3, ",")),
		tripleArgs...,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var postID, rtype string
			var cnt int
			rows.Scan(&postID, &rtype, &cnt)
			if idx, ok := idxMap[postID]; ok {
				posts[idx].ReactionCounts[rtype] = cnt
				posts[idx].ReactionCount += cnt
			}
		}
	}

	// Current user's reaction — check reactions first, fall back to likes
	if userID != "" {
		uph := make([]string, n)
		uargs := make([]any, n+1)
		uargs[0] = userID
		for i, id := range ids {
			uph[i] = fmt.Sprintf("$%d", i+2)
			uargs[i+1] = id
		}
		urows, err := s.db.Query(
			fmt.Sprintf(`SELECT post_id, reaction_type FROM reactions WHERE user_id = $1 AND post_id IN (%s)`, strings.Join(uph, ",")),
			uargs...,
		)
		if err == nil {
			defer urows.Close()
			for urows.Next() {
				var postID, rtype string
				urows.Scan(&postID, &rtype)
				if idx, ok := idxMap[postID]; ok {
					posts[idx].MyReaction = rtype
				}
			}
		}

		// For posts where user has no reactions entry, check legacy likes
		needsLikeCheck := []string{}
		needsLikeIdx := map[string]int{}
		for _, id := range ids {
			if idx, ok := idxMap[id]; ok && posts[idx].MyReaction == "" {
				needsLikeCheck = append(needsLikeCheck, id)
				needsLikeIdx[id] = idx
			}
		}
		if len(needsLikeCheck) > 0 {
			lplaceholders := make([]string, len(needsLikeCheck))
			largs := make([]any, len(needsLikeCheck))
			for i, id := range needsLikeCheck {
				lplaceholders[i] = fmt.Sprintf("$%d", i+2)
				largs[i] = id
			}
			lrows, err := s.db.Query(
				fmt.Sprintf(`SELECT post_id FROM likes WHERE user_id = $1 AND post_id IN (%s)`, strings.Join(lplaceholders, ",")),
				append([]any{userID}, largs...)...,
			)
			if err == nil {
				defer lrows.Close()
				for lrows.Next() {
					var postID string
					lrows.Scan(&postID)
					if idx, ok := needsLikeIdx[postID]; ok {
						posts[idx].MyReaction = "like"
					}
				}
			}
		}
	}
}

func pageParams(r *http.Request) (limit, offset int) {
	limit = 20
	offset = 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

// ── Link preview ──────────────────────────────────────────────────────────────

func (s *Service) GetLinkPreview(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeError(w, 400, "url required")
		return
	}
	preview, err := fetchLinkPreview(rawURL)
	if err != nil {
		writeError(w, 422, err.Error())
		return
	}
	writeJSON(w, 200, preview)
}

type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Domain      string `json:"domain"`
}

// fetchLinkPreview fetches a URL and extracts Open Graph / meta tags.
// Blocks private/loopback IPs to prevent SSRF.
func fetchLinkPreview(rawURL string) (*LinkPreview, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid URL")
	}

	// Expand known short URLs to canonical forms before fetching
	rawURL = expandShortURL(rawURL, parsed)
	parsed, _ = url.ParseRequestURI(rawURL)

	// ── YouTube oEmbed fast path ───────────────────────────────────────────
	// YouTube blocks scrapers but has a public oEmbed API that always works.
	host := strings.ToLower(parsed.Hostname())
	if host == "www.youtube.com" || host == "youtube.com" || host == "youtu.be" {
		if p := youtubePreview(rawURL); p != nil {
			return p, nil
		}
	}

	// ── Tenor fast path ────────────────────────────────────────────────────
	// tenor.com/XYZ.gif share pages → resolve to direct media URL via oEmbed
	if host == "tenor.com" || host == "www.tenor.com" {
		if p := tenorPreview(rawURL); p != nil {
			return p, nil
		}
	}

	// ── Giphy fast path ────────────────────────────────────────────────────
	// giphy.com/gifs/slug → extract direct media URL
	if host == "giphy.com" || host == "www.giphy.com" || host == "media.giphy.com" {
		if p := giphyPreview(rawURL, parsed); p != nil {
			return p, nil
		}
	}

	// Resolve the hostname and check for private IPs (SSRF protection)
	host = parsed.Hostname()
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil, fmt.Errorf("could not resolve host")
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil { continue }
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return nil, fmt.Errorf("URL not allowed")
		}
	}

	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 { return fmt.Errorf("too many redirects") }
			return nil
		},
	}

	req, _ := http.NewRequest("GET", rawURL, nil)
	// Use a realistic browser user agent — many sites block bot UAs
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil { return nil, fmt.Errorf("could not fetch URL") }
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("remote returned %d", resp.StatusCode)
	}

	// Update host to the final redirected URL's host
	finalHost := parsed.Hostname()
	if resp.Request != nil && resp.Request.URL != nil {
		finalHost = resp.Request.URL.Hostname()
	}

	// Only parse HTML responses
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && ct != "" {
		return nil, fmt.Errorf("URL does not point to a web page")
	}

	// Read up to 512KB — enough to get the <head> without downloading the whole page
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil { return nil, fmt.Errorf("could not read response") }
	html := string(body)

	preview := &LinkPreview{
		URL:    rawURL,
		Domain: finalHost,
	}

	// Extract OG tags and fallback meta tags with simple regex
	// og:title → meta name="title" → <title>
	if v := extractMeta(html, "og:title"); v != "" {
		preview.Title = v
	} else if v := extractMeta(html, "twitter:title"); v != "" {
		preview.Title = v
	} else {
		preview.Title = extractTitle(html)
	}

	// og:description → meta name="description"
	if v := extractMeta(html, "og:description"); v != "" {
		preview.Description = v
	} else {
		preview.Description = extractMeta(html, "description")
	}

	// og:image → twitter:image
	if v := extractMeta(html, "og:image"); v != "" {
		preview.Image = resolveURL(rawURL, v)
	} else if v := extractMeta(html, "twitter:image"); v != "" {
		preview.Image = resolveURL(rawURL, v)
	}

	// Truncate long descriptions
	if len(preview.Description) > 300 {
		preview.Description = preview.Description[:297] + "…"
	}
	if len(preview.Title) > 120 {
		preview.Title = preview.Title[:117] + "…"
	}

	return preview, nil
}

var (
	ogPropertyRe = regexp.MustCompile(`(?i)<meta[^>]+property=["']([^"']+)["'][^>]+content=["']([^"']*?)["']`)
	ogContentRe  = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']*?)["'][^>]+property=["']([^"']+)["']`)
	metaNameRe   = regexp.MustCompile(`(?i)<meta[^>]+name=["']([^"']+)["'][^>]+content=["']([^"']*?)["']`)
	metaNameRe2  = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']*?)["'][^>]+name=["']([^"']+)["']`)
	titleRe      = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
)

func extractMeta(html, key string) string {
	lkey := strings.ToLower(key)
	for _, re := range []*regexp.Regexp{ogPropertyRe, ogContentRe} {
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if strings.ToLower(m[1]) == lkey { return htmlUnescape(m[2]) }
			if strings.ToLower(m[2]) == lkey { return htmlUnescape(m[1]) }
		}
	}
	for _, re := range []*regexp.Regexp{metaNameRe, metaNameRe2} {
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if strings.ToLower(m[1]) == lkey { return htmlUnescape(m[2]) }
			if strings.ToLower(m[2]) == lkey { return htmlUnescape(m[1]) }
		}
	}
	return ""
}

func extractTitle(html string) string {
	if m := titleRe.FindStringSubmatch(html); len(m) > 1 {
		return htmlUnescape(strings.TrimSpace(m[1]))
	}
	return ""
}

// expandShortURL converts short/redirect URLs to their canonical long-form equivalents.
func expandShortURL(rawURL string, parsed *url.URL) string {
	host := strings.ToLower(parsed.Hostname())
	if host == "youtu.be" || host == "www.youtube.com" || host == "youtube.com" {
		return cleanYouTubeURL(rawURL)
	}
	return rawURL
}

// youtubePreview uses YouTube's public oEmbed API to get video metadata.
func youtubePreview(rawURL string) *LinkPreview {
	cleanURL := cleanYouTubeURL(rawURL)
	oembedURL := "https://www.youtube.com/oembed?url=" + url.QueryEscape(cleanURL) + "&format=json"

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(oembedURL)
	if err != nil || resp.StatusCode != 200 { return nil }
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var data struct {
		Title        string `json:"title"`
		AuthorName   string `json:"author_name"`
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := json.Unmarshal(body, &data); err != nil || data.Title == "" { return nil }

	return &LinkPreview{
		URL:         rawURL,
		Title:       data.Title,
		Description: "YouTube video by " + data.AuthorName,
		Image:       data.ThumbnailURL,
		Domain:      "www.youtube.com",
	}
}

// resolveGifURL converts Tenor/Giphy share page URLs to direct media GIF URLs.
// Returns "" if the URL is already a direct media URL or not a GIF service.
func resolveGifURL(rawURL string) string {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil { return "" }
	host := strings.ToLower(parsed.Hostname())

	// Only process known GIF share page hosts
	isTenorShare := (host == "tenor.com" || host == "www.tenor.com") && !strings.Contains(parsed.Hostname(), "media")
	isGiphyShare := (host == "giphy.com" || host == "www.giphy.com")

	if isTenorShare {
		if p := tenorPreview(rawURL); p != nil && p.URL != rawURL {
			return p.URL
		}
	}
	if isGiphyShare {
		if p := giphyPreview(rawURL, parsed); p != nil {
			return p.URL
		}
	}
	return ""
}

// tenorPreview resolves a tenor.com share URL to its direct GIF media URL via oEmbed.
func tenorPreview(rawURL string) *LinkPreview {
	oembedURL := "https://tenor.com/oembed?url=" + url.QueryEscape(rawURL) + "&format=json"
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(oembedURL)
	if err != nil || resp.StatusCode != 200 { return nil }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data struct {
		URL          string `json:"url"`
		ThumbnailURL string `json:"thumbnail_url"`
		Title        string `json:"title"`
	}
	if err := json.Unmarshal(body, &data); err != nil { return nil }
	// Prefer the direct GIF URL, fall back to thumbnail
	mediaURL := data.URL
	if mediaURL == "" { mediaURL = data.ThumbnailURL }
	if mediaURL == "" { return nil }
	return &LinkPreview{
		URL:    mediaURL,
		Image:  mediaURL, // Image field used by mobile to get direct GIF URL
		Domain: "tenor.com",
		Title:  data.Title,
	}
}

// giphyPreview extracts a direct media URL from a Giphy share URL.
func giphyPreview(rawURL string, parsed *url.URL) *LinkPreview {
	// Extract GIF ID from paths like /gifs/name-GIPHYID or /media/GIPHYID/...
	path := parsed.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var gifID string
	for _, part := range parts {
		// Giphy IDs are at the end of hyphenated slug or as bare ID
		if idx := strings.LastIndex(part, "-"); idx != -1 {
			gifID = part[idx+1:]
		} else if len(part) > 8 {
			gifID = part
		}
	}
	if gifID == "" { return nil }
	mediaURL := "https://media.giphy.com/media/" + gifID + "/giphy.gif"
	return &LinkPreview{
		URL:    mediaURL,
		Image:  mediaURL,
		Domain: "giphy.com",
		Title:  "GIF",
	}
}

// cleanYouTubeURL strips tracking parameters from YouTube URLs, keeping only v= and list=.
func cleanYouTubeURL(rawURL string) string {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil { return rawURL }

	host := strings.ToLower(parsed.Hostname())

	// youtu.be short links — extract video ID
	if host == "youtu.be" {
		videoID := strings.TrimPrefix(parsed.Path, "/")
		if videoID != "" {
			return "https://www.youtube.com/watch?v=" + videoID
		}
	}

	// youtube.com — keep only v= and list= params
	if host == "www.youtube.com" || host == "youtube.com" {
		q := parsed.Query()
		clean := url.Values{}
		if v := q.Get("v"); v != "" { clean.Set("v", v) }
		if l := q.Get("list"); l != "" { clean.Set("list", l) }
		parsed.RawQuery = clean.Encode()
		return parsed.String()
	}

	return rawURL
}

func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&apos;", "'")
	return s
}

func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil { return ref }
	r, err := url.Parse(ref)
	if err != nil { return ref }
	return b.ResolveReference(r).String()
}

func timeNow() string { return time.Now().UTC().Format(time.RFC3339) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── Group mention search (AGORA-89) ──────────────────────────────────────────

func (s *Service) GroupMentionSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 200, map[string]any{"groups": []any{}}); return
	}
	rows, err := s.db.Query(`
		SELECT slug, name, avatar_url
		FROM community_groups
		WHERE privacy = 'public'
		  AND (slug ILIKE $1 OR name ILIKE $1)
		ORDER BY member_count DESC
		LIMIT 8
	`, "%"+q+"%")
	if err != nil {
		writeJSON(w, 200, map[string]any{"groups": []any{}}); return
	}
	defer rows.Close()
	type GroupHit struct {
		Slug      string `json:"slug"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	var groups []GroupHit
	for rows.Next() {
		var g GroupHit
		rows.Scan(&g.Slug, &g.Name, &g.AvatarURL)
		groups = append(groups, g)
	}
	if groups == nil { groups = []GroupHit{} }
	writeJSON(w, 200, map[string]any{"groups": groups})
}

// ── Mention helpers ───────────────────────────────────────────────────────────

func (s *Service) notifyMentions(content, authorID, postID string) {
	// AGORA-163: a fediverse mention (@handle@instance.tld) must not also be
	// treated as a local mention — @([a-zA-Z0-9_-]+) alone would otherwise
	// match just the "handle" portion and look up a local user by that name,
	// almost always resolving to nobody or, worse, an unrelated local user
	// who happens to share the name. Skip any local match whose start index
	// falls inside a fediverse match's span.
	fediverseSpans := fediverseMentionRe.FindAllStringIndex(content, -1)
	inFediverseSpan := func(idx int) bool {
		for _, sp := range fediverseSpans {
			if idx >= sp[0] && idx < sp[1] {
				return true
			}
		}
		return false
	}

	matches := mentionRe.FindAllStringSubmatchIndex(content, -1)
	seen := map[string]bool{}
	for _, m := range matches {
		if inFediverseSpan(m[0]) {
			continue
		}
		username := strings.ToLower(content[m[2]:m[3]])
		if seen[username] { continue }
		seen[username] = true

		var userID string
		s.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = $1 AND deletion_scheduled_at IS NULL`, username).Scan(&userID)
		if userID == "" || userID == authorID { continue }

		s.notif.Create(userID, authorID, "post_mention", postID, "")
	}
}

// notifyGroupTags parses +group-slug from post content and notifies group
// owners and mods that their group was tagged (AGORA-89).
func (s *Service) notifyGroupTags(content, authorID, postID string) {
	matches := groupTagRe.FindAllStringSubmatch(content, -1)
	seen := map[string]bool{}
	for _, m := range matches {
		slug := strings.ToLower(m[1])
		if seen[slug] { continue }
		seen[slug] = true

		var groupID string
		s.db.QueryRow(`SELECT id FROM community_groups WHERE slug = $1`, slug).Scan(&groupID)
		if groupID == "" { continue }

		// Notify all owners and mods
		rows, err := s.db.Query(`SELECT user_id FROM community_group_members WHERE group_id = $1 AND role IN ('owner','mod')`, groupID)
		if err != nil { continue }
		for rows.Next() {
			var uid string
			rows.Scan(&uid)
			if uid == authorID { continue }
			s.notif.Create(uid, authorID, "group_tag", postID, groupID)
		}
		rows.Close()
	}
}

// notifyPostFollowers fires a notification to everyone who has enabled
// post notifications for this author.
func (s *Service) notifyPostFollowers(authorID, postID string) {
	rows, err := s.db.Query(`
		SELECT follower_id FROM post_notifications
		WHERE followed_id = $1
	`, authorID)
	if err != nil { return }
	defer rows.Close()
	for rows.Next() {
		var followerID string
		rows.Scan(&followerID)
		s.notif.Create(followerID, authorID, "user_post", postID, "")
	}
}
