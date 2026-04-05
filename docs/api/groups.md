# Groups API

All endpoints require `Authorization: Bearer <token>`.

---

## `GET /api/groups`
**Query params:** `q` (search), `filter` (mine|joined|public), `page`, `limit`
**Response 200:** `{"groups": [...], "total": 0}`

## `POST /api/groups`
**Body:** `{"name": "string", "description": "string", "privacy": "public|private"}`
**Response 201:** Group object

## `GET /api/groups/{slug}`
**Response 200:** Group object
**Errors:** `403` private group, not a member

## `PATCH /api/groups/{slug}`
Owner/mod only. Accepts: `name`, `description`, `privacy`, `cover_url`, `avatar_url`, `cover_position`
**Response 200:** Updated group object

## `DELETE /api/groups/{slug}`
Owner only.
**Response 204**

## `GET /api/groups/{slug}/members`
**Response 200:** `[{ "user_id", "username", "display_name", "avatar_url", "role", "joined_at" }]`

## `GET /api/groups/{slug}/member-search?q=...`
**Response 200:** `[{ "user_id", "username", "display_name", "avatar_url", "role" }]`

## `POST /api/groups/{slug}/join`
Public groups: joins immediately. Private groups: creates a join request.
**Response 200:** `{"status": "joined|requested"}`

## `DELETE /api/groups/{slug}/leave`
**Response 204**

## `PATCH /api/groups/{slug}/members/{userID}/role`
Owner/mod only. **Body:** `{"role": "mod|member"}`
**Response 204**

## `DELETE /api/groups/{slug}/members/{userID}`
Owner/mod only. **Response 204**

## `POST /api/groups/{slug}/members/add`
**Body:** `{"username": "string"}`
**Response 204**

## `GET /api/groups/{slug}/feed`
**Query params:** `page`, `limit`
**Response 200:** `{"posts": [...], "total": 0}`

## `POST /api/groups/{slug}/posts`
**Body:** Same as `POST /api/posts` (community_group_id is set automatically)
**Response 201:** Post object

---

## Invites

### `GET /api/groups/{slug}/invites`
**Response 200:** `[{ "token", "max_uses", "use_count", "created_by", "expires_at" }]`

### `POST /api/groups/{slug}/invites`
**Body:** `{"max_uses": 0}` (0 = unlimited)
**Response 201:** `{"token": "...", "url": "..."}`

### `DELETE /api/groups/{slug}/invites/{token}`
**Response 204**

---

## Join Requests (private groups)

### `POST /api/groups/{slug}/request`
**Body:** `{"message": "optional"}`
**Response 200:** `{"status": "requested"}`

### `GET /api/groups/{slug}/requests`
Owner/mod only. **Response 200:** `[{ "id", "user", "message", "status", "created_at" }]`

### `POST /api/groups/{slug}/requests/{requestID}/approve`
**Response 204**

### `POST /api/groups/{slug}/requests/{requestID}/reject`
**Response 204**

---

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
