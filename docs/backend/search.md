# Search Service

**Package:** `internal/search`
**File:** `internal/search/search.go`

Local full-text search for users, posts, and hashtags using PostgreSQL's `pg_trgm` extension. This service only ever searches **local** data plus whatever remote content is already cached from ingestion — it has no network-wide reach of its own. The frontend's unified Search page (AGORA-217) additionally calls `internal/atproto`'s endpoints directly for genuine network-wide Bluesky search, and simply has no equivalent for the fediverse, which has no network-wide search API to call. See [AT Protocol API → search](../api/atproto.md) and [Search API](../api/search.md#bluesky-network-wide-search-not-part-of-this-service) for that half.

## Constructor

```go
func NewService(db *store.DB) *Service
```

## Handlers

### `SearchUsers(w, r)`
`GET /api/search/users?q=...&scope=...`

Searches `username` and `display_name` using `ILIKE`. `scope=local` (default) restricts to non-remote users; `scope=federated` also matches cached fediverse/Bluesky user rows already ingested onto this instance — **not** a live query against the fediverse or Bluesky network (see below). Filters out:
- Users who have blocked the caller
- Users blocked by the caller
- Banned users

**Query params:**
- `q` — search string
- `scope` — `local` (default) or `federated`
- `page`, `limit` (default 30)

**Response:**
```json
[{
  "id": "uuid",
  "username": "string",
  "display_name": "string",
  "avatar_url": "string",
  "is_remote": false,
  "remote_instance": "string"
}]
```

### `SearchPosts(w, r)`
`GET /api/search/posts?q=...`

Full-text search on post content, or an exact hashtag match if `q` is `#`-prefixed (AGORA-213/214 — see below). Respects visibility rules and friendship:
- Only `public` posts for unauthenticated users
- `friends` posts visible if mutual friendship exists
- `private` posts never returned (author's own posts excepted)

Since AGORA-214, results are no longer restricted to locally-authored posts — an already-ingested fediverse or Bluesky post (`is_remote=true`) matches too, without needing a separate visibility rule: both ingestion paths only ever pull in public content, so the existing scoping already covers it correctly.

**Hashtag search** (`hashtagFromQuery`): a bare `#tag` query is matched as a case-insensitive **exact** lookup against the `post_hashtags` table (populated from AT Proto richtext facets and, where extracted, ActivityPub content — see [AT Protocol Service](atproto.md#inbound-at-proto--agora-posts)), never a content substring — an `ILIKE` on `#games` would both miss `#Games` and false-positive on a word like "average".

**Query params:** `q`, `page`, `limit`

**Response:** Array of [post objects](../backend/feed.md)

### `SearchPages(w, r)`
`GET /api/search/pages?q=...`

Searches Page `name`/`slug` using `ILIKE`.

**Query params:** `q`, `page`, `limit`

**Response:** `{"pages": [...]}` — see [Pages](../user/pages.md) for the Page concept.

## Bluesky network-wide search is a different service

The endpoints above only ever search **local data plus whatever remote content this instance has already ingested** — there's no live query against Mastodon or Bluesky here. The frontend's unified Search page additionally calls two `internal/atproto` endpoints directly for genuine network-wide reach against the real Bluesky AppView (`app.bsky.actor.searchActors` / `app.bsky.feed.searchPosts`) — see [AT Protocol Service → AppView reads](atproto.md#appview-reads) and [AT Protocol API](../api/atproto.md). There is no fediverse equivalent: ActivityPub has no network-wide search API to call, so a fediverse account can only ever be found by its exact handle (via `GET /federation/ap-lookup`, see [Federation API](../api/federation.md)) or if it's already been cached here.
