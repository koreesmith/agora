# AT Protocol Service

**Package:** `internal/atproto`
**Files:** `atproto.go`, `repo.go`, `sync.go`, `firehose.go`, `relay.go`, `ingest.go`, `follow.go`, `post.go`, `reply.go`, `interaction.go`, `reactions.go`, `thread.go`, `search.go`, `blob.go`, `blockstore.go`, `blocklist.go`, `car.go`, `labels.go`, `persist.go`, `bridge.go`, `session.go`

The v3.0.0 counterpart to [`internal/federation`](federation.md)'s ActivityPub layer (AGORA-184): instead of bridging to Bluesky, every Agora account **is** a real AT Protocol account. Agora acts as its own [PDS](https://atproto.com/guides/glossary#pds-personal-data-server) (Personal Data Server) — every user has their own signed, append-only repo, a real DID, and posts/likes/reposts/follows are genuine `app.bsky.*` records a Bluesky relay can crawl and any AppView can index, with no bridge account in between.

This package is deliberately the **only** one that imports [`github.com/bluesky-social/indigo`](https://github.com/bluesky-social/indigo) directly — every other package deals only in this package's own types, so swapping the underlying AT Proto library stays contained to one package plus a migration.

## Constructor

```go
func NewService(db *store.DB, cfg *config.Config, notif *notifications.Service) *Service
```

Same structural-interface pattern federation uses to avoid an import cycle: `users.Service` declares its own `atprotoSyncer` interface (`SyncProfile`, `FollowsMe`, `GetRemoteActorStats`) and `feed.Service` declares `atprotoSender` (`BroadcastPost*`, `DeliverReply*`, `DeliverLike/Unlike/Announce/Unannounce`), each satisfied structurally by `*atproto.Service` and wired up with `SetAtproto(...)` after construction in `cmd/server/main.go`.

## Configuration

| Setting | Governs | Default |
|---|---|---|
| `instance_settings['atproto_enabled']` | Whether this instance speaks AT Proto at all | **off** (`'false'`, unlike `activitypub_enabled` which defaults on — no instance has AT Proto configured out of the box) |
| `users.atproto_enabled` | Per-account opt-out | `true` |

Both the instance-wide and per-account flags must be true for a given user's content to federate over AT Proto; every write path re-checks both at send time rather than trusting a cached decision (same defense-in-depth federation uses). A retraction (unlike/unrepost/delete) is **not** gated on either flag — it should still propagate after opt-out, same reasoning as ActivityPub's `Undo` handling.

Other `instance_settings` keys, all admin-overridable, none seeded (absence = default):

| Key | Default | Purpose |
|---|---|---|
| `atproto_relay_host` | `bsky.network` | Where `requestCrawl` registers this PDS |
| `atproto_appview_host` | `public.api.bsky.app` | Anonymous AppView used for profile/feed/thread/like/repost/search reads |
| `atproto_bot_pds_host` | `bsky.social` | PDS the search bot account logs into |

**Env vars** (`internal/config`): `ATPROTO_BOT_HANDLE` / `ATPROTO_BOT_APP_PASSWORD` — an optional dedicated Bluesky bot account. Left blank, everything works except `app.bsky.feed.searchPosts` (network-wide post/hashtag search), which the AppView 403s for anonymous callers.

## Identity: did:web, not did:plc

Every user gets a per-subdomain `did:web`, not the `did:plc` a real bsky.social account would have:

```go
func didForUsername(instanceDomain, username string) string {
	return "did:web:" + username + "." + domainFromURL(instanceDomain)
}
```

- `GET /.well-known/did.json`, resolved by `Host` header — serves the DID document (Multikey verification method over secp256k1, `alsoKnownAs: ["at://username.domain"]`, a `service` entry of type `AtprotoPersonalDataServer`).
- `GET /.well-known/atproto-did` — the bare DID as plain text, the reverse-direction half of AT Proto's mutual handle↔DID verification.
- Real deployment needs wildcard-subdomain DNS/nginx routing so `username.instance.tld` actually resolves (AGORA-186 — see [Deployment](../deployment.md)); a spoofed `Host` header stands in for it below the nginx layer in dev.
- The bare instance also has its own server-level identity: `DescribeServer` returns `did:web:<domain>` for the host itself, separate from any per-user DID.
- Signing key: secp256k1 (not the RSA keypair ActivityPub actors use), hex-encoded and stored in `users.atproto_private_key`, lazily generated on first use (`getOrCreateSigningKey`).
- Eligibility for every identity/sync endpoint (`eligibleUser`/`eligibleUserByDID`): not remote, `profile_private = false`, `atproto_enabled = true`, no deletion scheduled — plus the instance-wide toggle.

## The repo: commits, the firehose, and why writes are single-writer

Each user's AT Proto repo is a signed Merkle Search Tree of records, advanced one **commit** at a time. Every commit both updates the user's repo head and appends an event to Agora's own outbound firehose — and because a relay reconstructs a repo purely by chaining each commit's `since` field to the previous commit's `rev`, **two commits racing off the same head fork the chain**, after which a relay silently stops applying that user's commits with no error surfaced anywhere (see the AGORA-244 postmortem below).

Three things enforce single-writer safety:

1. **Per-user commit lock.** `Service.repoLocks sync.Map` + `lockRepo(userID)` is taken with `defer s.lockRepo(userID)()` at the top of all ten repo-mutating entry points (`SyncProfile`, `BroadcastPost`, `BroadcastPostUpdate`, `BroadcastPostDelete`, `DeliverReply`, `DeliverReplyUpdate`, `deliverInteraction`, `undoInteraction`, `followBlueskyActor`, `UnfollowBlueskyAccount`).
2. **One transaction covers the commit.** `commitAndPersist` (`repo.go`) signs the pending write → builds the firehose `#commit` event → writes it to `atproto_firehose_events` **in the same DB transaction** that advances `users.atproto_repo_head`/`atproto_repo_rev` → commits → only *then* broadcasts to live `subscribeRepos` subscribers. A stored head ahead of the firehose would make the next commit's `since` cite a rev no subscriber ever saw; a firehose ahead of the head would fork on the very next write.
3. **Durable, replayable firehose log.** `atproto_firehose_events` (keyed by a Postgres sequence) is the backing store for `pgEventPersister`, which implements indigo's `events.EventPersistence`. `SubscribeRepos` resumes a reconnecting relay from its own `cursor` query param via `Playback`, rather than replaying everything or dropping missed events.

### Postmortem: AGORA-244

Before this, every write path ran as a detached goroutine doing an unsynchronized read-modify-write of the repo head. AGORA-233 (avatar/cover sync) started firing `SyncProfile` on every photo upload, so a profile sync racing a post commit — or two photo edits racing each other — could produce two commits reading the same head and emitting `#commit` events with the same `since`. The relay forks and gives up on that repo's commit chain silently. Symptom: **a user's profile keeps updating fine (the AppView re-fetches the profile singleton directly via `listRecords`, independent of firehose continuity) while their posts simply stop reaching Bluesky, with nothing in any log to explain why.** If you're debugging a "my posts don't show up on Bluesky anymore" report, this is the first thing to suspect — check whether `users.atproto_repo_head` still matches the tip the firehose log actually emitted.

## Outbound: Agora content → AT Proto records

| Agora concept | Record | Notes |
|---|---|---|
| Public top-level post | `app.bsky.feed.post` | `BroadcastPost` (`post.go`) |
| Post edit | Same record, `repo.UpdateRecord` in place | No AP-style `Update` activity — a record at an existing path is just overwritten |
| Post delete | `repo.DeleteRecord` | No Tombstone object; the firehose commit itself records the deletion |
| Comment/reply | `app.bsky.feed.post` with `Reply.Parent` **and** `Reply.Root` strong-refs | Unlike ActivityPub's single `inReplyTo`, AT Proto requires both the immediate parent and the thread root on every reply |
| Like / repost | `app.bsky.feed.like` / `app.bsky.feed.repost`, `Subject` = strong-ref | `deliverInteraction` |
| Unlike / unrepost | `repo.DeleteRecord` | `undoInteraction` — not gated on the opt-out toggle |
| Native Bluesky follow | `app.bsky.graph.follow` | `followBlueskyActor` — unilateral, no accept/reject the way ActivityPub Follow has |
| Profile | `app.bsky.actor.profile`, fixed rkey `"self"` | `SyncProfile` — a singleton, needs `PutRecord` on first write and `UpdateRecord` thereafter since neither call is a true upsert |
| Post images (≤4) | `app.bsky.embed.images`, each a content-addressed blob | `blob.go` |
| Avatar / banner | Same blob path, into the profile record's `Avatar`/`Banner` | Cover photos have no field on the lightweight `ProfileViewBasic` shape most read paths see — only the detailed `getProfile` response carries a banner, so `followBlueskyActor` opportunistically caches it at follow time, the one point a detailed profile is fetched |
| Content warning | A custom self-label `agora-content-warning` | `labels.go` — deliberately not one of Bluesky's built-in adult-content categories, since Agora's CW is free text unrelated to those fixed categories |
| Over-length post | Truncated, with `"…\n\n" + permalink` appended | Bluesky's AppView silently drops (never indexes) any `app.bsky.feed.post.text` over 300 graphemes, with no error surfaced back — `truncateForBluesky` avoids that fate entirely rather than erroring (AGORA-256) |

## Inbound: AT Proto → Agora posts

Agora does **not** subscribe to the wider Bluesky network's own firehose — consuming `subscribeRepos` network-wide would mean parsing every commit on Bluesky just to catch the handful of accounts a given instance's users follow. Instead, `StartBlueskyIngestion` runs a poller (`ingest.go`) on a 5-minute ticker per followed account:

| Poll | Calls | Effect |
|---|---|---|
| Author feed | `app.bsky.feed.getAuthorFeed` | New top-level posts from a followed DID → `posts` row (`is_remote=true`, `remote_instance='bsky.app'`, `remote_post_id`/`remote_post_cid` = the record's URI/CID) |
| Thread replies | `app.bsky.feed.getPostThread` | Bluesky-side replies to a broadcast post, ingested into the same comment tree |
| Likes | `app.bsky.feed.getLikes`, diffed against `reactions` | Written to `reactions` (`reaction_type='like'`), not the legacy `likes` table |
| Reposts | `app.bsky.feed.getRepostedBy`, diffed | A synthetic `posts` row with `repost_of_id` set; `remote_post_id = "bsky-repost:" + postID + ":" + did` since `getRepostedBy` carries no per-record ref to dedupe against |

Embed handling (`storeInboundEmbed` and friends):

| Bluesky embed | Agora storage |
|---|---|
| `app.bsky.embed.images` | `posts.image_url` / `post_photos` |
| `app.bsky.embed.video` | `posts.video_url`/`video_thumb_url` (an HLS `.m3u8` playlist, not an mp4) |
| `app.bsky.embed.external` | `posts.link_url/link_title/link_description/link_image/link_domain` — Bluesky has no distinct GIF embed type, so a GIF picked from Bluesky's own composer is just an external link embed too |
| `app.bsky.embed.record` (quote post) | The quoted post is ingested as a real local post and `repost_of_id` is set to it — one level deep only, no recursive quote-of-quote resolution |
| `#tag` richtext facets | `post_hashtags` rows, lowercased and deduped |
| Self-labels | Best-effort reverse-mapped to `posts.content_warning` — Agora's own label surfaces as `"content warning"`, any other label (including real Bluesky adult-content categories) surfaces as `"Bluesky content label: <val>"`. Not lossless: the original free-text CW never round-trips, only "a warning existed" does |

A cached remote account gets a `users` stub row keyed by `users.atproto_remote_did` (stable across handle changes, unlike a handle itself), created on first ingested content — the same "no local row until the first interaction" caveat ActivityPub actors have.

## Firehose: serving `subscribeRepos`

`GET /xrpc/com.atproto.sync.subscribeRepos` (`firehose.go`) is a websocket, publicly reachable with no origin check (`CheckOrigin` always returns true — a relay is expected to connect cross-origin). Resumes from a `cursor` query param via `pgEventPersister.Playback`. Since AGORA-243, connect (with cursor) and disconnect (with duration and event count) are both logged — previously totally silent, which made it impossible to tell whether `bsky.network` was still actually subscribed or had quietly dropped off.

Backfill for pre-existing history (a relay only tails *new* commits from the moment it subscribes): `com.atproto.sync.getRepo` (full CARv1 export), `getLatestCommit` (cheap `{cid, rev}` poll), `getBlocks` (specific CIDs), `listRepos` (cursor-paginated DID discovery across every eligible user on this instance).

## Relay registration

`requestCrawl` (`relay.go`) calls `com.atproto.sync.requestCrawl` against `atproto_relay_host`. `StartRelayCrawl` requests it on startup with exponential backoff on failure (capped at 24h) and reconfirms every 6h on success — there's no other way to detect a silently-dropped subscription — and polls the instance-wide toggle every cycle so disabling AT Proto actually stops crawl requests without a restart. `com.atproto.server.describeServer` exists specifically because a relay probes it before accepting a crawl request to confirm the host actually speaks the PDS protocol.

## AppView reads

Nearly everything Agora reads *from* the wider Bluesky network — profile resolution, author-feed/thread/likes/reposts polling, account search — goes through the **public anonymous AppView** (`atproto_appview_host`, default `public.api.bsky.app`). Agora has no local index of the Bluesky network; there's no equivalent of a fediverse-wide search here, because AT Proto's AppView already provides one.

The one exception is `app.bsky.feed.searchPosts` (network-wide post/hashtag search), which 403s anonymous callers. `SearchBlueskyPosts` instead authenticates as the optional dedicated bot account (`authedAppviewClient`, `session.go`) — session tokens cached in `instance_settings`, refreshed on 401. Without `ATPROTO_BOT_HANDLE`/`ATPROTO_BOT_APP_PASSWORD` configured, this one search feature is simply unavailable; everything else still works.

## Blob storage

Images/avatars/banners are uploaded as content-addressed blobs (`blob.go`) and served back via `GET /xrpc/com.atproto.sync.getBlob?did=...&cid=...`. Backed by `pgBlockstore` (`blockstore.go`), a Postgres-backed IPLD blockstore over `atproto_blocks` — the same table that holds every MST/commit block for a user's repo, not a separate media store.

## Blocking

`blocklist.go` is the single enforcement point every inbound ingestion path (posts, replies, likes/reposts, search, follow) checks, from each path's very first version — a deliberate contrast with the fediverse side, where instance blocking was found to be inconsistently enforced only after the fact (AGORA-148/177, see [Federation](federation.md)).

- `isDIDBlocked(did)` — checks `blocked_dids`, AT Proto's natural blockable unit since a DID identifies one specific account rather than an instance.
- `isInstanceBlocked(domain)` — reused as the PDS-host block scope, comparing against a Bluesky handle's own domain (the closest available proxy, since there's no direct PDS-host resolution machinery here). This is the *same* `instance_bans` table the ActivityPub side manages — a domain block is protocol-agnostic at the transport layer.
- `isBlueskyActorBlocked(did, handle)` — the combined check called at every inbound path.

## Rate limiting

Purely nginx-layer, no Go-level rate limiting for AT Proto endpoints specifically (contrast the invite-email endpoint, which does use `httprate` in `cmd/server/main.go`):

| Zone | Rate | Covers |
|---|---|---|
| `atproto` | 120r/m, burst 20 | `= /.well-known/did.json`, `= /.well-known/atproto-did` (exact-match so they don't fall into federation's `/.well-known/` prefix zone), and the general `/xrpc/` prefix |
| `firehose` | 10r/m, burst 5 | `= /xrpc/com.atproto.sync.subscribeRepos` only — governs new websocket *connections*, not traffic within one already-open stream, which `limit_req` can't gate |

`atproto`'s zone is sized more generously than federation's `60r/m` since legitimate AppView/relay polling can be more frequent than ActivityPub inbox delivery.

## Bridgy Fed migration

Before native Bluesky support existed, Agora users could already reach Bluesky accounts indirectly by following their [Bridgy Fed](https://fed.brid.gy/) `*.brid.gy` bridged actor over ActivityPub. `bridge.go` lets a user reconcile such a follow into a native AT Proto one: `ListBridgedBlueskyFollows` finds `ap_following` rows pointed at a bridged actor, `MigrateBridgedFollow` resolves the underlying DID and creates the equivalent `at_following` row (leaving the original bridged follow in place — migration doesn't unfollow the bridge).

## Background workers

Both started from `cmd/server/main.go`:

```go
go atprotoSvc.StartRelayCrawl(context.Background())       // relay.go — requestCrawl + periodic reconfirmation
go atprotoSvc.StartBlueskyIngestion(context.Background())  // ingest.go — per-followed-account polling (posts/replies/likes/reposts)
```

`StartBlueskyIngestion` polls unconditionally regardless of the local `atproto_enabled` toggle — reading from the public AppView is harmless either way, only outbound writes are gated.

## Known fix history worth knowing about

- **AGORA-240** — `listRepos` 500'd on every call with no `cursor` (i.e. the relay's normal first-page case) from the moment it shipped, because `($1 = '' OR id > $1)` still type-checks `id > $1` against a `uuid` column even when the empty-cursor branch short-circuits true in application logic — Postgres doesn't skip type-checking a branch SQL's `OR` didn't need to evaluate. Fixed by branching the query in Go instead of relying on SQL to skip the comparison.
- **AGORA-241** — `com.atproto.repo.listRecords`/`getRecord` didn't exist at all (404 on every call) until this fix — the missing piece behind `app.bsky.actor.profile` (a mutable singleton an AppView re-fetches directly rather than trusting firehose continuity for) never actually indexing.
- **AGORA-244** — see the single-writer postmortem above.
- **AGORA-256** — see the truncation row in the outbound table above.
