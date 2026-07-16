# Federation API

Two protocols share this surface: **standard ActivityPub** (talks to Mastodon and the rest of the real fediverse) and a **legacy custom Agora-to-Agora protocol** (older, only talks to other Agora instances, not extended further). Public endpoints don't require auth — signature verification (HTTP Signatures for ActivityPub, Ed25519 for the legacy protocol) substitutes for it.

---

## ActivityPub: public endpoints

### `GET /.well-known/webfinger?resource=acct:user@domain`

Resolves a handle to an actor URL. Checks local users first, falls back to pages on a slug/username collision.

**Response 200:**
```json
{
  "subject": "acct:alice@agora.example.com",
  "aliases": ["https://agora.example.com/federation/users/alice"],
  "links": [
    { "rel": "http://webfinger.net/rel/profile-page", "type": "text/html", "href": "https://agora.example.com/profile/alice" },
    { "rel": "self", "type": "application/activity+json", "href": "https://agora.example.com/federation/users/alice" }
  ]
}
```
**Response 404** if no matching user or page, or the domain doesn't match this instance.

---

### `GET /.well-known/host-meta`

XRD document pointing back at the WebFinger endpoint. `Content-Type: application/xrd+xml`.

---

### `GET /.well-known/nodeinfo`

NodeInfo discovery document (AGORA-171).

**Response 200:**
```json
{ "links": [{ "rel": "http://nodeinfo.diaspora.software/ns/schema/2.0", "href": "https://agora.example.com/nodeinfo/2.0" }] }
```

### `GET /nodeinfo/2.0`

**Response 200:**
```json
{
  "version": "2.0",
  "software": { "name": "agora", "version": "2.0.0" },
  "protocols": ["activitypub"],
  "services": { "inbound": [], "outbound": [] },
  "openRegistrations": true,
  "usage": { "users": { "total": 42 } },
  "metadata": {}
}
```

---

### `GET /federation/users/{handle}`

Content-negotiated. `Accept: application/activity+json` (or `application/ld+json`) returns the ActivityPub actor document; anything else returns the legacy flat-JSON profile.

**Actor document (200):**
```json
{
  "@context": ["https://www.w3.org/ns/activitystreams", "https://w3id.org/security/v1"],
  "id": "https://agora.example.com/federation/users/alice",
  "type": "Person",
  "preferredUsername": "alice",
  "name": "Alice",
  "summary": "bio text",
  "inbox": "https://agora.example.com/federation/inbox",
  "outbox": "https://agora.example.com/federation/users/alice/outbox",
  "followers": "https://agora.example.com/federation/users/alice/followers",
  "url": "https://agora.example.com/profile/alice",
  "publicKey": { "id": "...#main-key", "owner": "...", "publicKeyPem": "-----BEGIN PUBLIC KEY-----..." },
  "icon": { "type": "Image", "url": "https://agora.example.com/uploads/avatars/alice.jpg" }
}
```
**Response 404** if the user doesn't exist, is private, or has `activitypub_enabled = false`.

### `GET /federation/users/{handle}/outbox`

A paginated `OrderedCollection` of the user's public posts as `Create` activities.

### `GET /federation/users/{handle}/followers`

A `Collection` of the user's followers (count + first page of actor URLs).

### `GET /federation/pages/{slug}`, `.../outbox`, `.../followers`

Same three shapes, for a page's own actor — always ActivityPub JSON, no legacy fallback.

---

### `POST /federation/inbox`

Receives activities from remote instances — **shared by both protocols**, routed internally based on the body shape (see `docs/backend/federation.md`).

**Standard ActivityPub body**, HTTP-Signature-signed (`Signature`/`Digest` headers, draft-cavage):
```json
{ "@context": "...", "id": "...", "type": "Follow|Undo|Create|Update|Delete|Like|Announce|Block|Accept|Reject", "actor": "...", "object": { ... } }
```
Handled types: `Follow`, `Undo(Follow|Like|Announce|Block)`, `Create`, `Update`, `Delete`, `Like`, `Announce`, `Block`, `Accept(Follow)`, `Reject(Follow)`.

**Legacy custom-protocol body**, see below.

**Response 202** on acceptance (both protocols always report success once past signature verification — a not-applicable activity is a silent no-op, not an error, since remote redelivery/unknown-type activities are expected traffic, not client mistakes). **Response 401** if signature invalid. **Response 403** if the sending instance is blocked. **Response 404** if the relevant protocol isn't enabled instance-wide.

---

## ActivityPub: authenticated endpoints (require an Agora session)

### `GET /federation/lookup?handle=user@instance.com`

Resolves a remote handle via the legacy protocol's own instance-info exchange (distinct from `ap-lookup` below).

### `GET /federation/ap-lookup?handle=user@instance.com`

Resolves a standard fediverse handle or profile URL via WebFinger + a signed actor fetch — the search/preview step before following someone.

**Response 200:**
```json
{ "actor_url": "...", "inbox": "...", "preferred_username": "...", "name": "...", "instance": "...", "icon_url": "...", "summary": "..." }
```
**Response 404** if the handle can't be resolved (unknown instance, no such user, or the fetch itself fails — including on an authorized-fetch instance if signing somehow fails).

### `POST /federation/follow`

**Body:** `{ "actor_url": "https://..." }`

Sends an outbound `Follow`. Eagerly creates a local stub for the remote actor so they immediately appear in the `fediverse_account` custom-feed picker even before their first post arrives.

**Response 201:** `{ "message": "follow requested" }`. **Response 403** if the target instance is admin-blocked. **Response 404** if the actor can't be resolved.

### `DELETE /federation/follow/{id}`

Sends `Undo(Follow)` and removes the local follow record.

### `GET /federation/following`

Lists the caller's fediverse follows.

**Response 200:**
```json
{
  "following": [
    {
      "id": "...", "actor_url": "...", "accepted": true, "notify": false, "created_at": "...",
      "user_id": "...", "username": "...", "display_name": "...", "avatar_url": "...", "instance": "..."
    }
  ]
}
```

### `PUT /federation/follow/{id}/notify`

**Body:** `{ "notify": true }`

Flips per-account notification opt-in (AGORA-166) — independent of the account-wide `fediverse_notifications_enabled` setting, which remains the all-accounts kill switch.

**Response 200:** `{ "notify": true }`. **Response 404** if the follow doesn't belong to the caller.

### `GET /federation/search?q=...`

Searches local public users — used by remote instances resolving who to friend under the legacy protocol.

---

## Legacy Agora-to-Agora protocol

### `GET /.well-known/agora-instance`

Public instance metadata, Ed25519 public key. Used by remote *Agora* instances to discover this one before verifying its signed activities.

**Response 200:**
```json
{
  "domain": "agora.example.com",
  "name": "My Agora Instance",
  "description": "string",
  "public_key": "base64-encoded Ed25519 public key",
  "api_version": "1",
  "user_count": 42,
  "software": "agora",
  "rules": ["Be kind", "No spam"]
}
```

### Legacy `POST /federation/inbox` body

```json
{
  "type": "post|delete_post|friend_request|friend_accept|profile_update",
  "actor": "username@remote.instance",
  "instance": "remote.instance",
  "public_key": "base64",
  "timestamp": "ISO8601",
  "data": { ... }
}
```
**Headers:** `X-Agora-Signature: <base64 Ed25519 signature over the raw request body>`.

### Legacy activity data payloads

**`post`**
```json
{ "id": "remote-post-id", "content": "string", "image_url": "string", "created_at": "timestamp" }
```

**`delete_post`**
```json
{ "post_id": "remote-post-id" }
```

**`friend_request`** / **`friend_accept`**
```json
{ "from_handle": "user@remote.instance", "to_username": "local_username" }
```

**`profile_update`**
```json
{ "display_name": "string", "bio": "string", "avatar_url": "string" }
```
