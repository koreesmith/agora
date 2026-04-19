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

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

// fedSender is the subset of federation.Service used here.
type fedSender interface {
	BroadcastToFriendInstances(userID string, activity any)
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
	r.Get("/feed",                               s.GetFeed)
	r.Post("/posts",                             s.CreatePost)
	r.Get("/posts/{id}",                         s.GetPost)
	r.Delete("/posts/{id}",                      s.DeletePost)
	r.Patch("/posts/{id}",                       s.EditPost)
	r.Post("/posts/{id}/like",                   s.LikePost)
	r.Delete("/posts/{id}/like",                 s.UnlikePost)
	r.Post("/posts/{id}/react",                  s.ReactPost)
	r.Delete("/posts/{id}/react",                s.UnreactPost)
	r.Get("/posts/{id}/reactions",               s.GetReactions)
	r.Post("/posts/{id}/repost",                 s.Repost)
	r.Get("/posts/{id}/comments",                s.GetComments)
	r.Post("/posts/{id}/comments",               s.CreateComment)
	r.Delete("/posts/{id}/comments/{commentID}", s.DeleteComment)
	r.Patch("/posts/{id}/comments/{commentID}",  s.EditComment)
	r.Get("/users/{username}/posts",             s.GetUserPosts)
	r.Post("/posts/{id}/poll/vote",              s.PollVote)
	r.Delete("/posts/{id}/poll/vote",            s.PollUnvote)
	r.Post("/posts/{id}/poll/options",           s.PollAddOption)
	r.Get("/users/{username}/wall",              s.GetWall)
	r.Get("/users/me/wall-queue",                s.GetWallQueue)
	r.Post("/posts/{id}/wall-approve",           s.WallApprove)
	r.Post("/posts/{id}/wall-reject",            s.WallReject)
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
			       p.repost_of_id, p.is_remote, p.remote_instance,
			       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
			       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
			       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
			       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
			       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
			       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
			       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
			       rp.content, rp.image_url, rp.created_at,
			       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved')
			FROM posts p
			JOIN users u ON u.id = p.author_id
			LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
			LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
			LEFT JOIN community_groups cg ON cg.id = p.community_group_id
			LEFT JOIN users wu ON wu.id = p.wall_user_id
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
			       p.repost_of_id, p.is_remote, p.remote_instance,
			       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
			       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
			       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
			       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
			       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
			       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
			       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
			       rp.content, rp.image_url, rp.created_at,
			       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved')
			FROM posts p
			JOIN users u ON u.id = p.author_id
			LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
			LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
			LEFT JOIN community_groups cg ON cg.id = p.community_group_id
			LEFT JOIN users wu ON wu.id = p.wall_user_id
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
	writeJSON(w, 200, map[string]any{"posts": posts})
}

func (s *Service) execCustomFeed(w http.ResponseWriter, userID string, limit, offset int, customFeedID string) {
	var feedExists bool
	s.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM custom_feeds WHERE id = $1 AND owner_id = $2)`,
		customFeedID, userID,
	).Scan(&feedExists)
	if !feedExists {
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

	// Inclusion: posts must come from at least one included source (OR across groups/communities)
	if len(friendGroupIDs) > 0 || len(communityGroupIDs) > 0 {
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

	// Post type filter
	if len(postTypes) > 0 {
		var typeParts []string
		for _, pt := range postTypes {
			switch pt {
			case "repost":
				typeParts = append(typeParts, `p.repost_of_id IS NOT NULL`)
			case "media":
				typeParts = append(typeParts, `p.image_url != ''`)
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
		       p.repost_of_id, p.is_remote, p.remote_instance,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes   WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts   WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts   WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
		       rp.content, rp.image_url, rp.created_at,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved')
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN posts  rp   ON rp.id = p.repost_of_id
		LEFT JOIN users  rp_u ON rp_u.id = rp.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
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
	writeJSON(w, 200, map[string]any{"posts": posts})
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

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
			       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::timestamptz,
		       NULL::uuid, NULL::text, NULL::text, 'approved'::text
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		WHERE p.author_id = $2 AND p.parent_id IS NULL AND p.deleted_at IS NULL
		  AND p.wall_user_id IS NULL
		  AND `+visFilter+`
		ORDER BY p.created_at DESC LIMIT $3 OFFSET $4
	`, viewerID, authorID, limit, offset)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	defer rows.Close()

	posts := scanPosts(rows)
	s.enrichReactions(posts, viewerID)
	s.enrichPolls(posts, viewerID)
	writeJSON(w, 200, map[string]any{"posts": posts})
}

// ── Post CRUD ─────────────────────────────────────────────────────────────────

func (s *Service) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Content         string   `json:"content"`
		ImageURL        string   `json:"image_url"`
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

	if req.Content == "" && req.ImageURL == "" && len(pollOpts) == 0 {
		writeError(w, 400, "post must have content or image")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "friends"
	}

	// Resolve Tenor/Giphy share page URLs to direct media URLs
	if req.ImageURL != "" {
		if resolved := resolveGifURL(req.ImageURL); resolved != "" {
			req.ImageURL = resolved
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
		INSERT INTO posts (author_id, content, image_url, visibility, community_group_id, group_id, content_warning,
		                   link_url, link_title, link_description, link_image, link_domain,
		                   wall_user_id, wall_status,
		                   poll_multiple_choice, poll_allows_new_options,
		                   poll_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
		        CASE WHEN $17 > 0 THEN NOW() + ($17 * INTERVAL '1 hour') ELSE NULL END)
		RETURNING id
	`, userID, req.Content, req.ImageURL, req.Visibility, communityGroupID, friendGroupID, req.ContentWarning,
		req.LinkURL, req.LinkTitle, req.LinkDescription, req.LinkImage, req.LinkDomain,
		wallUserID, wallStatus,
		req.PollMultipleChoice, req.PollAllowsNewOptions, req.PollExpiresHours).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not create post")
		return
	}

	// Insert poll options if provided
	for i, opt := range pollOpts {
		s.db.Exec(`INSERT INTO poll_options (post_id, text, position) VALUES ($1, $2, $3)`, id, opt, i)
	}

	go s.notifyMentions(req.Content, userID, id)
	if wallUserID == nil {
		go s.notifyPostFollowers(userID, id)
	} else if wallStatus == "approved" {
		// Notify the wall owner
		s.notif.Create(*wallUserID, userID, "wall_post", id, "")
	} else {
		// Notify wall owner they have a pending post to review
		s.notif.Create(*wallUserID, userID, "wall_post_pending", id, "")
	}

	// Add image to user's Timeline Photos album
	if req.ImageURL != "" && s.albums != nil {
		go s.albums.AddToTimelineAlbum(userID, req.ImageURL)
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

	// Access granted — run the full query
	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
			   cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning, p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes    WHERE post_id = p.id AND user_id = $2) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $2) AS reposted,
		       rp_u.username, rp_u.display_name, rp_u.pronouns, rp_u.avatar_url,
		       rp.content, rp.image_url, rp.created_at,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved')
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN posts rp   ON rp.id = p.repost_of_id
		LEFT JOIN users rp_u ON rp_u.id = rp.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
		WHERE p.id = $1 AND p.deleted_at IS NULL
	`, id, viewerID)
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
	writeJSON(w, 200, map[string]any{"post": posts[0]})
}

func (s *Service) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	role := auth.RoleFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var authorID string
	var wallUserID *string
	s.db.QueryRow(`SELECT author_id, wall_user_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&authorID, &wallUserID)
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

	// Broadcast deletion to federated instances
	if s.fed != nil {
		var username string
		s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
		go s.fed.BroadcastToFriendInstances(userID, map[string]any{
			"type":  "delete_post",
			"actor": username,
			"object": map[string]string{"id": id},
		})
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
	writeJSON(w, 200, map[string]string{"message": "liked"})
}

func (s *Service) UnlikePost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, userID, postID)
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

	writeJSON(w, 200, map[string]string{"message": "reacted", "type": req.Type})
}

func (s *Service) UnreactPost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	s.db.Exec(`DELETE FROM reactions WHERE user_id = $1 AND post_id = $2`, userID, postID)
	s.db.Exec(`DELETE FROM likes WHERE user_id = $1 AND post_id = $2`, userID, postID)
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
		       p.repost_of_id, p.is_remote, p.remote_instance,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning,
		       p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::timestamptz,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved')
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
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
	writeJSON(w, 200, map[string]any{"posts": posts})
}

func (s *Service) GetWallQueue(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())

	rows, err := s.db.Query(`
		SELECT p.id, p.author_id, u.username, u.display_name, u.pronouns, u.avatar_url,
		       p.content, p.image_url, p.visibility, p.community_group_id, p.group_id,
		       cg.name, cg.slug,
		       p.repost_of_id, p.is_remote, p.remote_instance,
		       p.created_at, p.updated_at, p.edited_at, p.content_warning,
		       p.link_url, p.link_title, p.link_description, p.link_image, p.link_domain,
		       (SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS like_count,
		       (SELECT COUNT(*) FROM posts WHERE parent_id = p.id AND deleted_at IS NULL) AS comment_count,
		       (SELECT COUNT(*) FROM posts WHERE repost_of_id = p.id) AS repost_count,
		       EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND user_id = $1) AS liked,
		       EXISTS(SELECT 1 FROM posts rp WHERE rp.repost_of_id = p.id AND rp.author_id = $1) AS reposted,
		       NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::timestamptz,
		       p.wall_user_id, wu.username, wu.display_name, COALESCE(p.wall_status,'approved')
		FROM posts p
		JOIN users u ON u.id = p.author_id
		LEFT JOIN community_groups cg ON cg.id = p.community_group_id
		LEFT JOIN users wu ON wu.id = p.wall_user_id
		WHERE p.wall_user_id = $1 AND p.wall_status = 'pending' AND p.deleted_at IS NULL
		ORDER BY p.created_at ASC
	`, userID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	posts := scanPosts(rows)
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
			       (SELECT COUNT(*) FROM poll_votes pv WHERE pv.option_id = po.id) AS votes
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
		Content    string `json:"content"`
		Visibility string `json:"visibility"`
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

	var id string
	err = s.db.QueryRow(`
		INSERT INTO posts (author_id, content, visibility, repost_of_id)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, userID, req.Content, req.Visibility, repostOfID).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not share post")
		return
	}

	if origAuthorID != "" && origAuthorID != userID {
		go s.notif.Create(origAuthorID, userID, "post_repost", repostOfID, "")
	}

	writeJSON(w, 201, map[string]string{"id": id})
}

// ── Comments ──────────────────────────────────────────────────────────────────

func (s *Service) GetComments(w http.ResponseWriter, r *http.Request) {
	postID := chi.URLParam(r, "id")
	viewerID := auth.UserIDFromCtx(r.Context())

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
	rows, err := s.db.Query(commentSQL, viewerID, postID)
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
		rrows, err := s.db.Query(commentSQL, viewerID, c.ID)
		if err != nil { continue }
		for rrows.Next() {
			reply := scanComment(rrows)
			// Fetch depth-2 replies for this depth-1 reply
			rrrows, err2 := s.db.Query(commentSQL, viewerID, reply.ID)
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

	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Service) DeleteComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	role := auth.RoleFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	commentID := chi.URLParam(r, "commentID")

	var commentAuthor, parentAuthor string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1 AND parent_id = $2 AND deleted_at IS NULL`, commentID, postID).Scan(&commentAuthor)
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
	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

// ── Edit ──────────────────────────────────────────────────────────────────────

func (s *Service) EditPost(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var authorID string
	var repostOfID *string
	s.db.QueryRow(`SELECT author_id, repost_of_id FROM posts WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&authorID, &repostOfID)
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
		Content        *string `json:"content"`
		ImageURL       *string `json:"image_url"`
		Visibility     *string `json:"visibility"`
		FriendListID   *string `json:"friend_list_id"`
		ContentWarning *string `json:"content_warning"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}
	if req.Content == nil && req.ImageURL == nil && req.Visibility == nil && req.ContentWarning == nil {
		writeError(w, 400, "nothing to update"); return
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
	writeJSON(w, 200, map[string]string{"message": "updated"})
}

func (s *Service) EditComment(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	postID := chi.URLParam(r, "id")
	commentID := chi.URLParam(r, "commentID")

	var authorID string
	s.db.QueryRow(`SELECT author_id FROM posts WHERE id = $1 AND parent_id = $2 AND deleted_at IS NULL`,
		commentID, postID).Scan(&authorID)
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
			&p.RepostOfID, &p.IsRemote, &p.RemoteInstance,
			&p.CreatedAt, &p.UpdatedAt, &p.EditedAt, &p.ContentWarning,
			&p.LinkURL, &p.LinkTitle, &p.LinkDescription, &p.LinkImage, &p.LinkDomain,
			&p.LikeCount, &p.CommentCount, &p.RepostCount,
			&p.Liked, &p.Reposted,
			&p.RepostAuthorUsername, &p.RepostAuthorName, &p.RepostAuthorPronouns, &p.RepostAuthorAvatar,
			&p.RepostContent, &p.RepostImageURL, &p.RepostCreatedAt,
			&p.WallUserID, &p.WallUsername, &p.WallDisplayName, &p.WallStatus,
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

// ── Mention helpers ───────────────────────────────────────────────────────────

func (s *Service) notifyMentions(content, authorID, postID string) {
	matches := mentionRe.FindAllStringSubmatch(content, -1)
	seen := map[string]bool{}
	for _, m := range matches {
		username := strings.ToLower(m[1])
		if seen[username] { continue }
		seen[username] = true

		var userID string
		s.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = $1 AND deletion_scheduled_at IS NULL`, username).Scan(&userID)
		if userID == "" || userID == authorID { continue }

		s.notif.Create(userID, authorID, "post_mention", postID, "")
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
