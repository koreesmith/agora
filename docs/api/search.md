# Search API

All endpoints require `Authorization: Bearer <token>`.

The frontend's unified Search page (`/search`) presents three tabs — **People / Posts / Pages** — each split into "On Agora", "On the Fediverse" (cached remote rows only, see below), and "On Bluesky" (genuinely live network-wide results). Within this file, every endpoint searches **local data plus already-cached remote content only**; live network-wide Bluesky search is a separate API — see [Bluesky network-wide search](#bluesky-network-wide-search) below.

---

## `GET /api/search/users`

**Query params:**
- `q` (required) — search string
- `scope` — `local` (default, excludes remote users) or `federated` (also matches already-cached fediverse/Bluesky user rows — not a live network query)
- `page` (int, default 1)
- `limit` (int, default 30)

**Response 200:**
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

Uses PostgreSQL `ILIKE` with trigram index for fast substring matching on `username` and `display_name`.

Excludes: blocked users, banned users.

---

## `GET /api/search/posts`

**Query params:**
- `q` (required) — search string. A bare `#tag` value switches this to an exact, case-insensitive hashtag lookup instead of a content substring match (AGORA-213/214).
- `page` (int, default 1)
- `limit` (int, default 20)

**Response 200:** `{"posts": [...post objects...], "total": 0}`

Searches post `content` with full-text matching, or `post_hashtags` for a `#tag` query. Respects all visibility rules. Includes already-ingested fediverse/Bluesky posts (`is_remote=true`) alongside local ones.

---

## `GET /api/search/pages`

**Query params:** `q` (required), `page`, `limit`

**Response 200:** `{"pages": [...]}`

Searches Page `name`/`slug`.

---

## Bluesky network-wide search

Unlike everything above, these two endpoints are genuinely live and network-wide — they call the Bluesky AppView directly, not just Agora's own cache:

- `GET /api/atproto/search/actors?q=...`
- `GET /api/atproto/search/posts?q=...`

See [AT Protocol API](atproto.md) for request/response shapes. There is no fediverse equivalent — ActivityPub has no network-wide search API — so the closest a fediverse account gets is `GET /federation/ap-lookup?handle=...` ([Federation API](federation.md)), an exact-handle resolve, not a search.
