# Federation Service

**Package:** `internal/federation`
**Files:** `internal/federation/federation.go`, `internal/federation/activitypub.go`, `internal/federation/relay.go`

Agora federates three ways, side by side:

1. **Standard ActivityPub** — talks to the real fediverse (Mastodon, Pleroma, Akkoma, etc.). This is the primary, actively-developed protocol and the subject of most of this document.
2. **Native AT Protocol** — Agora acts as its own PDS and talks directly to Bluesky/the AT Proto network, no bridge involved. Separate service, separate document: see [AT Protocol Service](atproto.md).
3. **Agora-to-Agora peering** on top of ActivityPub. There was once a custom Ed25519-signed protocol here; AGORA-330 deleted it and everything Agora-to-Agora now rides ActivityPub with Agora vocabulary where the standard has no equivalent. See [Agora-to-Agora peering](#agora-to-agora-peering) below and [ADR-002](../adr/002-agora-to-agora-federation-protocol.md).

## Constructor

```go
func NewService(db *store.DB, cfg *config.Config, notif *notifications.Service) *Service
```

`feed.Service` and `users.Service` do **not** get passed in — the historical signature `NewService(db, cfg, feed, users)` documented here previously is stale and hasn't matched the real code since AGORA-147. Instead, `feed.Service`/`users.Service`/`pages.Service` each declare their own small `fedSender` interface (satisfied structurally by `federation.Service`) and call `SetFed(f fedSender)` after construction — avoids an import cycle, and each caller only depends on the handful of methods it actually uses.

## Configuration

Two independent instance-wide toggles, both admin-settable in **Admin → Settings**:

| Setting | Governs | Default |
|---|---|---|
| `federation_enabled` | The Agora-native surface: instance info, peering, cross-instance handle lookup and search. Formerly also the legacy transport, which AGORA-330 removed. | off |
| `activitypub_enabled` | Standard ActivityPub | **on** (any value other than the literal string `"false"`) |

`activityPubEnabled()` defaults *on* deliberately — an instance that already had federation configured shouldn't silently lose fediverse discoverability the moment ActivityPub support shipped.

There's also a **per-account** opt-out: `users.activitypub_enabled` (default `true`). A user can turn this off in Settings → Privacy without affecting the instance-wide toggle — both must be true for that user's own posts to federate. Pages have their own equivalent column on the `pages` table, toggled from the page's own settings.

## ActivityPub: discovery

| Endpoint | Purpose |
|---|---|
| `GET /.well-known/webfinger?resource=acct:user@domain` | Resolves a handle to an actor URL (`WebFinger`). Checks users first, falls back to pages on a slug/username collision — user wins. |
| `GET /.well-known/host-meta` | XRD document pointing back at WebFinger (`HostMeta`). Some implementations still probe this before trying WebFinger directly. |
| `GET /.well-known/nodeinfo` → `GET /nodeinfo/2.0` | NodeInfo (AGORA-171) — software name/version, protocols, user count. Used by instance directories and Mastodon's own "About this server" panel, not by anything Agora itself calls. |
| `GET /federation/users/{handle}` | Content-negotiated: `Accept: application/activity+json` (or `application/ld+json`) returns the actor document (`writeActorObject`); anything else returns Agora's own flat-JSON profile (`GetUser`), which peers and the web client both read. |
| `GET /federation/pages/{slug}` | Actor document for a page. Always ActivityPub JSON, with no flat-JSON alternative (pages never had one). |
| `GET /federation/instance` | The **instance actor** (AGORA-219) — a single actor document representing the instance itself, not any one user. Used for instance-level operations that shouldn't be attributed to a specific admin: relay subscriptions (below) and delivery to instances that require every requester to be a signable actor. |
| `GET /federation/instance/outbox` | Always an empty `OrderedCollection` — the instance actor never authors content, only relay Follow/Undo handshakes. |

The actor document includes `publicKey` (RSA, PEM-encoded — see below), `inbox`, `outbox`, `followers`, and an `icon` if the account has an avatar.

## Per-actor RSA keys & HTTP Signatures

Every user and page actor has its own RSA keypair. (Agora once also had an instance-wide Ed25519 key for the legacy transport; AGORA-330 deleted both the transport and the key.) Stored PEM-encoded in `users.federation_public_key`/`federation_private_key` (and the equivalent `pages` columns), generated lazily on first use (`getOrCreateUserKeyPair`/`getOrCreatePageKeyPair`).

Every outbound POST (activity delivery) and every outbound GET that needs to survive an "authorized fetch" instance (Threads, `AUTHORIZED_FETCH` Mastodon — see below) is signed per [draft-cavage HTTP Signatures](https://datatracker.ietf.org/doc/html/draft-cavage-http-signatures), the scheme real-world ActivityPub implementations actually expect (not the newer RFC 9421). `signRequest`/`verifyInboundSignature`/`buildSigningString` are the shared machinery; `Inbox` verifies every inbound activity's signature before processing it — an unverified activity's `actor`/`attributedTo` fields are treated as untrusted and only `verifiedActor` (derived from the signature's keyId) is ever used for authorization decisions.

**Signed GET, not just signed POST:** some instances 404 an anonymous actor-document fetch. `fetchActorProfileSigned`/`fetchActorProfileSignedAsPage` sign the GET as a specific local user/page so those instances don't reject Agora's outbound follows and lookups. There's no unsigned fallback left in the codebase — the anonymous `fetchActorProfile` was removed once every call site had a real local user/page to sign as (follower-inbox resolution, `getOrCreateRemoteAPUser`'s cache-miss path, the `ListFollowing` stub-backfill loop).

## Inbound activities

`Inbox` (`POST /federation/inbox`) verifies the signature, then dispatches on `type`:

| Type | Handler | Effect |
|---|---|---|
| `Follow` | `handleInboundFollow` → `handleInboundFollowUser`/`handleInboundFollowPage` | Records the follower in `ap_followers`/`page_remote_subscribers`, replies with `Accept`. |
| `Undo(Follow)` | `handleInboundUndoFollow` | Removes the follower record. |
| `Block` | `handleInboundBlock` | Records the block in `ap_blocked_by` (keyed by inbox URL), auto-removes any `ap_following` row where the local user follows the blocker. |
| `Undo(Block)` | `handleInboundUndoBlock` | Removes the block record. |
| `Create` | `handleInboundCreate` | A followed account's top-level post (→ `ingestFollowedPost`) or a reply into a thread Agora owns. Parses image/video attachments. |
| `Update` | `handleInboundUpdate` | Refreshes a previously-ingested post's content/attachments/`edited_at`. Scoped to the post's author actually being the verified signer — a different actor can't edit someone else's ingested post. |
| `Delete` | `handleInboundAPDelete` | Soft-deletes a previously-ingested post (`deleted_at`), same ownership scoping as `Update`. Handles both Delete object shapes (bare id string or `Tombstone`). |
| `Like` | `handleInboundLike` | Writes to `reactions` (`reaction_type='like'`), not the legacy `likes` table. |
| `Undo(Like)` | `handleInboundUndoLike` | Removes the reaction. |
| `Announce` | `handleInboundAnnounce` | A remote repost of a local post — creates a local repost row. |
| `Undo(Announce)` | `handleInboundUndoAnnounce` | Removes it. |
| `Accept`/`Reject` (of a `Follow`) | `handleInboundAcceptFollow`/`handleInboundRejectFollow` | Confirms or clears a pending outbound follow in `ap_following`. |

**Instance blocking (unified in AGORA-177).** Two independent tables both express an instance-level block: `federated_instances.status = 'blocked'` (managed from Admin → Federation) and `instance_bans` (managed from Admin → Fediverse → Instance Bans). Before AGORA-177 these were checked inconsistently, and `instance_bans` in particular was **never actually enforced anywhere**. `isInstanceBlocked(domain)` (`federation.go`) now checks both tables with one call, and is consulted at every relevant point: inbound `Follow`, inbound replies, inbound `Like`/`Announce` target resolution, mention resolution, actor lookup, and outbound follow. Neither table needs a row in the other to take effect. `BanInstance` normalizes its input (strips scheme/trailing slash, lowercases) so a pasted URL and a bare domain hit the same row. The AT Proto side reuses `instance_bans` as its own PDS-host block scope; see [AT Protocol Service → Blocking](atproto.md#blocking).

## Outbound activities

Fire-and-forget goroutines, called from `feed`/`pages`/`users` via each package's own `fedSender` interface:

| Function | Fired by | Sends |
|---|---|---|
| `BroadcastPublicPost` | new public post | `Create` to followers, plus any resolved fediverse mentions |
| `BroadcastUpdatePost` | edited post | `Update`, same audience re-derived fresh |
| `BroadcastDeletePost` | deleted post | `Delete`/`Tombstone` |
| `DeliverReply` | new comment | `Create` addressed at the remote reply target and/or mentioned actors |
| `DeliverReplyUpdate` | edited comment | `Update`, same target/mention re-derivation as `DeliverReply` |
| `DeliverLike`/`DeliverUnlike` | like/unlike a remote-authored post | `Like`/`Undo(Like)` |
| `DeliverAnnounce`/`DeliverUnannounce` | repost/un-repost | `Announce`/`Undo(Announce)`, addressed at both the reposter's followers and the original author directly |
| `BroadcastPagePost`/`Update`/`Delete` | page post lifecycle | Same shapes, signed with the page's own key, delivered to `page_remote_subscribers` |

Every one of these re-derives current visibility/opt-out state at send time rather than trusting the caller (defense in depth — e.g. `BroadcastUpdatePost` re-checks `profile_private`/`activitypub_enabled` even though the original `Create` already passed that check once).

### Fediverse mentions

`resolveFediverseMentions(userID, content)` finds `@handle@instance.tld`-shaped mentions (capped at 5 per post — each is a live WebFinger + signed actor fetch), resolves them via the same machinery search/follow uses, and returns `Mention` tags plus extra delivery targets. Mentions **add** recipients on top of the normal Public/followers audience — they don't replace it, so a mention reaches its target even if that target isn't a follower or the reply's own parent.

### Delivery queue & blocking

`ap_delivery_queue` (users) / `page_ap_delivery_queue` (pages) hold pending deliveries; `drainAPQueue`/`drainPageAPQueue` process them with exponential backoff, abandoning after enough failed attempts. HTTP Signatures are computed at *send* time (a fresh `Date` header each attempt), not once at enqueue time.

`enqueueAPDelivery` — the single function every outbound path above funnels through — checks `ap_blocked_by` before queuing anything: if the destination inbox belongs to an actor who has blocked this local user, the send is silently skipped. This is the one central guard rather than a check duplicated at each call site.

## Quote posts (FEP-044f)

AGORA-255 adds outbound support for [FEP-044f](https://w3id.org/fep/044f) quote posts, the informal ActivityPub convention Mastodon 4.4+ uses. Every outbound `Note` (`buildNoteObject` — posts, comments, page posts, Outbox history) now carries an `interactionPolicy.canQuote` allowing anyone to quote it, matching the openness Agora already gives boosts. Inbound `QuoteRequest` activities are accepted and recorded, and `GET /federation/users/{handle}/posts/{postID}/quote-authorizations/{authID}` (`GetQuoteAuthorization`) serves a dereferenceable `QuoteAuthorization` object other servers verify against before rendering the quote. This is what lets a Mastodon user quote an Agora post instead of only being able to boost it.

For servers that don't implement FEP-044f at all, a quote-share still degrades gracefully: `quotedPostURL()` appends a `"RE: <url>"` fallback line to the outbound Note's content. When the quoted post originated on Bluesky, `remote_post_id` holds a raw AT-URI (`at://did:plc:.../app.bsky.feed.post/rkey`) — meaningless to a human and unlinkable by the plain-URL linkifier — so `blueskyATURIToWebURL()` (AGORA-262) converts it to `https://bsky.app/profile/{did}/post/{rkey}` before appending; a DID alone resolves fine on bsky.app without a separate handle lookup.

## Remote profile stats

Agora never tracks a remote account's actual social graph, so a fediverse profile's follower/following/post counts shown on its profile page are a **live fetch**, not cached columns: `GetRemoteActorStats(actorURL)` dereferences the actor's `followers`/`following`/`outbox` collection URLs and reads each one's `totalItems`, independently and best-effort per collection (AGORA-253). The AT Proto equivalent, `atproto.Service.GetRemoteActorStats(did)`, does the same job with a single `app.bsky.actor.getProfile` call.

## Fediverse relays

AGORA-219–223 add relay support, modeled on Mastodon's own admin Relays feature: an admin enters a relay's **inbox URL** directly (relay implementations vary too much for actor-URL resolution to be reliable), and Agora subscribes via the same "object: Public collection" convention most relay software (Mastodon's bundled relay, `activityrelay`, `aoderrelay`, ...) expects.

- **Instance actor** (AGORA-219, `GetInstanceActor`/`InstanceActorOutbox` above) — relay subscription is an instance-level concern, signed as the instance actor rather than any one admin's own account.
- **Subscription handshake** (AGORA-220): `relays` table tracks `inbox_url`, resolved `actor_url` (cached lazily on first successful profile fetch), and `status` (`pending → enabled`, or `rejected`/`disabled`). `AddRelay` sends a `Follow` from the instance actor; some relays never reply at all and just start forwarding, so `pending` is not treated as an error state. `DisableRelay`/`DeleteRelay` send `Undo(Follow)`.
- **Outbound** (AGORA-221): `enabledRelayInboxes()` adds every enabled relay's inbox as an extra recipient of a local public post's normal delivery (`BroadcastPublicPost`/`Update`/`Delete`), delivered signed as the post's own author — not the instance actor — since at least one popular relay implementation verifies a delivered activity's `attributedTo`/`actor` against the HTTP signature's own `keyId` the same strict way Agora's own inbound handling does.
- **Inbound** (AGORA-222): `Create`/`Announce` activities arriving from a subscribed relay are ingested the same way a directly-followed account's posts are.
- **Delivery queue**: the instance actor gets its own `instance_ap_delivery_queue`, mirroring `ap_delivery_queue`'s shape but with no per-user/per-page owner column — there's only ever one instance actor to sign as.
- **Admin routes**: `RegisterAdminRoutes` (`relay.go`) — see [Admin API → Relays](../api/admin.md#relays).

## Custom feeds integration

Two custom-feed filter types (AGORA-146) surface followed fediverse accounts through the existing custom-feeds engine rather than a dedicated timeline: `fediverse_account` (posts from one specific followed actor) and `fediverse_all` (posts from every followed actor). Per-viewer visibility for an ingested post is enforced at custom-feed query time, not at ingestion — a single ingested post is shared by every local follower of that actor.

## Following & notifications

- `FollowFediverseAccount`/`UnfollowFediverseAccount` (`internal/federation/activitypub.go`) — outbound `Follow`/`Undo(Follow)`, backed by `ap_following`.
- `ap_following.notify` (AGORA-166) — per-followed-account notification opt-in, default `false`. Following someone doesn't imply getting notified of their posts, same as a local profile follow. `ingestFollowedPost`'s notification loop requires both this **and** the account-level `users.fediverse_notifications_enabled` toggle (the global kill switch) to be true.
- `ToggleFollowNotify` — flips the per-account flag; surfaced both on the Fediverse follows list and directly on a followed account's own profile page (AGORA-167).

## Agora-to-Agora peering

Agora instances recognise each other and federate as peers, but they no longer speak a protocol of their own. There was one: Ed25519-signed with an instance-wide key, carrying `post`, `delete_post`, `friend_request`, `friend_accept` and `profile_update` activities. AGORA-330 deleted it. Everything Agora-to-Agora now rides ActivityPub, with Agora vocabulary only where ActivityPub has no equivalent. See [ADR-002](../adr/002-agora-to-agora-federation-protocol.md) for why.

What is left is identification and peering, which were never part of the transport:

- `GET /.well-known/agora-instance` (`InstanceInfo`) answers with this instance's name, description, rules and user count. Only Agora serves it, so answering it at all is how one instance recognises another as Agora, which NodeInfo does not establish with enough specificity. It no longer publishes a `public_key`: nothing signs with an instance-wide key any more, and `api_version` moved to `2` to say so.
- `FetchInstanceInfo(domain)` reads that endpoint behind the SSRF-safe dialer, and is what an admin adding a peer goes through.
- `registerInboundPeer(domain)` records a peer that contacted us first and notifies admins once. This used to be a side effect of fetching a peer's signing key to verify a legacy activity; with that gone it is called deliberately, from the ActivityPub activities that carry Agora vocabulary (a marked `Follow`, or a `Create` with an audience marker). Those are the traffic where peering means something, which keeps every Mastodon server that ever delivers here out of a list that means "Agora instances we federate with".
- `federated_instances` remains the peer list behind Admin → Federation: direction (AGORA-321), disconnect (AGORA-320), first-contact notification (AGORA-314), and the `peered_only` mode of `friend_requests_from`. Its `public_key` column is retained but no longer written.

**Legacy rows still exist.** Users cached over the old protocol are keyed on `(remote_user_id, remote_instance)` with no `ap_actor_url`, and may hold real friendships. They stay, and `remoteAgoraActorURL`/`remoteUserIDForActor` bridge them onto ActivityPub identity. `syncStaleRemoteUsers` still refreshes them through Agora's own `/federation/users/{handle}`, and since AGORA-330 is scoped to exactly those rows: an ActivityPub row has no `remote_user_id` and could never sync there.

**An inbound legacy activity** is no longer understood. It reaches `handleStandardInbox` and is refused as the unrecognised JSON it is, which is the intended outcome rather than a gap.

## Background workers

`StartBackgroundSync(ctx)` starts the delivery pollers: `drainAPQueue` (standard ActivityPub user deliveries), `drainPageAPQueue` (page deliveries) and `drainInstanceAPQueue` (relay traffic), plus `refreshInstances` (peer liveness) and `syncStaleRemoteUsers` (legacy stub profiles). Each delivery poller retries with backoff and gives up after `maxDeliveryAttempts` (10) failed attempts rather than retrying forever. The legacy protocol's own `drainQueue` went with it in AGORA-330.

**Delivery logging (AGORA-325, extended to the ActivityPub queue by AGORA-338).** `drainAPQueue` logs a peer's *first* failure and its eventual abandonment, and stays quiet for the retries in between so an instance that's down for a day doesn't produce a line per activity per attempt. Recording failures only to a `last_error` column, as both queues once did, makes a completely broken federation path indistinguishable from an idle one from outside the database. That is how AGORA-316 stayed hidden long enough for every legacy activity ever sent to be silently rejected, and the reason the ActivityPub queue was not left in the same state. The inbound side logs an activity that verifies but is then dropped (unknown type, unresolvable local user), for the same reason.
