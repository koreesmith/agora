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
	if addressedPublicly {
		return "public"
	}
	switch marker {
	case "friends":
		return "friends"
	case "list":
		// AGORA-342: fail-closed. 'private' is already excluded by every
		// existing feed filter, so a list post is invisible until an explicit
		// post_audience join says otherwise. A dedicated visibility value would
		// have been admitted by default anywhere a query excludes rather than
		// allow-lists, and a missed query there leaks the post instead of
		// hiding it.
		return "private"
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

// ── Limited-audience thread fan-out (AGORA-340) ───────────────────────────────
//
// A limited-audience conversation can only be completed by the instance that
// owns it. Alice addresses a friends-only post to Bob and Carol; Bob replies,
// but Bob's instance was told nothing about Carol, deliberately, because
// ADR-002 keeps the audience's membership off the wire. So Bob's reply reaches
// Alice and stops there unless Alice's instance forwards it on.
//
// That requires the receiving instance to accept an activity attributed to Bob
// but signed by Alice's, which is the one exception to the rule that a signer
// may only speak for itself. The exception is bounded by thread ownership: a
// peer can only put words in someone's mouth inside a thread it already
// controls entirely, and could equally have posted those words as itself.
// AGORA-222 already accepts relay-forwarded Creates on the same reasoning.

// isThreadOwnerForwarding reports whether verifiedActor is the author of the
// thread that inReplyTo belongs to, and so is entitled to forward a reply into
// it on somebody else's behalf.
//
// Every condition here is load-bearing:
//   - the target must resolve to a post this instance already holds, so a
//     forward cannot introduce a thread nobody was part of;
//   - that post's author must be the signer, so only the thread's owner may
//     forward into it;
//   - the thread must be limited-audience, since a public thread needs no
//     forwarding and should not gain a bypass it has no use for.
func (s *Service) isThreadOwnerForwarding(verifiedActor, inReplyTo string) bool {
	parentID, rootPostID, visibility, postAuthorID, ok := s.resolveReplyTarget(inReplyTo)
	if !ok || parentID == "" || postAuthorID == "" {
		return false
	}
	// AGORA-342: a received friend-list post is stored 'private', so allow that
	// too, but only where an audience record proves it is one. Every other
	// 'private' post is genuinely addressed to nobody.
	switch visibility {
	case "friends":
	case "private":
		var isListPost bool
		s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM post_audience WHERE post_id = $1)`, rootPostID).Scan(&isListPost)
		if !isListPost {
			return false
		}
	default:
		return false
	}

	// The thread author must be the signer. Locally-authored threads are
	// excluded by construction: a local user has no ap_actor_url, so this
	// cannot match, and this instance would be the forwarder rather than a
	// recipient of one.
	var authorActor string
	s.db.QueryRow(`SELECT COALESCE(ap_actor_url,'') FROM users WHERE id = $1`, postAuthorID).Scan(&authorActor)
	return authorActor != "" && authorActor == verifiedActor
}

// FanOutThreadReply forwards a reply in a thread this instance owns to the rest
// of that thread's audience.
//
// Called after a remote reply has been ingested into a local friends-only
// thread. The replier is excluded, since their own instance already has it, and
// so is anyone whose instance shares an inbox with them, because the activity is
// addressed per-instance rather than per-person.
func (s *Service) FanOutThreadReply(rootPostID, replierActorURL string, activity []byte) {
	if !s.activityPubEnabled() {
		return
	}

	var authorID, visibility string
	var groupID *string
	if err := s.db.QueryRow(`
		SELECT author_id, visibility, group_id FROM posts WHERE id = $1 AND deleted_at IS NULL AND is_remote = false
	`, rootPostID).Scan(&authorID, &visibility, &groupID); err != nil {
		return
	}

	// AGORA-342: the audience to forward to is the one the post went out to, so
	// a reply in a friend-list thread reaches that list and stops there.
	var audience []friendRecipient
	switch {
	case visibility == "friends":
		audience = s.remoteFriendRecipients(authorID)
	case visibility == "group" && groupID != nil:
		audience = s.remoteListRecipients(authorID, *groupID)
	default:
		return
	}

	replierInbox, _ := s.remoteInboxFor(authorID, replierActorURL)

	var sent int
	seen := map[string]bool{}
	for _, r := range audience {
		// Skip the replier's own instance. It has the reply already, and
		// sending it back would be a redelivery its dedup has to absorb.
		if r.inboxURL == "" || r.inboxURL == replierInbox || r.actorURL == replierActorURL || seen[r.inboxURL] {
			continue
		}
		seen[r.inboxURL] = true
		s.enqueueAPDeliveryRaw(authorID, r.inboxURL, activity)
		sent++
	}
	if sent > 0 {
		log.Printf("federation: forwarded a reply in thread %s to %d further instance inbox(es)", rootPostID, sent)
	}
}

// ── Friend-list posts (AGORA-342) ─────────────────────────────────────────────
//
// The other half of AGORA-328, and the harder one. A friends-only audience is
// derivable at both ends from a relationship both ends record; a list is not.
// "Close Friends" is the author's own categorization, and ADR-002 keeps its
// membership off the wire, so the receiving instance learns only that its user
// was addressed. That is enough to enforce correctly and is all it should know.
//
// Stored fail-closed. The post lands with visibility 'private', which every
// existing feed filter already excludes, and becomes visible solely through an
// explicit post_audience join added where an addressed user should see it. The
// alternative, a new visibility value, would have been admitted by default
// anywhere a query excludes by `!= 'private'` rather than allow-listing, and a
// query overlooked there leaks somebody's private post rather than hiding it.
// For a privacy feature the failure mode decides the design.

// BroadcastListPost delivers a friend-list post to the list's members on other
// Agora instances, addressed to them by name.
//
// The list's name and its membership never leave this instance. Recipients are
// addressed individually, so each learns that they were included and nothing
// about who else was.
func (s *Service) BroadcastListPost(userID, postID string) {
	if !s.activityPubEnabled() {
		return
	}

	var username, visibility, content, contentWarning string
	var apEnabled bool
	var createdAt time.Time
	var groupID *string
	err := s.db.QueryRow(`
		SELECT u.username, u.activitypub_enabled, p.visibility, p.content, p.content_warning, p.created_at, p.group_id
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.author_id = $2 AND p.deleted_at IS NULL
	`, postID, userID).Scan(&username, &apEnabled, &visibility, &content, &contentWarning, &createdAt, &groupID)
	if err != nil || visibility != "group" || !apEnabled || groupID == nil {
		return
	}

	recipients := s.remoteListRecipients(userID, *groupID)
	if len(recipients) == 0 {
		return
	}

	actor := s.actorURL(username)
	note := s.buildNoteObject(actor, postID, content, createdAt, "", contentWarning)

	to := make([]string, 0, len(recipients))
	for _, r := range recipients {
		to = append(to, r.actorURL)
	}
	note["to"] = to
	note["cc"] = []string{}
	note[audienceMarker] = "list"

	create := map[string]any{
		"@context":     agoraContext,
		"id":           actor + "/posts/" + postID + "/activity",
		"type":         "Create",
		"actor":        actor,
		"to":           to,
		"cc":           []string{},
		audienceMarker: "list",
		"object":       note,
	}

	sent := map[string]bool{}
	for _, r := range recipients {
		if r.inboxURL == "" || sent[r.inboxURL] {
			continue
		}
		sent[r.inboxURL] = true
		s.enqueueAPDelivery(userID, r.inboxURL, create)
	}
	log.Printf("federation: list post %s delivered to %d instance inbox(es) for %d member(s)", postID, len(sent), len(recipients))
}

// remoteListRecipients lists the members of one friend list who live on another
// Agora instance.
//
// Membership is not gated on friendship here, unlike remoteFriendRecipients.
// AGORA-182/257 deliberately allow a followed account into a list without a
// mutual friendship, and a list's own membership is the audience the author
// chose; second-guessing it would silently drop somebody they meant to include.
func (s *Service) remoteListRecipients(userID, groupID string) []friendRecipient {
	rows, err := s.db.Query(`
		SELECT DISTINCT u.ap_actor_url,
		       COALESCE(NULLIF(af.followed_inbox_url, ''),
		                'https://' || u.remote_instance || '/federation/inbox')
		FROM friend_group_members fgm
		JOIN friend_groups fg ON fg.id = fgm.group_id AND fg.user_id = $1
		JOIN users u ON u.id = fgm.friend_id
		LEFT JOIN ap_following af
		       ON af.follower_user_id = $1 AND af.followed_actor_url = u.ap_actor_url
		WHERE fgm.group_id = $2
		  AND u.is_remote = true
		  AND COALESCE(u.ap_actor_url, '') != ''
		  AND COALESCE(u.remote_instance, '') != ''
	`, userID, groupID)
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

// recordPostAudience records which local users an inbound limited post was
// addressed to, which is the only thing that will make it visible to them.
//
// Silently recording nobody is the correct outcome for a post addressed only to
// users this instance does not host: it stays stored and invisible, rather than
// becoming visible to everyone for want of an audience.
func (s *Service) recordPostAudience(postID string, addressed []string) int {
	var n int
	for _, actorURL := range addressed {
		var userID string
		s.db.QueryRow(`
			SELECT id FROM users WHERE is_remote = false AND ap_actor_url = $1
		`, actorURL).Scan(&userID)
		if userID == "" {
			// Local actor URLs are not stored on the row, so fall back to the
			// username the actor URL encodes.
			if name := usernameFromActorURL(actorURL, s.cfg.InstanceDomain); name != "" {
				s.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = LOWER($1) AND is_remote = false`, name).Scan(&userID)
			}
		}
		if userID == "" {
			continue
		}
		if _, err := s.db.Exec(`
			INSERT INTO post_audience (post_id, user_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, postID, userID); err == nil {
			n++
		}
	}
	return n
}

// isAddressedListMember reports whether a remote actor is in the friend list a
// local post was limited to, and so is entitled to reply to it or react to it.
//
// This is the authorisation counterpart to remoteListRecipients: exactly the
// people that function delivered the post to are the people this one lets back
// in. Membership is read fresh, so removing somebody from a list closes the
// thread to them from that moment; the copy they already hold is beyond recall,
// but nothing further of theirs is accepted.
func (s *Service) isAddressedListMember(postID, actorURL string) bool {
	if actorURL == "" {
		return false
	}
	var ok bool
	s.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1
			FROM posts p
			JOIN friend_group_members fgm ON fgm.group_id = p.group_id
			JOIN users u ON u.id = fgm.friend_id
			WHERE p.id = $1
			  AND p.group_id IS NOT NULL
			  AND u.ap_actor_url = $2
		)
	`, postID, actorURL).Scan(&ok)
	return ok
}
