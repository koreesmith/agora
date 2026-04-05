# Blocks Service

**Package:** `internal/blocks`
**File:** `internal/blocks/blocks.go`

Mutual user blocking. When A blocks B, neither can see the other's content.

## Constructor

```go
func New(db *store.DB) *Service
```

## Handlers

### `ListBlocks(w, r)`
`GET /api/blocks`

Returns users the authenticated user has blocked.

**Response:** `[{ "id", "username", "display_name", "avatar_url", "blocked_at" }]`

### `Block(w, r)`
`POST /api/blocks/{username}`

Blocks the user. Side effects:
- Removes any existing friendship between the two users
- Removes each from the other's friend groups

### `Unblock(w, r)`
`DELETE /api/blocks/{username}`

## Effect on Other Services

Blocks are checked throughout the application:
- Profile: blocked users see a `404`
- Feed: posts from blocked users are hidden
- Search: blocked users excluded from results
- Friend requests: blocked users cannot send requests
- DMs: blocked users cannot start conversations
