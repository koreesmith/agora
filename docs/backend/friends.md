# Friends Service

**Package:** `internal/friends`
**File:** `internal/friends/friends.go`

Manages mutual friend requests and friend groups (named lists for post visibility targeting).

## Constructor

```go
func NewService(db *store.DB, notif *notifications.Service) *Service
func (s *Service) SetFed(fed fedSender)  // wire federation broadcaster
```

## Handlers

### `ListFriends(w, r)`
`GET /api/friends`

Returns all accepted friends for the authenticated user, sorted by display name.

**Response:** `[{ "id", "username", "display_name", "avatar_url", "is_remote", "remote_instance" }]`

### `ListRequests(w, r)`
`GET /api/friends/requests`

Returns `{"incoming": [...], "outgoing": [...]}`:
- `incoming` — users who sent a request to the authenticated user (status=`pending`, they are `requester`)
- `outgoing` — users the authenticated user has requested (status=`pending`, they are `addressee`)

### `SendRequest(w, r)`
`POST /api/friends/request/{userID}`

Creates a friendship record with `status='pending'`. Sends a `friend_request` notification. For federated users, broadcasts the request to the remote instance.

### `Accept(w, r)`
`POST /api/friends/accept/{userID}`

Sets `status='accepted'`. Sends `friend_accepted` notification to requester. Broadcasts to federation.

### `Decline(w, r)`
`POST /api/friends/decline/{userID}`

Deletes the friendship record.

### `Unfriend(w, r)`
`DELETE /api/friends/{userID}`

Deletes an accepted friendship record.

## Friend Groups

Friend groups are named lists used to target post visibility. A post with `visibility='group'` and a `friend_list_id` is visible only to members of that group.

### `ListGroups(w, r)`
`GET /api/friend-groups`

Returns all friend groups owned by the authenticated user with member counts.

### `CreateGroup(w, r)`
`POST /api/friend-groups`

**Body:** `{"name": "Close Friends"}`

### `DeleteGroup(w, r)`
`DELETE /api/friend-groups/{groupID}`

Deletes the group. Posts targeted at this group become inaccessible (no cascade to posts).

### `ListGroupMembers(w, r)`
`GET /api/friend-groups/{groupID}/members`

Returns members of the group (must be friends with the authenticated user).

### `AddToGroup(w, r)`
`POST /api/friend-groups/{groupID}/members/{friendID}`

Adds a friend to a group. The friend must be an accepted friend.

### `RemoveFromGroup(w, r)`
`DELETE /api/friend-groups/{groupID}/members/{friendID}`

## Friendship Status Values

| Status | Meaning |
|--------|---------|
| `pending` | Request sent, awaiting response |
| `accepted` | Mutual friendship established |
| `declined` | Request was declined (record deleted) |
| `blocked` | User is blocked (see blocks service) |
