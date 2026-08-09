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

// ── Edits and deletes of a limited post (AGORA-343) ───────────────────────────

// limitedPostAudience returns the remote recipients of a non-public post,
// derived the same way the original Create derived them, and reports whether
// the post was limited at all.
//
// Deliberately does not filter on deleted_at: DeletePost soft-deletes the row
// before broadcasting, so requiring a live row would return an empty audience
// for every delete and leave the copies standing.
//
// The audience is read fresh rather than replayed from what was sent. Somebody
// removed from the list since the post will therefore not receive the edit or
// the delete. Their copy is already beyond recall, and adding them back to the
// delivery list purely to withdraw it would tell them they had been removed.
func (s *Service) limitedPostAudience(userID, postID string) (recipients []friendRecipient, limited bool) {
	var visibility string
	var groupID, parentID *string
	if err := s.db.QueryRow(`
		SELECT visibility, group_id, parent_id FROM posts WHERE id = $1 AND author_id = $2
	`, postID, userID).Scan(&visibility, &groupID, &parentID); err != nil {
		return nil, false
	}

	// AGORA-347: a reply inherits its thread's visibility, so it arrives here
	// looking limited, but its audience is the thread's and not this author's.
	// Answering with the author's own friends would be wrong twice over: it
	// misses the people who actually hold the reply, and it tells the author's
	// friends that a post they never saw has been withdrawn.
	//
	// The replier cannot know the thread's audience and by design never will,
	// so the correct recipient is the thread itself, and its owner fans out
	// from there.
	if parentID != nil {
		return s.threadLifecycleRecipients(*parentID, postID), true
	}

	switch {
	case visibility == "friends":
		return s.remoteFriendRecipients(userID), true
	case visibility == "group" && groupID != nil:
		return s.remoteListRecipients(userID, *groupID), true
	case visibility == "public":
		return nil, false
	default:
		// 'private' and anything unrecognised: limited, with no federated
		// audience. Returning limited=true is the point, since it keeps the
		// caller off the public path rather than broadcasting to followers.
		return nil, true
	}
}

// addressLimited rewrites an activity and its object to address named
// recipients only, replacing whatever public addressing the builder produced.
//
// Both levels have to be rewritten. A receiving instance reads the object's own
// to/cc when deciding what a post is (friendsOnlyVisibility does exactly that),
// so an activity addressed privately around an object still claiming Public
// would be stored public by its recipient.
func addressLimited(activity map[string]any, recipients []friendRecipient, marker string) {
	to := make([]string, 0, len(recipients))
	for _, r := range recipients {
		to = append(to, r.actorURL)
	}

	activity["to"] = to
	activity["cc"] = []string{}
	activity[audienceMarker] = marker
	if obj, ok := activity["object"].(map[string]any); ok {
		obj["to"] = to
		obj["cc"] = []string{}
		obj[audienceMarker] = marker
	}
}

// deliverLimited enqueues an activity to each recipient's instance, once per
// inbox, since the addressing is per-instance rather than per-person.
func (s *Service) deliverLimited(userID string, recipients []friendRecipient, activity map[string]any) int {
	sent := map[string]bool{}
	for _, r := range recipients {
		if r.inboxURL == "" || sent[r.inboxURL] {
			continue
		}
		sent[r.inboxURL] = true
		s.enqueueAPDelivery(userID, r.inboxURL, activity)
	}
	return len(sent)
}

// audienceMarkerFor names the audience kind a post's visibility corresponds to
// on the wire.
func audienceMarkerFor(visibility string) string {
	if visibility == "group" {
		return "list"
	}
	return "friends"
}

// ── Edits and deletes of a reply in a limited thread (AGORA-347) ──────────────
//
// AGORA-343 fixed a limited *post's* edit and delete. A reply is a different
// path: it reaches the thread's author, and the author's instance forwards it to
// the rest of the audience, because the replier's instance does not know who
// else is in it and by design never will. Nothing did the equivalent for an
// Update or a Delete of that reply, so an edit reached the thread author and
// nobody else, and a delete left every other copy standing.
//
// Two halves are needed, not one. Forwarding the activity is useless on its own,
// because both inbound handlers require attributedTo to equal the signer, and a
// forwarded edit is by definition attributed to somebody other than the
// forwarder. So the receiving side needs the same carve-out AGORA-340 made for
// the reply itself, or the fan-out would deliver into a wall.

// rootPostIDOf returns the top-level post a comment belongs to, or the post
// itself when it is already top-level.
func (s *Service) rootPostIDOf(postID string) string {
	var parentID *string
	if err := s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1`, postID).Scan(&parentID); err != nil {
		return ""
	}
	if parentID == nil {
		return postID
	}
	// Agora caps threads at root -> comment -> reply, so one hop up is enough,
	// and a second walk would only re-find the same row.
	var grandParentID *string
	s.db.QueryRow(`SELECT parent_id FROM posts WHERE id = $1`, *parentID).Scan(&grandParentID)
	if grandParentID != nil {
		return *grandParentID
	}
	return *parentID
}

// isThreadOwnerForwardingComment authorises a forwarded edit or delete.
//
// Mirrors isThreadOwnerForwarding, but keyed on a comment this instance already
// holds rather than on an inReplyTo URL, because an Update carries no inReplyTo
// and a Delete carries nothing but an id.
//
// The conditions are the same three, and each is load-bearing: the comment must
// resolve to a thread we already hold, that thread's author must be the signer,
// and the thread must be limited. A public thread gains no bypass because it has
// no use for one.
func (s *Service) isThreadOwnerForwardingComment(verifiedActor, commentPostID string) bool {
	rootID := s.rootPostIDOf(commentPostID)
	if rootID == "" || rootID == commentPostID {
		// Not a comment. A forwarded edit of a top-level post is not a thing:
		// the author's own instance sends that directly to everyone.
		return false
	}

	var visibility, rootAuthorActor string
	if err := s.db.QueryRow(`
		SELECT p.visibility, COALESCE(u.ap_actor_url, '')
		FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.id = $1 AND p.deleted_at IS NULL
	`, rootID).Scan(&visibility, &rootAuthorActor); err != nil {
		return false
	}
	if rootAuthorActor == "" || rootAuthorActor != verifiedActor {
		return false
	}

	switch visibility {
	case "friends":
		return true
	case "private":
		// A received friend-list post is stored 'private' (AGORA-342), so allow
		// that, but only where an audience record proves it is one. Every other
		// 'private' post is genuinely addressed to nobody.
		var isListPost bool
		s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM post_audience WHERE post_id = $1)`, rootID).Scan(&isListPost)
		return isListPost
	}
	return false
}

// FanOutThreadLifecycle forwards an edit or a delete of a reply to the rest of
// the thread's audience, when this instance owns that thread.
//
// Thin on purpose: FanOutThreadReply already resolves the audience from the
// root post's own visibility, refuses a thread this instance does not own, and
// skips the sender's instance. The only thing worth adding here is finding the
// root, since the caller has a comment.
func (s *Service) FanOutThreadLifecycle(commentPostID, senderActorURL string, activity []byte) {
	if len(activity) == 0 || commentPostID == "" {
		return
	}
	rootID := s.rootPostIDOf(commentPostID)
	if rootID == "" || rootID == commentPostID {
		return
	}
	s.FanOutThreadReply(rootID, senderActorURL, activity)
}

// localPostIDForRemoteObject resolves a remote object id to the local row
// holding it, scoped to the author it claims.
//
// Used to authorise a forwarded edit or delete before applying it: the check
// needs the local comment, and the caller only has the sender's object id.
func (s *Service) localPostIDForRemoteObject(objectID, authorActorURL string) string {
	if objectID == "" || authorActorURL == "" {
		return ""
	}
	var postID string
	s.db.QueryRow(`
		SELECT p.id FROM posts p JOIN users u ON u.id = p.author_id
		WHERE p.remote_post_id = $1 AND p.remote_instance = $2 AND u.ap_actor_url = $3 AND p.deleted_at IS NULL
	`, objectID, domainFromURL(objectID), authorActorURL).Scan(&postID)
	return postID
}

// threadLifecycleRecipients addresses an edit or a delete of a reply at the
// thread it belongs to, which for the replier's instance means the parent's
// author and the thread's owner.
//
// Deliberately the same target DeliverReply already uses, resolved through
// lookupRemoteTarget so a stale stub with no inbox self-heals here as it does
// there (AGORA-254). Anything beyond these two is the thread owner's to reach,
// which is what FanOutThreadLifecycle does on the other side.
func (s *Service) threadLifecycleRecipients(parentID, commentID string) []friendRecipient {
	seen := map[string]bool{}
	var out []friendRecipient

	add := func(targetID string) {
		if targetID == "" {
			return
		}
		actorURL, inboxURL, _, ok := s.lookupRemoteTarget(targetID)
		if !ok || actorURL == "" || inboxURL == "" || seen[inboxURL] {
			return
		}
		seen[inboxURL] = true
		out = append(out, friendRecipient{actorURL: actorURL, inboxURL: inboxURL})
	}

	add(parentID)
	// The root as well as the direct parent: replying to a comment in somebody
	// else's thread means the owner who has to fan out is not the parent's
	// author, and reaching only the parent would leave them unaware.
	if root := s.rootPostIDOf(commentID); root != "" && root != commentID {
		add(root)
	}
	return out
}
