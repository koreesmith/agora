# Federation Service

**Package:** `internal/federation`
**Files:** `internal/federation/federation.go`, `internal/federation/activitypub.go`

Agora federates two ways, side by side:

1. **Standard ActivityPub** — talks to the real fediverse (Mastodon, Pleroma, Akkoma, etc.). This is the primary, actively-developed protocol and the subject of most of this document.
2. **A legacy custom Agora-to-Agora protocol** — Ed25519-signed, pre-dates ActivityPub support, and only ever talks to other Agora instances. Still live (see [Legacy protocol](#legacy-agora-to-agora-protocol) below) but not being extended further.

## Constructor

```go
func NewService(db *store.DB, cfg *config.Config, notif *notifications.Service) *Service
```

`feed.Service` and `users.Service` do **not** get passed in — the historical signature `NewService(db, cfg, feed, users)` documented here previously is stale and hasn't matched the real code since AGORA-147. Instead, `feed.Service`/`users.Service`/`pages.Service` each declare their own small `fedSender` interface (satisfied structurally by `federation.Service`) and call `SetFed(f fedSender)` after construction — avoids an import cycle, and each caller only depends on the handful of methods it actually uses.

## Configuration

Two independent instance-wide toggles, both admin-settable in **Admin → Settings**:

| Setting | Governs | Default |
|---|---|---|
| `federation_enabled` | The legacy custom protocol | off |
| `activitypub_enabled` | Standard ActivityPub | **on** (any value other than the literal string `"false"`) |

`activityPubEnabled()` defaults *on* deliberately — an instance that already had federation configured shouldn't silently lose fediverse discoverability the moment ActivityPub support shipped.

There's also a **per-account** opt-out: `users.activitypub_enabled` (default `true`). A user can turn this off in Settings → Privacy without affecting the instance-wide toggle — both must be true for that user's own posts to federate. Pages have their own equivalent column on the `pages` table, toggled from the page's own settings.

## ActivityPub: discovery

| Endpoint | Purpose |
|---|---|
| `GET /.well-known/webfinger?resource=acct:user@domain` | Resolves a handle to an actor URL (`WebFinger`). Checks users first, falls back to pages on a slug/username collision — user wins. |
| `GET /.well-known/host-meta` | XRD document pointing back at WebFinger (`HostMeta`). Some implementations still probe this before trying WebFinger directly. |
| `GET /.well-known/nodeinfo` → `GET /nodeinfo/2.0` | NodeInfo (AGORA-171) — software name/version, protocols, user count. Used by instance directories and Mastodon's own "About this server" panel, not by anything Agora itself calls. |
| `GET /federation/users/{handle}` | Content-negotiated: `Accept: application/activity+json` (or `application/ld+json`) returns the actor document (`writeActorObject`); anything else returns the legacy flat-JSON profile (`GetUser`). |
| `GET /federation/pages/{slug}` | Actor document for a page — always ActivityPub JSON, no legacy fallback (pages never had one). |

The actor document includes `publicKey` (RSA, PEM-encoded — see below), `inbox`, `outbox`, `followers`, and an `icon` if the account has an avatar.

## Per-actor RSA keys & HTTP Signatures

Every user and page actor has its own RSA keypair — **not** the instance-wide Ed25519 key the legacy protocol uses. Stored PEM-encoded in `users.federation_public_key`/`federation_private_key` (and the equivalent `pages` columns), generated lazily on first use (`getOrCreateUserKeyPair`/`getOrCreatePageKeyPair`).

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

An admin-blocked instance (`federated_instances.status = 'blocked'`) is checked before `Follow` and `Create` are processed — a blocked instance can't gain a new follower or inject content, on top of whatever per-actor block exists.

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

## Custom feeds integration

Two custom-feed filter types (AGORA-146) surface followed fediverse accounts through the existing custom-feeds engine rather than a dedicated timeline: `fediverse_account` (posts from one specific followed actor) and `fediverse_all` (posts from every followed actor). Per-viewer visibility for an ingested post is enforced at custom-feed query time, not at ingestion — a single ingested post is shared by every local follower of that actor.

## Following & notifications

- `FollowFediverseAccount`/`UnfollowFediverseAccount` (`internal/federation/activitypub.go`) — outbound `Follow`/`Undo(Follow)`, backed by `ap_following`.
- `ap_following.notify` (AGORA-166) — per-followed-account notification opt-in, default `false`. Following someone doesn't imply getting notified of their posts, same as a local profile follow. `ingestFollowedPost`'s notification loop requires both this **and** the account-level `users.fediverse_notifications_enabled` toggle (the global kill switch) to be true.
- `ToggleFollowNotify` — flips the per-account flag; surfaced both on the Fediverse follows list and directly on a followed account's own profile page (AGORA-167).

## Legacy Agora-to-Agora protocol

Kept for backwards compatibility with older Agora instances, not extended further. Ed25519-signed, instance-wide key (not per-actor), `X-Agora-Signature` header over the raw request body.

```go
func (s *Service) InstanceInfo(w, r)      // GET /.well-known/agora-instance
func (s *Service) Inbox(w, r)             // POST /federation/inbox — shared by BOTH protocols. A
                                           // cheap probe of the body ("@context" present, or
                                           // type is "Follow"/"Undo") routes to the standard
                                           // ActivityPub path (handleStandardInbox) before the
                                           // legacy Ed25519 verification ever runs — a standard
                                           // activity has no "signature"/"instance_id" fields and
                                           // would otherwise always fail that check.
func (s *Service) BroadcastToFriendInstances(userID, activity)
func (s *Service) SendToUserInstance(remoteInstance, instanceURL, activity)
```

Legacy activity types: `post`, `delete_post`, `friend_request`, `friend_accept`, `profile_update` — see `docs/api/federation.md` for payload shapes. Verification (`verifyActivity`) fetches the remote instance's Ed25519 public key from its own `/.well-known/agora-instance` and caches it in `federated_instances`.

## Background workers

`StartBackgroundSync(ctx)` starts three independent pollers: `drainQueue` (legacy protocol retries), `drainAPQueue` (standard ActivityPub user deliveries), `drainPageAPQueue` (page deliveries). Each retries with backoff and gives up after enough failed attempts, logging the failure rather than retrying forever.
