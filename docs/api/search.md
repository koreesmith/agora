# Search API

All endpoints require `Authorization: Bearer <token>`.

---

## `GET /api/search/users`

**Query params:**
- `q` (required) — search string
- `scope` — `local` (default) or `federated`
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
- `q` (required) — search string
- `page` (int, default 1)
- `limit` (int, default 20)

**Response 200:** `{"posts": [...post objects...], "total": 0}`

Searches post `content` with full-text matching. Respects all visibility rules.
