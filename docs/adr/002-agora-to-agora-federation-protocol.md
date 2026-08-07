# ADR-002: Agora-to-Agora Federation Protocol

**Status:** Proposed
**Ticket:** AGORA-326
**Date:** 2026-08-07
**Supersedes:** the implicit "two protocols side by side" arrangement that has existed since AGORA-145
**Superseded by:** none

---

## Context

Agora speaks three federation protocols. Two of them connect Agora instances to each other:

1. **Standard ActivityPub**, added in AGORA-145 and extended continuously since. Talks to Mastodon and the rest of the fediverse, and to other Agora instances, since every Agora instance is an ActivityPub server.
2. **The legacy Agora-to-Agora protocol**, which predates ActivityPub support and only ever talks to other Agora instances. Ed25519-signed with an instance-wide key, five activity types, its own delivery queue and its own inbox path.

(The third, native AT Protocol, is out of scope here. It talks to Bluesky and has no bearing on Agora-to-Agora.)

The question this ADR settles: **should the legacy protocol be fixed and extended, kept as-is, or retired, and if retired, what carries the things ActivityPub has no answer for?**

### The deciding requirement

Stated during the spike, and it is the constraint the decision turns on:

> A friend list must work as an audience across instances. If a local user has a list called Close Friends, and a member of that list is on another Agora instance, a post addressed only to that list must reach them, be interactable by them, and must not be public.

This is not satisfied by anything today, on either protocol, and that is worth being blunt about before comparing options:

- The legacy protocol drops any inbound activity whose visibility is not `public` (`federation.go:270`), and only ever broadcasts public posts in the first place.
- The ActivityPub path only fires on `visibility == "public"` (`feed.go:1152`).

**No non-public post has ever federated out of an Agora instance by any route.** A friend list containing a remote member silently behaves as if that member were not there. So this requirement is not "keep what works", it is new capability, and the decision below is largely a question of which protocol makes it reachable.

### What the legacy protocol actually carries

Five activity types (`internal/federation/federation.go`): `post`, `delete_post`, `friend_request`, `friend_accept`, `profile_update`.

### Three findings that reframe the question

Investigating AGORA-316 turned up facts that were not visible when AGORA-326 was written, and they change the shape of the answer.

**Finding 1: every content-carrying legacy activity is already duplicated by an ActivityPub equivalent that fires on the same event.**

| Legacy activity | Fired at | ActivityPub equivalent fired alongside it |
|---|---|---|
| `post` | `internal/feed/feed.go:1155` | `BroadcastPublicPost` (`:1169`) |
| `delete_post` | `internal/feed/feed.go:1366` | `BroadcastDeletePost` (`:1374`) |
| `profile_update` | `internal/users/users.go:428,468` | `BroadcastActorUpdate` (`:443,475`) |

These are not alternatives that fire in different circumstances. Both calls sit in the same `if` block, on the same event. The legacy version carries content, one `image_url` and a timestamp; the ActivityPub version carries all of that plus polls, content warnings, multiple attachments, video, custom emoji, quote authorization and edits.

**Finding 2: a federated friendship has never granted access to anything.**

`handleInboundPost` drops any activity whose visibility is not `public` (`federation.go:270`), and `BroadcastToFriendInstances` is only ever called for public posts in the first place. So a friends-only post has never federated to a remote friend, in any version of this protocol.

The only thing an accepted federated friendship does is add the remote instance to the recipient list for **public** posts, which an ActivityPub follow does better and with full fidelity. The friendship is a label on a list. It is not an access-control edge.

**Finding 3: AGORA-316 has made duplicate delivery possible for the first time.**

Legacy delivery never worked before AGORA-316, so the redundancy in Finding 1 was harmless. It is not harmless now.

The two protocols key ingested posts differently. Legacy stores `remote_post_id` as the origin's bare post UUID; ActivityPub stores it as the note URL (`actor + "/posts/" + postID`, `activitypub.go:567`). The unique index that dedupes ingested posts is on `(remote_post_id, remote_instance)`, so the two never collide.

The result: where a person on instance B is both a legacy friend of and an ActivityPub follower of someone on instance A, a single public post from A arrives at B twice, attributed to two different local stub accounts for the same human. Nothing prevents that pairing. AGORA-167 stops a user friending an *ActivityPub actor*, but a legacy remote stub and an AP remote stub are separate `users` rows keyed on different columns (`remote_user_id` versus `ap_actor_url`), so the same person can legitimately exist as both.

This needs fixing regardless of what this ADR decides, and it is urgent in a way the rest of this is not.

### What ActivityPub genuinely does not have

Two things, and it is worth being precise because the rest of the decision turns on them.

**Mutual friendship with a request and accept handshake.** ActivityPub defines `Follow`, which is asymmetric. There is no FEP for friendship or any other mutual relationship. The ActivityStreams vocabulary contains `Relationship` objects and suggests `Offer`-based friend requests, but the [SocialHub discussion on non-follow relationships](https://socialhub.activitypub.rocks/t/friendship-and-other-non-follow-relationships/5248) describes that route as "very underdefined" and unused in practice, and closes by deferring standardization until there is implementation experience to draw on.

**Instance-to-instance peering.** ActivityPub has no notion of two servers being connected. Any server may deliver to any inbox. The Federation tab, `federated_instances`, the direction tracking from AGORA-321 and the disconnect from AGORA-320 are all concepts with no ActivityPub counterpart short of relays, which are a different shape (a relay is a third-party firehose, not a bilateral relationship).

### Prior art

Friendica is the closest analogue to Agora: a federated social network with real friendships, private posts and profiles, rather than a microblog. It has supported ActivityPub since 2018 and still initiates Friendica-to-Friendica relationships over its own DFRN protocol rather than ActivityPub, using ActivityPub for interoperability with Mastodon and everything else. Per the SocialHub thread above, Friendica and NodeBB both express cross-platform "connections" as reciprocated Follows, where following back is what grants access to friends-only posts.

Two lessons, pulling in opposite directions:

- A mature project in exactly this space concluded that a bespoke relationship layer was worth keeping alongside ActivityPub. That is evidence against retiring ours outright.
- The mechanism that carries the *content* of those relationships, and the thing that grants access, is a reciprocated ActivityPub Follow. That is evidence that the relationship layer should be thin.

---

## Decision

**One transport, two vocabularies. ActivityPub carries everything, including friend requests and limited-audience posts, using an Agora-specific activity type only where ActivityPub has no standard. The legacy Ed25519 transport is frozen now and removed once both live instances are on the new build.**

Concretely:

1. **All content federates over ActivityPub only.** The legacy `post`, `delete_post` and `profile_update` activities are removed from the outbound path. They are strictly redundant, strictly worse, and currently cause duplicates.
2. **Friend-list and friends-only posts federate, addressed to explicit actor URLs.** This is the deciding requirement above, and it is the primary reason for the choice rather than a side benefit. Details below.
3. **A federated friendship becomes an ActivityPub activity delivered to the ActivityPub inbox**, signed with HTTP Signatures, queued in `ap_delivery_queue`, using an Agora-namespaced activity type. Not the legacy transport.
4. **An accepted friendship establishes a mutual ActivityPub follow underneath.** That is what makes ordinary content flow.
5. **Instance peering stays**, as a local admin concept backed by `federated_instances` and the `/.well-known/agora-instance` document. It stops being a transport concern.
6. **The legacy Ed25519 signing, verification, queue and inbox path are deleted** once both live instances have the replacement.

### Why this is the only option that satisfies the deciding requirement

ActivityPub already has audience addressing. A post's `to`/`cc` is a list of actor URLs, and an activity addressed to named actors with no `Public` and no followers collection is delivered to exactly those actors and to nobody else. Mastodon's own direct and limited posts work this way, so the mechanism is not a private extension: it is the part of the protocol that is most widely and most carefully implemented, because getting it wrong leaks private posts.

Agora already emits addressed activities today. `DeliverReply` addresses a reply at a specific remote actor, and `enqueueAPDelivery` already resolves per-actor inboxes and honours `ap_blocked_by`. A list-addressed post is the same machinery with a different recipient set.

The legacy protocol has none of this. It has one visibility value it accepts (`public`, hardcoded) and no addressing concept whatsoever. Delivering a list post over it means inventing audience addressing, per-recipient delivery, and inbound visibility enforcement from scratch, on the transport that has no tests and whose signature scheme was broken for its entire life. That is the same "extend the legacy protocol to parity" work rejected as Option A, arrived at from a different direction.

### Why one transport rather than the obvious hybrid

The hybrid (ActivityPub for content, legacy protocol for relationships) is the answer AGORA-326 predicted, and it is defensible. It is rejected because it misidentifies where the cost is.

The problem with the legacy protocol is not the *idea* of a friend request. It is the bespoke transport underneath it: an instance-wide Ed25519 key, signatures over a JSON body field, a second delivery queue, a second inbox code path, and until this week, zero tests. That machinery produced a defect that silently rejected every inbound activity for the entire life of the protocol, and nothing surfaced it because the failure path did not log.

Keeping the legacy protocol "just for friendships" keeps all of that machinery, permanently, to carry two activity types. Moving friend requests onto the ActivityPub transport keeps the *concept* while deleting the machinery: HTTP Signatures, the delivery queue with backoff, per-actor keys, instance blocking, rate limiting (AGORA-149) and the SSRF-safe client are all already there, already tested, and already carry far more traffic than a friend request ever will.

A custom activity type inside ActivityPub is not exotic. It is what the extensibility is for, and it degrades correctly: a Mastodon server receiving an activity type it does not recognize ignores it. Agora's own inbox already does the same (and now says so in the log, per AGORA-325).

### Why not retire friendships entirely in favour of mutual follows

Tempting, given Finding 2: a federated friendship currently grants nothing a follow does not. But friendship is not only a federation concept. `friendships` is load-bearing in ten packages (`feed`, `dm`, `albums`, `groups`, `search`, `users`, `blocks`, `notifications`, `friends`, `admin`) and gates friends-only visibility, direct messages, wall posts and group tagging for **local** users. Collapsing friendship into "mutual follow" is a product change to the whole application, not a federation change, and it is not this ADR's to make.

The narrower claim this ADR does make: a federated friendship should mean the same thing a local one does. Today it does not, and point 3 above is what closes that gap.

---

## Detailed design

### The friend request activity

Delivered to the existing `/federation/inbox`, HTTP-Signature-signed as the requesting **user's** actor (not the instance actor: a friend request is from a person), queued through `enqueueAPDelivery` so it inherits backoff, blocking and the `ap_blocked_by` guard.

Shape, using ActivityStreams `Offer` + `Relationship`, which is the closest thing the vocabulary has, with an Agora extension term for the relationship kind:

```json
{
  "@context": ["https://www.w3.org/ns/activitystreams",
               {"agora": "https://agora.social/ns#"}],
  "id": "https://a.example/federation/users/alice/friend-requests/<uuid>",
  "type": "Offer",
  "actor": "https://a.example/federation/users/alice",
  "to": ["https://b.example/federation/users/bob"],
  "object": {
    "type": "Relationship",
    "subject": "https://a.example/federation/users/alice",
    "relationship": "agora:Friend",
    "object": "https://b.example/federation/users/bob"
  }
}
```

Accept and reject reuse ActivityPub's own `Accept` and `Reject` wrapping the original `Offer`, exactly as they already wrap `Follow` (`handleInboundAcceptFollow`/`handleInboundRejectFollow` are the model). Unfriending is `Undo` of the `Offer`.

The precise vocabulary is an implementation detail to settle in the ticket, not here. What this ADR fixes is that it rides the ActivityPub transport and not a second one. If `Offer`/`Relationship` proves awkward in practice, a fully namespaced `agora:FriendRequest` type is an acceptable substitute; the transport decision does not change.

### Mutual follow on acceptance

When a friend request is accepted, both sides establish an ActivityPub follow of the other, through the existing `FollowFediverseAccount` path. This is what makes ordinary public content flow, and it is why removing legacy `post` delivery costs nothing.

Unfriending sends `Undo(Offer)` and undoes both follows.

### Limited-audience posts (friend lists and friends-only)

The deciding requirement. Three parts, and the third is the hard one.

**1. Outbound addressing.** A post with `visibility = 'group'` resolves its `friend_group_members` to actor URLs and addresses the `Create` at exactly those actors:

```json
{
  "type": "Create",
  "actor": "https://a.example/federation/users/alice",
  "to": ["https://b.example/federation/users/bob",
         "https://c.example/federation/users/carol"],
  "cc": [],
  "object": { "type": "Note", "to": [ ...same... ], "cc": [] }
}
```

No `Public` anywhere, no followers collection. `visibility = 'friends'` is the same shape with the audience derived from accepted friendships instead of a list.

**The list's name and membership are never federated.** `to` carries only the actors who are receiving it. That Bob is in a list called "Close Friends", and that Carol is also in it, is Alice's instance's private categorization. Bob's server learns that Bob was addressed, not why, and not by whom else he is kept company. This falls out of addressing actors directly rather than publishing an audience object, and it is a deliberate property worth preserving.

**2. Inbound enforcement.** An arriving `Create` with no `Public` in `to`/`cc` is stored non-public and shown only to the addressed local users. Agora's ingest currently assumes public (`ingestFollowedPost` hardcodes `'public'`), so this needs a real audience column or join table on the receiving side, not a visibility flag alone: "who was this addressed to" is per-recipient state that the existing schema has nowhere to put.

**3. Replies, and why the origin instance has to fan them out.** If Bob replies, that reply must reach Alice **and** Carol. Bob's server cannot do this: it was addressed the post, so it knows Alice and it knows Bob, but it does not know Carol exists, and by design it never will.

So the origin instance mediates. Alice's instance owns the thread and the audience. When it receives a reply into a thread it owns, it re-delivers that reply to the rest of the original audience. This is the only arrangement consistent with not publishing the membership list, and it is a genuine addition: `DeliverReply` today addresses the parent's author and any resolved mentions (`activitypub.go:3457`), with no notion of a thread audience at all.

Consequences to accept deliberately:

- **The origin server is a single point of failure for the conversation.** If Alice's instance is down, Bob and Carol stop seeing each other's replies even though both are up. Acceptable: it is Alice's post and Alice's audience.
- **Reactions have the same shape.** A `Like` from Bob needs the same fan-out for Carol to see a count that matches Alice's.
- **Enforcement is on the receiving server.** Once a limited post is delivered, the receiving instance decides who sees it. Agora-to-Agora that is fine, both ends run this code. It is inherent to federation rather than a defect of this choice: Mastodon's followers-only posts have exactly the same property.

### Mixed audiences: deliver to everyone we can, and say what each member gets

A friend list can already contain accounts from all three networks (AGORA-182 for fediverse, AGORA-257 for Bluesky), because lists are used for *reading* as well as posting. Removing a Mastodon friend from a list to protect a posting guarantee would break the feed of them, so list membership is not the lever. What each member receives at post time is.

The three networks are not one "non-Agora" bucket. They differ in kind:

| Network | Can it receive a limited post? | What the member actually gets |
|---|---|---|
| **Agora** | Yes, fully | A private post. Replies and reactions fan out to the rest of the audience. The guarantee holds. |
| **Fediverse** | Yes, degraded | Arrives as a direct/private mention. No "Close Friends" concept exists there, and per Mastodon's own docs a recipient **can change their reply's visibility to public** and can pull new people in by mentioning them. |
| **Bluesky** | **No** | Nothing to deliver to. AT Proto repos are public by design and there is no addressed-delivery or private-post mechanism. Private data is targeted for 2026 but has not shipped. |

**Decision: allow and label.** Deliver to every member the protocol permits, and state in the composer what each one will actually experience, before the post is sent.

```
Audience: Close Friends · 5 members
  3 on Agora        private post, replies stay in the group
  1 on Mastodon     arrives as a direct message, they can reply publicly
  1 on Bluesky      cannot receive private posts, will not see this
```

Rejected alternative: filtering non-deliverable members out silently. Its failure mode is invisible. The author believes they posted to five people, two received nothing, and nothing anywhere says so. A visible degraded delivery is better than an invisible non-delivery.

**The strong guarantee is scoped to Agora, deliberately and explicitly.** The one-click widening a fediverse recipient can perform is a real weakening of a feature whose whole promise is "this is not public", and it is accepted here on the grounds that the author is told before sending. That makes the label load-bearing rather than decorative: it is the entire mitigation, and it is why the fediverse row says "they can reply publicly" rather than something reassuring. If the label is ever dropped or softened for being noisy, this decision has been reversed without anyone noticing.

Bluesky is not a policy choice. There is no mechanism, so the row is informational only.

### What happens to each legacy activity type

| Activity | Disposition |
|---|---|
| `post` | **Removed from the outbound path immediately.** Redundant with `BroadcastPublicPost` and currently causes duplicate ingestion (Finding 3). |
| `delete_post` | **Removed.** Redundant with `BroadcastDeletePost`. |
| `profile_update` | **Removed.** Redundant with `BroadcastActorUpdate` (AGORA-242). |
| `friend_request` | **Reimplemented** over ActivityPub as above. Legacy handler kept for one release to accept requests from instances still on the old build, then deleted. |
| `friend_accept` | Same. |

Inbound legacy handlers outlive outbound ones by one release. That asymmetry is deliberate: an instance that upgrades first should still be able to receive from one that has not.

### Instance peering

Unchanged in behaviour, narrowed in meaning. `federated_instances` remains the admin's list of known Agora peers, with the direction tracking and disconnect from AGORA-321 and AGORA-320, fed by `/.well-known/agora-instance`.

What changes is that peering stops being a delivery precondition. It becomes what it already effectively is: a discovery and moderation surface. The Federation tab copy shipped in AGORA-321 already says this rather than implying timelines flow from it.

`/.well-known/agora-instance` stays. It is how one Agora instance recognizes another as Agora, which NodeInfo does not tell you with enough specificity, and it is where the peering UI gets an instance's name and rules.

### The instance-wide Ed25519 keypair

Deleted with the transport. `federation_public_key`/`federation_private_key` in `instance_settings` become dead once nothing signs or verifies with them. They should be removed in the same change that deletes the transport, not left as a loaded gun in the settings table. Note AGORA-143 already had to specifically exclude them from the admin settings API.

---

## Migration

**The two live instances are the entire migration surface, and the cost is close to zero.**

The legacy protocol has never successfully delivered a single activity to a receiving instance. Every inbound one failed signature verification for the whole life of the protocol (AGORA-316). There is therefore no working federated behaviour to preserve, no accumulated federated state that depends on the legacy transport, and no interoperability commitment to anyone outside these two servers.

One wrinkle, created by this week's work: as of the build now on `origin/v4.0.0`, legacy delivery **does** work. If federated friendships are created between the two live instances before the replacement lands, those friendships are stored in the local `friendships` table on both sides and survive the transport change untouched. Only the delivery mechanism for *new* requests changes.

Sequence:

1. **Now:** stop outbound legacy `post`/`delete_post`/`profile_update`. Fixes the duplicate-ingestion bug and removes the redundancy. Nothing is lost, since the ActivityPub equivalent already fires on every one of these events. (AGORA-327)
2. **Next:** implement friend requests over ActivityPub, with mutual follow on acceptance. Keep the legacy inbound handlers.
3. **Then:** limited-audience posts: outbound addressing, inbound per-recipient audience storage, and origin-mediated reply and reaction fan-out. This is the deciding requirement and the largest single piece of work in the sequence.
4. **Finally:** once both instances run the new build, delete the legacy transport, its handlers, its queue and the instance keypair.

Step 1 is independent of everything after it and should not wait. Step 3 depends on step 2 only for the friendship edge that defines a list member's eligibility; the addressing work itself could start in parallel if useful.

---

## Consequences for the blocked tickets

**AGORA-322 (public posts do not flow between federated instances without a friendship)** is largely dissolved rather than answered. Content already flows between Agora instances over ActivityPub for anyone who follows anyone. What remains of the ticket is the genuine question it raised: should peering with an instance optionally pull that instance's public timeline, the way a relay does? That is a real feature, it is now clearly a *relay-shaped* feature rather than a legacy-protocol one, and it should be rewritten against the AGORA-218 relay machinery. The per-peer opt-in the ticket recommends is still the right shape, because an admin action should not silently reshape every local user's public feed.

**AGORA-323 (direct messages across instances)** is unblocked and its scope narrows sharply. A DM becomes a `Create` carrying a `Note` addressed to a single actor with no Public in `to`/`cc`, which is how Mastodon direct messages already work. As a side effect it works with Mastodon users, not only Agora ones. The product questions the ticket raises (who may message whom, group conversations, and what users are told about privacy given that DMs are readable by both instances' administrators) are unchanged and still need answering before implementation.

---

## Rejected alternatives

### Option A: fix the legacy protocol and extend it to parity

Rejected. Reaching parity means reimplementing comments, reactions, reposts, edits, polls, content warnings, videos and multi-image posts on a bespoke protocol, every one of which is already working on the ActivityPub side. It also means two inbound paths to secure and test forever. The cost is enormous and entirely duplicative.

### Option B: retire the legacy protocol and model friendship as a mutual ActivityPub follow, with no friendship concept over federation

Rejected, for now. It is the cleanest protocol answer and it has real prior art, but friendship gates ten packages' worth of local behaviour and collapsing it into "mutual follow" is a product decision about the whole application. If that decision is ever made, this ADR does not block it: the mutual follow this design establishes on acceptance is exactly the substrate such a change would need.

### Option C: hybrid, with the legacy protocol kept for friendships and peering

Rejected, and it was the predicted answer. Two objections, the second fatal.

It keeps an entire bespoke transport (instance-wide keypair, body-field signatures, a second queue, a second inbox path) alive permanently in order to carry two activity types, when the ActivityPub transport carrying everything else can carry them with no new machinery at all.

More decisively: it cannot satisfy the deciding requirement without becoming Option A. If friend lists are a federated audience, then the transport carrying the relationship also has to carry the audience-addressed post, the per-recipient delivery, the inbound visibility enforcement and the reply fan-out. That is the whole of ActivityPub's addressing model, rebuilt on the transport with no tests. The concept worth keeping is the friend request; the transport is not.

### Option D: put friend requests on the legacy transport but delete its content types

Rejected as a stopping point, though it is exactly what migration step 1 produces. As a destination it is Option C with fewer activity types, and inherits the same objection.

---

## Open questions

- **Vocabulary.** `Offer` + `Relationship` versus a namespaced `agora:FriendRequest`. Settle during implementation; the transport decision stands either way.
- **Should a friend request from an instance that is not a known peer be accepted?** Today anyone can deliver to the inbox and peering is not a precondition. Staying open matches the accepted position (open federation, ban bad actors after the fact), but it is worth stating deliberately rather than inheriting.
- **Exact composer copy.** The decision to allow and label is settled above; the wording is not. It has to convey a degraded delivery without making the feature read as broken, and it has to survive being seen on every limited post rather than only the first. Worth testing on a real person rather than being written once and left.
- **Editing and deleting a limited-audience post.** `Update` and `Delete` need the same audience derivation and the same fan-out. Straightforward once the audience is stored, but it must not be forgotten, since a delete that reaches fewer people than the original post is worse than no delete at all.
- **`published_at` is unclamped** on both ingest paths, so a peer controls where its posts sort in the receiving feed. Pre-existing, not caused by this decision, still worth closing.
