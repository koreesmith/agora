# AT Protocol API

Two audiences share this surface: **XRPC/well-known endpoints** dereferenced by real Bluesky relays, AppViews, and clients (public, no Agora auth — AT Proto's own signature/DID machinery substitutes for it), and **`/api/atproto/*` endpoints** called only by Agora's own authenticated frontend. See [AT Protocol Service](../backend/atproto.md) for the architecture behind all of this, and [Federation API](federation.md) for the parallel ActivityPub surface.

All of this is gated on `instance_settings['atproto_enabled']` (instance-wide) — a 404 from any endpoint below likely means AT Proto isn't enabled on this instance.

---

## Identity & well-known

### `GET /.well-known/did.json`

Resolved by the `Host` header — either the per-user subdomain (`username.instance.tld`) or a verified custom domain the user has claimed (AGORA-283, see [Custom Domains API](custom-domains.md)). Serves that user's DID document.

**Response 200:**
```json
{
  "@context": ["https://www.w3.org/ns/did/v1", "https://w3id.org/security/multikey/v1"],
  "id": "did:web:alice.agora.example.com",
  "alsoKnownAs": ["at://alice.agora.example.com"],
  "verificationMethod": [{
    "id": "did:web:alice.agora.example.com#atproto",
    "type": "Multikey",
    "controller": "did:web:alice.agora.example.com",
    "publicKeyMultibase": "z..."
  }],
  "service": [{
    "id": "#atproto_pds",
    "type": "AtprotoPersonalDataServer",
    "serviceEndpoint": "https://agora.example.com"
  }]
}
```
**Response 404** if the host doesn't resolve to an eligible user (not remote, not private, `atproto_enabled=true`, no deletion scheduled).

A user with a live custom domain (AGORA-282) gets it as an additional `alsoKnownAs` entry, listed **first** since AT Proto reads the leading entry as the primary handle:

```json
"alsoKnownAs": ["at://alice.example", "at://alice.agora.example.com"]
```

The `id` is unaffected — a custom domain is a verified alias, not a DID migration, and the instance-issued handle stays published alongside it. An entry appears only while the claim is both `verification_status = 'verified'` and `approval_status = 'approved'`; a lapsed or unapproved claim simply isn't listed, which is what makes the fallback to the instance handle automatic.

### `GET /.well-known/atproto-did`

**Response 200** (`text/plain`): the bare DID, e.g. `did:web:alice.agora.example.com`. Used by AT Proto's own mutual handle/DID verification. Resolves for a verified custom domain `Host` as well as the per-user subdomain.

---

## XRPC: server & sync (public)

### `GET /xrpc/com.atproto.server.describeServer`

Confirms this host speaks the PDS protocol — probed by relays before they'll accept a `requestCrawl`.

**Response 200:**
```json
{ "did": "did:web:agora.example.com", "availableUserDomains": ["agora.example.com"] }
```

### `GET /xrpc/com.atproto.sync.subscribeRepos?cursor=<seq>`

Websocket. Streams repo commit events (`#commit`) for every eligible user's repo on this instance. Omit `cursor` to start from the current tip; pass a previously-received sequence number to resume without gaps (backed by the durable `atproto_firehose_events` log).

### `GET /xrpc/com.atproto.sync.getRepo?did=...`

Full repo export as a CARv1 byte stream (`Content-Type: application/vnd.ipld.car`) — used for backfill, not incremental sync.

### `GET /xrpc/com.atproto.sync.getLatestCommit?did=...`

**Response 200:** `{ "cid": "...", "rev": "..." }` — a cheap poll to decide whether a full re-sync is needed.

### `GET /xrpc/com.atproto.sync.getBlocks?did=...&cids=cid1,cid2,...`

Specific MST/commit blocks by CID, as a CAR byte stream.

### `GET /xrpc/com.atproto.sync.listRepos?cursor=&limit=`

Cursor-paginated discovery of every eligible DID on this (multi-tenant) host. `limit` defaults 500, max 1000.

**Response 200:**
```json
{ "repos": [{ "did": "did:web:alice.agora.example.com", "head": "cid...", "rev": "..." }], "cursor": "..." }
```

### `GET /xrpc/com.atproto.sync.getBlob?did=...&cid=...`

Raw blob bytes (an image) by `(did, cid)`, content-type inferred from the stored blob.

### `GET /xrpc/com.atproto.repo.listRecords?repo=<did>&collection=<nsid>&limit=&cursor=&reverse=`

Records in a collection (e.g. `app.bsky.feed.post`), paginated. `limit` defaults 500, max 1000.

**Response 200:**
```json
{ "records": [{ "uri": "at://did:web:.../app.bsky.feed.post/3k...", "cid": "...", "value": { "$type": "app.bsky.feed.post", "text": "..." } }], "cursor": "..." }
```

### `GET /xrpc/com.atproto.repo.getRecord?repo=<did>&collection=<nsid>&rkey=<rkey>`

A single record. **Response 404** if the record doesn't exist.

---

## `/api/atproto/*` (Agora's own frontend — requires `Authorization: Bearer <token>`)

### `GET /api/atproto/lookup?handle=user.bsky.social`

Resolves a Bluesky handle or DID to a live profile preview — the search/preview step before following.

**Response 200:**
```json
{ "did": "did:plc:...", "handle": "user.bsky.social", "display_name": "...", "avatar_url": "...", "bio": "..." }
```
**Response 404** if it can't be resolved.

### `GET /api/atproto/search/actors?q=...`

Network-wide account search via the AppView (`app.bsky.actor.searchActors`) — not limited to already-cached accounts.

**Response 200:** `{ "actors": [{ "did", "handle", "display_name", "avatar_url", "bio" }] }`

### `GET /api/atproto/search/posts?q=...`

Network-wide post/hashtag search via the AppView (`app.bsky.feed.searchPosts`). **Response 404/503** if no bot account is configured (`ATPROTO_BOT_HANDLE`/`ATPROTO_BOT_APP_PASSWORD`) — this one endpoint requires an authenticated AppView session, unlike every other read in this file.

**Response 200:** `{ "posts": [{ "uri", "cid", "author": {...}, "text", "created_at" }] }`

### `POST /api/atproto/follow`

**Body:** `{ "did": "did:plc:..." }` or `{ "handle": "user.bsky.social" }`

Writes an `app.bsky.graph.follow` record. Eagerly creates a local stub row so the account can immediately be added to a Friend List or custom feed filter, even before their first post is ingested.

**Response 201:** `{ "id": "...", "message": "follow created" }`. **Response 403** if the target DID or its handle's domain is admin-blocked.

### `DELETE /api/atproto/follow/{id}`

Deletes the `app.bsky.graph.follow` record and the local follow row.

### `GET /api/atproto/following`

**Response 200:**
```json
{
  "following": [{
    "id": "...", "remote_did": "...", "remote_handle": "...", "display_name": "...", "avatar_url": "...",
    "notify": false, "show_in_feed": false, "follows_back": false, "created_at": "...",
    "user_id": "..."
  }]
}
```
`follows_back` comes from a live `app.bsky.graph.getRelationships` AppView call, not a stored column — AT Proto has no inbound-follow delivery to a local inbox the way ActivityPub does.

### `PUT /api/atproto/follow/{id}/notify`

**Body:** `{ "notify": true }` — per-account opt-in to notifications on new posts. Independent of the account-wide `atproto_notifications_enabled` setting, which is the all-accounts kill switch.

### `PUT /api/atproto/follow/{id}/show-in-feed`

**Body:** `{ "show_in_feed": true }` — per-account opt-in to appear in the plain main feed (as opposed to only a custom feed built around this account). Off by default.

### `GET /api/atproto/bridged-follows`

Lists the caller's ActivityPub follows that resolve to a [Bridgy Fed](https://fed.brid.gy/)-bridged Bluesky account (`*.brid.gy`), as candidates for migration to a native follow.

### `POST /api/atproto/bridged-follows/{id}/migrate`

Resolves the bridged follow's underlying DID and creates the equivalent native `app.bsky.graph.follow`. Does **not** unfollow the bridged ActivityPub actor — that's left in place.

**Response 201:** `{ "id": "...", "message": "migrated" }`. **Response 4xx** with an error body on a failed resolve — the frontend surfaces this inline per-row rather than failing silently (AGORA-237).

---

## Moderation (require `role=moderator` or `role=admin`)

### `GET /api/moderation/blocked-dids`

**Response 200:** `{ "blocks": [{ "id", "did", "reason", "notes", "created_at", "blocked_by" }] }`

### `POST /api/moderation/blocked-dids`

**Body:** `{ "did": "did:plc:...", "reason": "string", "notes": "string" }`

Blocks a specific Bluesky account (by DID) network-wide on this instance — enforced at every inbound ingestion path (posts, replies, likes/reposts, search results) and against outbound follow.

**Response 201:** `{ "id": "...", "message": "DID blocked" }`

### `DELETE /api/moderation/blocked-dids/{id}`

**Response 200:** `{ "message": "DID unblocked" }`

> PDS-host/domain-level blocking for AT Proto reuses the **same** `instance_bans` list the ActivityPub side manages — see [`POST /api/moderation/instance-bans`](moderation.md), which accepts a bare domain regardless of protocol.

---

## Relays (admin only, `/api/admin/relays`)

Not AT Proto-specific — Agora's own outbound `requestCrawl` registration with `bsky.network` (or another relay host) is configured via `instance_settings`, not this table. This section is the ActivityPub relay-subscription feature; see [Admin API](admin.md#relays) and [Federation Service](../backend/federation.md) for `RegisterAdminRoutes`.
