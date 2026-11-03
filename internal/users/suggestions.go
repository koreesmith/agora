package users

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lib/pq"

	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/federation"
)

// AGORA-423: Find Friends. Discover answers with one section per network so
// no single network can crowd out the others: accounts on this instance
// (ranked by how the viewer is connected to them), accounts on directly
// federated Agora instances, Fediverse accounts, and Bluesky accounts. Each
// suggestion can be dismissed, and a dismissal is remembered per network.
//
// Local ranking signals, strongest first: mutual friends, shared groups,
// recent interaction (reactions and replies in either direction), shared
// hashtags, and recent public activity. A person with none of them still
// appears, ordered last, so a small instance is not left with an empty list.
//
// Profiles are private by default, so a private profile is not a reason to
// hide someone altogether. It is only suggested to a viewer who already has a
// real connection to them (a mutual friend, a shared group, a recent
// interaction), never as a stranger picked off the member list.

const (
	discoverLocalLimit      = 30
	discoverNetworkLimit    = 12
	suggestionKindUser      = "user"
	suggestionKindPeer      = "peer"
	suggestionKindFediverse = "fediverse"
	suggestionKindBluesky   = "bluesky"
)

type DiscoverUser struct {
	ID            string   `json:"id"`
	Username      string   `json:"username"`
	DisplayName   string   `json:"display_name"`
	AvatarURL     string   `json:"avatar_url"`
	Bio           string   `json:"bio"`
	MutualCount   int      `json:"mutual_count"`
	MutualFriends []string `json:"mutual_friends"`
	FriendStatus  string   `json:"friend_status"` // "", "pending", "pending_incoming"
	// Reasons say why this person is suggested, most meaningful first.
	Reasons []string `json:"reasons"`
}

type FediverseSuggestion struct {
	ActorURL    string `json:"actor_url"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Instance    string `json:"instance"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Bio         string `json:"bio"`
	Reason      string `json:"reason"`
}

type BlueskySuggestion struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Bio         string `json:"bio"`
	Reason      string `json:"reason"`
}

// rankedLocalSuggestions returns the viewer's best local suggestions. A
// non-empty excludeID drops one more account (the onboarding inviter, who is
// shown separately).
func (s *Service) rankedLocalSuggestions(userID, excludeID string, limit int) ([]DiscoverUser, error) {
	rows, err := s.db.Query(`
		WITH my_friends AS (
			SELECT CASE WHEN requester_id = $1 THEN addressee_id ELSE requester_id END AS fid
			FROM friendships
			WHERE (requester_id = $1 OR addressee_id = $1) AND status = 'accepted'
		),
		mutual AS (
			SELECT CASE WHEN f.requester_id = mf.fid THEN f.addressee_id ELSE f.requester_id END AS cand,
			       COUNT(*) AS n
			FROM my_friends mf
			JOIN friendships f ON f.status = 'accepted'
			 AND (f.requester_id = mf.fid OR f.addressee_id = mf.fid)
			GROUP BY 1
		),
		shared_groups AS (
			SELECT m2.user_id AS cand, COUNT(*) AS n
			FROM community_group_members m1
			JOIN community_group_members m2 ON m2.group_id = m1.group_id AND m2.user_id <> $1
			WHERE m1.user_id = $1
			GROUP BY 1
		),
		inter AS (
			SELECT cand, SUM(n) AS n FROM (
				SELECT p.author_id AS cand, COUNT(*) AS n
				FROM reactions r JOIN posts p ON p.id = r.post_id
				WHERE r.user_id = $1 AND r.created_at > NOW() - INTERVAL '90 days' GROUP BY 1
				UNION ALL
				SELECT r.user_id, COUNT(*)
				FROM reactions r JOIN posts p ON p.id = r.post_id
				WHERE p.author_id = $1 AND r.created_at > NOW() - INTERVAL '90 days' GROUP BY 1
				UNION ALL
				SELECT p.author_id, COUNT(*)
				FROM posts c JOIN posts p ON p.id = c.parent_id
				WHERE c.author_id = $1 AND c.deleted_at IS NULL AND c.created_at > NOW() - INTERVAL '90 days' GROUP BY 1
				UNION ALL
				SELECT c.author_id, COUNT(*)
				FROM posts c JOIN posts p ON p.id = c.parent_id
				WHERE p.author_id = $1 AND c.deleted_at IS NULL AND c.created_at > NOW() - INTERVAL '90 days' GROUP BY 1
			) x WHERE cand <> $1 GROUP BY cand
		),
		my_tags AS (
			SELECT DISTINCT ph.tag
			FROM post_hashtags ph JOIN posts p ON p.id = ph.post_id
			WHERE p.author_id = $1 AND p.deleted_at IS NULL AND p.created_at > NOW() - INTERVAL '90 days'
		),
		shared_tags AS (
			SELECT p.author_id AS cand, COUNT(DISTINCT ph.tag) AS n
			FROM post_hashtags ph
			JOIN my_tags t ON t.tag = ph.tag
			JOIN posts p ON p.id = ph.post_id
			WHERE p.visibility = 'public' AND p.deleted_at IS NULL AND p.author_id <> $1
			  AND p.created_at > NOW() - INTERVAL '30 days'
			GROUP BY 1
		),
		active AS (
			SELECT author_id AS cand, COUNT(*) AS n
			FROM posts
			WHERE visibility = 'public' AND deleted_at IS NULL AND parent_id IS NULL
			  AND created_at > NOW() - INTERVAL '30 days'
			GROUP BY 1
		)
		SELECT u.id, u.username, u.display_name, u.avatar_url, COALESCE(u.bio, ''),
		       COALESCE(mu.n, 0), COALESCE(sg.n, 0),
		       LEAST(COALESCE(mu.n, 0), 10) * 3
		         + LEAST(COALESCE(sg.n, 0), 3) * 4
		         + LEAST(COALESCE(i.n, 0), 10) * 1.5
		         + LEAST(COALESCE(st.n, 0), 5)
		         + CASE WHEN COALESCE(a.n, 0) > 0 THEN 2 ELSE 0 END AS score,
		       COALESCE((
		           SELECT CASE WHEN fs.requester_id = $1 THEN 'pending' ELSE 'pending_incoming' END
		           FROM friendships fs
		           WHERE ((fs.requester_id = $1 AND fs.addressee_id = u.id)
		               OR (fs.addressee_id = $1 AND fs.requester_id = u.id))
		             AND fs.status = 'pending'
		           LIMIT 1
		       ), '') AS friend_status,
		       COALESCE(i.n, 0) > 0
		FROM users u
		LEFT JOIN mutual mu        ON mu.cand = u.id
		LEFT JOIN shared_groups sg ON sg.cand = u.id
		LEFT JOIN inter i          ON i.cand  = u.id
		LEFT JOIN shared_tags st   ON st.cand = u.id
		LEFT JOIN active a         ON a.cand  = u.id
		WHERE u.id <> $1 AND u.id::text <> $2
		  AND u.is_remote = false AND u.email_verified = true
		  AND u.deletion_scheduled_at IS NULL AND u.is_suspended = false
		  AND (u.profile_private = false
		       OR COALESCE(mu.n, 0) > 0 OR COALESCE(sg.n, 0) > 0 OR COALESCE(i.n, 0) > 0)
		  AND u.waitlist_status <> 'pending'
		  AND NOT EXISTS (
		      SELECT 1 FROM friendships f
		      WHERE ((f.requester_id = $1 AND f.addressee_id = u.id)
		          OR (f.addressee_id = $1 AND f.requester_id = u.id))
		        AND f.status IN ('accepted', 'declined', 'blocked')
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM blocks b
		      WHERE (b.blocker_id = $1 AND b.blocked_id = u.id)
		         OR (b.blocker_id = u.id AND b.blocked_id = $1)
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM suggestion_dismissals d
		      WHERE d.user_id = $1 AND d.kind = 'user' AND d.target = u.id::text
		  )
		ORDER BY score DESC, u.created_at DESC
		LIMIT $3
	`, userID, excludeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []DiscoverUser{}
	var ids []string
	sharedInteraction := map[string]bool{}
	groupCount := map[string]int{}
	for rows.Next() {
		var u DiscoverUser
		var groups int
		var score float64
		var interacted bool
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio,
			&u.MutualCount, &groups, &score, &u.FriendStatus, &interacted); err != nil {
			return nil, err
		}
		u.MutualFriends = []string{}
		u.Reasons = []string{}
		sharedInteraction[u.ID] = interacted
		groupCount[u.ID] = groups
		results = append(results, u)
		ids = append(ids, u.ID)
	}
	if len(results) == 0 {
		return results, nil
	}

	mutualNames := s.namesPerCandidate(`
		SELECT c.id::text, COALESCE(NULLIF(u2.display_name, ''), u2.username)
		FROM unnest($2::uuid[]) AS c(id)
		JOIN friendships f ON f.status = 'accepted' AND (f.requester_id = c.id OR f.addressee_id = c.id)
		JOIN users u2 ON u2.id = CASE WHEN f.requester_id = c.id THEN f.addressee_id ELSE f.requester_id END
		WHERE u2.id IN (
			SELECT CASE WHEN requester_id = $1 THEN addressee_id ELSE requester_id END
			FROM friendships WHERE (requester_id = $1 OR addressee_id = $1) AND status = 'accepted'
		)
		ORDER BY 2
	`, userID, ids)
	groupNames := s.namesPerCandidate(`
		SELECT m2.user_id::text, g.name
		FROM community_group_members m1
		JOIN community_group_members m2 ON m2.group_id = m1.group_id AND m2.user_id = ANY($2::uuid[])
		JOIN community_groups g ON g.id = m1.group_id
		WHERE m1.user_id = $1
		ORDER BY g.member_count DESC
	`, userID, ids)
	tagNames := s.namesPerCandidate(`
		SELECT p.author_id::text, '#' || ph.tag
		FROM post_hashtags ph
		JOIN posts p ON p.id = ph.post_id
		WHERE p.author_id = ANY($2::uuid[]) AND p.visibility = 'public' AND p.deleted_at IS NULL
		  AND p.created_at > NOW() - INTERVAL '30 days'
		  AND ph.tag IN (
		      SELECT ph2.tag FROM post_hashtags ph2 JOIN posts p2 ON p2.id = ph2.post_id
		      WHERE p2.author_id = $1 AND p2.deleted_at IS NULL AND p2.created_at > NOW() - INTERVAL '90 days'
		  )
		GROUP BY p.author_id, ph.tag
		ORDER BY 2
	`, userID, ids)

	for i := range results {
		u := &results[i]
		if names := mutualNames[u.ID]; len(names) > 0 {
			u.MutualFriends = firstN(names, 3)
		}
		if u.MutualCount > 0 {
			u.Reasons = append(u.Reasons, plural(u.MutualCount, "mutual friend")+
				listSuffix(u.MutualFriends, u.MutualCount))
		}
		if n := groupCount[u.ID]; n > 0 {
			u.Reasons = append(u.Reasons, "In "+joinNames(firstN(groupNames[u.ID], 2), n)+" with you")
		}
		if sharedInteraction[u.ID] {
			u.Reasons = append(u.Reasons, "You have interacted recently")
		}
		if tags := tagNames[u.ID]; len(tags) > 0 {
			u.Reasons = append(u.Reasons, "Posts about "+strings.Join(firstN(tags, 2), ", "))
		}
	}
	return results, nil
}

// namesPerCandidate runs a (candidate id, name) query and groups the names by
// candidate, in the order the query returned them.
func (s *Service) namesPerCandidate(query, userID string, ids []string) map[string][]string {
	out := map[string][]string{}
	rows, err := s.db.Query(query, userID, pq.Array(ids))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil && name != "" {
			out[id] = append(out[id], name)
		}
	}
	return out
}

func firstN(list []string, n int) []string {
	if len(list) > n {
		return list[:n]
	}
	return list
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// listSuffix renders ": A, B, C and 2 more" after a count.
func listSuffix(names []string, total int) string {
	if len(names) == 0 {
		return ""
	}
	s := ": " + strings.Join(names, ", ")
	if total > len(names) {
		s += fmt.Sprintf(" and %d more", total-len(names))
	}
	return s
}

// joinNames renders one or two group names, adding a count when the viewer
// shares more groups than are named.
func joinNames(names []string, total int) string {
	if len(names) == 0 {
		return plural(total, "group")
	}
	s := strings.Join(names, " and ")
	if total > len(names) {
		s += fmt.Sprintf(" and %d more", total-len(names))
	}
	return s
}

// fediverseSuggestions offers Fediverse accounts that follow the viewer but
// are not followed back, then accounts several people on this instance
// follow. Both come from rows this instance already holds, so no remote
// request is made.
func (s *Service) fediverseSuggestions(userID string, limit int) []FediverseSuggestion {
	out := []FediverseSuggestion{}
	rows, err := s.db.Query(`
		SELECT u.ap_actor_url, u.id, u.username, u.remote_instance, u.display_name, u.avatar_url, COALESCE(u.bio, ''),
		       CASE WHEN fol.actor_url IS NOT NULL THEN 'Follows you'
		            ELSE 'Followed by ' || pop.n || ' people here' END AS reason
		FROM users u
		LEFT JOIN (
			SELECT follower_actor_url AS actor_url FROM ap_followers WHERE followed_user_id = $1
		) fol ON fol.actor_url = u.ap_actor_url
		LEFT JOIN (
			SELECT followed_actor_url AS actor_url, COUNT(DISTINCT follower_user_id) AS n
			FROM ap_following WHERE follower_user_id <> $1 AND accepted = true
			GROUP BY 1 HAVING COUNT(DISTINCT follower_user_id) >= 2
		) pop ON pop.actor_url = u.ap_actor_url
		WHERE u.is_remote = true AND u.ap_actor_url <> ''
		  AND (fol.actor_url IS NOT NULL OR pop.actor_url IS NOT NULL)
		  AND u.deletion_scheduled_at IS NULL AND u.is_suspended = false
		  AND NOT EXISTS (SELECT 1 FROM ap_following mine
		                  WHERE mine.follower_user_id = $1 AND mine.followed_actor_url = u.ap_actor_url)
		  AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = $1 AND b.blocked_id = u.id)
		  AND NOT EXISTS (SELECT 1 FROM suggestion_dismissals d
		                  WHERE d.user_id = $1 AND d.kind = 'fediverse' AND d.target = u.ap_actor_url)
		ORDER BY (fol.actor_url IS NOT NULL) DESC, COALESCE(pop.n, 0) DESC, u.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var f FediverseSuggestion
		if rows.Scan(&f.ActorURL, &f.UserID, &f.Username, &f.Instance, &f.DisplayName, &f.AvatarURL, &f.Bio, &f.Reason) == nil {
			out = append(out, f)
		}
	}
	return out
}

// peerSuggestions is the federated-Agora section: the peers' own samples,
// minus people the viewer is already connected to and anyone dismissed.
func (s *Service) peerSuggestions(userID string) []federation.PeerUserSuggestion {
	all := federation.SuggestPeerUsers(s.db)

	skip := map[string]bool{}
	if rows, err := s.db.Query(`
		SELECT u.username || '@' || u.remote_instance
		FROM friendships f
		JOIN users u ON u.id = CASE WHEN f.requester_id = $1 THEN f.addressee_id ELSE f.requester_id END
		WHERE (f.requester_id = $1 OR f.addressee_id = $1)
		  AND f.status IN ('accepted', 'pending', 'declined', 'blocked') AND u.is_remote = true
		UNION
		SELECT target FROM suggestion_dismissals WHERE user_id = $1 AND kind = 'peer'
	`, userID); err == nil {
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil {
				skip[strings.ToLower(k)] = true
			}
		}
		rows.Close()
	}

	out := []federation.PeerUserSuggestion{}
	for _, p := range all {
		if skip[strings.ToLower(p.Username+"@"+p.Instance)] {
			continue
		}
		out = append(out, p)
		if len(out) >= discoverNetworkLimit {
			break
		}
	}
	return out
}

func (s *Service) blueskySuggestions(r *http.Request, userID string) []BlueskySuggestion {
	out := []BlueskySuggestion{}
	if s.atproto == nil {
		return out
	}
	// Ask for extra so dismissals can be dropped and the section still fills.
	dids, handles, names, avatars, bios, reasons := s.atproto.SuggestFollows(r.Context(), userID, discoverNetworkLimit*3)
	if len(dids) == 0 {
		return out
	}
	dismissed := map[string]bool{}
	if rows, err := s.db.Query(`SELECT target FROM suggestion_dismissals WHERE user_id = $1 AND kind = 'bluesky'`, userID); err == nil {
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil {
				dismissed[k] = true
			}
		}
		rows.Close()
	}
	for i := range dids {
		if dismissed[dids[i]] {
			continue
		}
		out = append(out, BlueskySuggestion{
			DID: dids[i], Handle: handles[i], DisplayName: names[i],
			AvatarURL: avatars[i], Bio: bios[i], Reason: reasons[i],
		})
		if len(out) >= discoverNetworkLimit {
			break
		}
	}
	return out
}

// Discover serves the Find Friends list.
func (s *Service) Discover(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}

	local, err := s.rankedLocalSuggestions(userID, "", discoverLocalLimit)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}

	writeJSON(w, 200, map[string]any{
		"users":                 local,
		"federated_suggestions": s.peerSuggestions(userID),
		"fediverse_suggestions": s.fediverseSuggestions(userID, discoverNetworkLimit),
		"bluesky_suggestions":   s.blueskySuggestions(r, userID),
	})
}

// DismissSuggestion remembers that the viewer does not want a person offered
// again. key is the network's own identifier for them: a user id, a
// username@instance, an actor URL or a DID.
func (s *Service) DismissSuggestion(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}
	var req struct {
		Kind string `json:"kind"`
		Key  string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" || len(req.Key) > 500 {
		writeError(w, 400, "kind and key required")
		return
	}
	switch req.Kind {
	case suggestionKindUser, suggestionKindPeer, suggestionKindFediverse, suggestionKindBluesky:
	default:
		writeError(w, 400, "unknown kind")
		return
	}
	if _, err := s.db.Exec(`
		INSERT INTO suggestion_dismissals (user_id, kind, target) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, userID, req.Kind, req.Key); err != nil {
		writeError(w, 500, "db error")
		return
	}
	writeJSON(w, 200, map[string]bool{"dismissed": true})
}
