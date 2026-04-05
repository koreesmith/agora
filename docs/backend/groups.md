# Groups Service

**Package:** `internal/groups`
**File:** `internal/groups/groups.go`

Manages community groups — public or private discussion spaces with owner/mod/member roles.

## Constructor

```go
func NewService(db *store.DB, notif *notifications.Service) *Service
```

## Group Object

```json
{
  "id": "uuid",
  "name": "string",
  "slug": "string",
  "description": "string",
  "cover_url": "string",
  "cover_position": "string",
  "avatar_url": "string",
  "privacy": "public|private",
  "created_by": "uuid",
  "member_count": 0,
  "post_count": 0,
  "is_member": false,
  "member_role": "owner|mod|member",
  "created_at": "timestamp"
}
```

## Handlers

### `ListGroups(w, r)`
`GET /api/groups`

**Query params:**
- `q` — search query
- `filter` — `mine` (owned), `joined` (member), `public` (all public groups)
- `page`, `limit`

### `CreateGroup(w, r)`
`POST /api/groups`

Creator becomes the group `owner`.

**Body:**
```json
{
  "name": "string",
  "description": "string",
  "privacy": "public|private"
}
```

Slug is auto-generated from the name (lowercase, hyphens).

### `GetGroup(w, r)`
`GET /api/groups/{slug}`

Returns group details. Private groups return `403` for non-members.

### `UpdateGroup(w, r)`
`PATCH /api/groups/{slug}`

Owner or mod only. Accepts same fields as create, plus `cover_url`, `avatar_url`, `cover_position`.

### `DeleteGroup(w, r)`
`DELETE /api/groups/{slug}`

Owner only. Deletes all posts, members, and invites.

### `ListMembers(w, r)`
`GET /api/groups/{slug}/members`

### `MemberSearch(w, r)`
`GET /api/groups/{slug}/member-search?q=...`

Search existing members by username/display name.

### `Join(w, r)`
`POST /api/groups/{slug}/join`

For public groups: joins immediately. For private groups: creates a join request.

### `Leave(w, r)`
`DELETE /api/groups/{slug}/leave`

Owner cannot leave (must transfer ownership or delete group).

### `SetMemberRole(w, r)`
`PATCH /api/groups/{slug}/members/{userID}/role`

Owner or mod only. **Body:** `{"role": "mod|member"}`

### `RemoveMember(w, r)`
`DELETE /api/groups/{slug}/members/{userID}`

Owner or mod only.

### `AddMemberByUsername(w, r)`
`POST /api/groups/{slug}/members/add`

**Body:** `{"username": "string"}`

### `GetFeed(w, r)`
`GET /api/groups/{slug}/feed`

Returns posts in the group, newest first. Members only for private groups.

### `CreatePost(w, r)`
`POST /api/groups/{slug}/posts`

Creates a post scoped to the group. Members only.

## Invites

### `ListInvites(w, r)`
`GET /api/groups/{slug}/invites`

### `CreateInvite(w, r)`
`POST /api/groups/{slug}/invites`

**Body:** `{"max_uses": 0}` (0 = unlimited)

Returns `{"token": "...", "url": "..."}`.

### `RevokeInvite(w, r)`
`DELETE /api/groups/{slug}/invites/{token}`

## Join Requests (private groups)

### `RequestJoin(w, r)`
`POST /api/groups/{slug}/request`

**Body:** `{"message": "optional message"}`

### `ListJoinRequests(w, r)`
`GET /api/groups/{slug}/requests`

Owner or mod only.

### `ApproveRequest(w, r)` / `RejectRequest(w, r)`
`POST /api/groups/{slug}/requests/{requestID}/approve`
`POST /api/groups/{slug}/requests/{requestID}/reject`
