# Friends API

All endpoints require `Authorization: Bearer <token>`.

---

## `GET /api/friends`
List accepted friends.
**Response 200:** `[{ "id", "username", "display_name", "avatar_url", "is_remote", "remote_instance" }]`

## `GET /api/friends/requests`
**Response 200:**
```json
{
  "incoming": [...],
  "outgoing": [...]
}
```

## `POST /api/friends/request/{userID}`
Send a friend request.
**Response 200:** `{"status": "pending"}`
**Errors:** `409` request already exists

## `POST /api/friends/accept/{userID}`
Accept an incoming request.
**Response 200:** `{"status": "accepted"}`

## `POST /api/friends/decline/{userID}`
Decline an incoming request.
**Response 204**

## `DELETE /api/friends/{userID}`
Remove an accepted friend.
**Response 204**

---

## Friend Groups

### `GET /api/friend-groups`
**Response 200:** `[{ "id", "name", "member_count", "created_at" }]`

### `POST /api/friend-groups`
**Body:** `{"name": "Close Friends"}`
**Response 201:** `{ "id", "name", "created_at" }`

### `DELETE /api/friend-groups/{groupID}`
**Response 204**

### `GET /api/friend-groups/{groupID}/members`
**Response 200:** `[{ "id", "username", "display_name", "avatar_url" }]`

### `POST /api/friend-groups/{groupID}/members/{friendID}`
Add friend to group. Must be accepted friends.
**Response 204**

### `DELETE /api/friend-groups/{groupID}/members/{friendID}`
**Response 204**
