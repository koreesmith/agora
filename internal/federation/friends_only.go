package federation

import (
	"log"
	"time"
)

// Friends-only posts across instances (AGORA-337).
//
// A friends-only post never federated at all: feed.CreatePost only broadcast
// when visibility was "public", so a remote friend was silently treated as
// though they were not a friend. The reported expectation, and the right one,
// is that a friend on another Agora instance is a friend.
//
// This is the friends-only half of AGORA-328. A friend-list post is deliberately
// not handled here, because a list is the author's own private categorization
// and the receiving instance cannot reconstruct it: that case needs per-recipient
// audience storage and is the rest of that ticket. Friends-only is different, and
// simpler, because the audience is derivable at both ends from a relationship
// both ends already record.

// audienceMarker states what kind of limited audience a Note was addressed to.
// Both ends of this exchange are Agora, so the term is read directly in its
// compact form, exactly as the friend-request marker is.
//
// It carries the audience's *kind*, never its membership. "friends" is enough
// for the receiver to enforce correctly, because it already knows which of its
// users are friends with the author. A list would need its membership sent to
// be reconstructable, which ADR-002 rules out, which is why lists are not here.
const audienceMarker = "agora:audience"

// BroadcastFriendsPost delivers a friends-only post to the author's friends on
// other Agora instances, addressed to them by name with no Public anywhere.
func (s *Service) BroadcastFriendsPost(userID, postID string) {
	if !s.activityPubEnabled() {
		return
	}

	var username, visibility, content, contentWarning string
	var apEnabled bool
	var createdAt time.Time
	err := s.db.QueryRow(`
		SELECT u.username, u.activitypub_enabled, p.visibility, p.content, p.content_warning, p.created_at
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, postID, userID).Scan(&username, &apEnabled, &visibility, &content, &contentWarning, &createdAt)
	if err != nil || visibility != "friends" || !apEnabled {
		return
	}

	// profile_private is deliberately not checked, unlike BroadcastPublicPost.
	// That flag governs whether strangers can see a profile, and everyone here
	// is an accepted friend, so it has no bearing on a post addressed to them.

	recipients := s.remoteFriendRecipients(userID)
	if len(recipients) == 0 {
		return
	}

	actor := s.actorURL(username)
	note := s.buildNoteObject(actor, postID, content, createdAt, "", contentWarning)

	// Addressed to named actors only. No Public, and no followers collection
	// either: a follower is not necessarily a friend, and this post is for
	// friends. Both the activity and the object carry the same addressing,
	// since receivers differ over which one they read.
	to := make([]string, 0, len(recipients))
	for _, r := range recipients {
		to = append(to, r.actorURL)
	}
	note["to"] = to
	note["cc"] = []string{}
	note[audienceMarker] = "friends"

	create := map[string]any{
		"@context":     agoraContext,
		"id":           actor + "/posts/" + postID + "/activity",
		"type":         "Create",
		"actor":        actor,
		"to":           to,
		"cc":           []string{},
		audienceMarker: "friends",
		"object":       note,
	}

	// Deduplicated by inbox: Agora serves one shared inbox per instance, so
	// several friends on the same peer would otherwise each get their own copy
	// of an identical activity.
	sent := map[string]bool{}
	for _, r := range recipients {
		if r.inboxURL == "" || sent[r.inboxURL] {
			continue
		}
		sent[r.inboxURL] = true
		s.enqueueAPDelivery(userID, r.inboxURL, create)
	}
	log.Printf("federation: friends-only post %s delivered to %d instance inbox(es) for %d friend(s)", postID, len(sent), len(recipients))
}

type friendRecipient struct {
	actorURL string
	inboxURL string
}

// remoteFriendRecipients lists this user's accepted friends who live on another
// Agora instance, with the inbox to reach each one at.
//
// The inbox prefers the ap_following row written when the friendship was
// established (AGORA-336) and falls back to Agora's shared /federation/inbox,
// which every Agora actor document advertises, so a friendship whose follow row
// predates that fix still resolves.
func (s *Service) remoteFriendRecipients(userID string) []friendRecipient {
	rows, err := s.db.Query(`
		SELECT DISTINCT u.ap_actor_url,
		       COALESCE(NULLIF(af.followed_inbox_url, ''),
		                'https://' || u.remote_instance || '/federation/inbox')
		FROM friendships f
		JOIN users u ON u.id = CASE WHEN f.requester_id = $1 THEN f.addressee_id ELSE f.requester_id END
		LEFT JOIN ap_following af
		       ON af.follower_user_id = $1 AND af.followed_actor_url = u.ap_actor_url
		WHERE (f.requester_id = $1 OR f.addressee_id = $1)
		  AND f.status = 'accepted'
		  AND u.is_remote = true
		  AND COALESCE(u.ap_actor_url, '') != ''
		  AND COALESCE(u.remote_instance, '') != ''
	`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []friendRecipient
	for rows.Next() {
		var r friendRecipient
		if rows.Scan(&r.actorURL, &r.inboxURL) == nil && r.actorURL != "" {
			out = append(out, r)
		}
	}
	return out
}

// isAcceptedFriendByActor reports whether the remote account behind an actor URL
// is an accepted friend of a local user (AGORA-339).
//
// Keyed on the actor URL because that is the only identity an inbound activity
// carries, and resolved through remoteUserIDForActor so a friendship recorded
// against a legacy stub, which has no ap_actor_url, still counts. Without that
// fallback a friendship formed before AGORA-333 would silently fail this check.
func (s *Service) isAcceptedFriendByActor(localUserID, actorURL string) bool {
	remoteUserID := s.remoteUserIDForActor(actorURL)
	if remoteUserID == "" {
		return false
	}
	var ok bool
	s.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM friendships
			WHERE status = 'accepted'
			  AND ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
		)
	`, localUserID, remoteUserID).Scan(&ok)
	return ok
}

// friendsOnlyVisibility decides what an inbound addressed Note should be stored
// as, given its audience marker and whether it was addressed publicly.
//
// Returns "friends" only for a Note that both says it is friends-only and is
// genuinely not public. A post claiming to be friends-only while addressed to
// Public is a contradiction, and the safe reading of a contradiction about
// visibility is the more public one, since that is what the sender's own
// followers will already have seen.
func friendsOnlyVisibility(marker string, addressedPublicly bool) string {
	if marker == "friends" && !addressedPublicly {
		return "friends"
	}
	return "public"
}

// isAddressedPublicly reports whether any of the given audience fields names the
// ActivityStreams Public collection, in any of the three spellings in use.
func isAddressedPublicly(audiences ...[]string) bool {
	for _, list := range audiences {
		for _, a := range list {
			switch a {
			case "https://www.w3.org/ns/activitystreams#Public", "as:Public", "Public":
				return true
			}
		}
	}
	return false
}
