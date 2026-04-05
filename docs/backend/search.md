# Search Service

**Package:** `internal/search`
**File:** `internal/search/search.go`

Local full-text search for users and posts using PostgreSQL's `pg_trgm` extension.

## Constructor

```go
func NewService(db *store.DB) *Service
```

## Handlers

### `SearchUsers(w, r)`
`GET /api/search/users?q=...&scope=...`

Searches `username` and `display_name` using `ILIKE`. Filters out:
- Users who have blocked the caller
- Users blocked by the caller
- Banned users

**Query params:**
- `q` — search string
- `scope` — `local` (default) or `federated` (also queries remote instances)
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

Full-text search on post content. Respects visibility rules and friendship:
- Only `public` posts for unauthenticated users
- `friends` posts visible if mutual friendship exists
- `private` posts never returned (author's own posts excepted)

**Query params:** `q`, `page`, `limit`

**Response:** Array of [post objects](../backend/feed.md)
