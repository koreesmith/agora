# Direct Messages Service

**Package:** `internal/dm`
**File:** `internal/dm/dm.go`

Real-time direct messaging between users, delivered via WebSocket.

## Constructor

```go
func New(db *store.DB) *Service
```

Internally creates a `Hub` that manages WebSocket connections and broadcasts.

## Conversation Object

```json
{
  "id": "uuid",
  "participants": [{
    "user_id": "uuid",
    "username": "string",
    "display_name": "string",
    "avatar_url": "string",
    "last_read_at": "timestamp",
    "read_receipts": true
  }],
  "last_message": { "message object" },
  "unread_count": 0,
  "is_accepted": true,
  "updated_at": "timestamp"
}
```

## Message Object

```json
{
  "id": "uuid",
  "conversation_id": "uuid",
  "sender_id": "uuid",
  "sender_username": "string",
  "sender_display_name": "string",
  "sender_avatar_url": "string",
  "content": "string",
  "image_url": "string",
  "reactions": [{ "type": "string", "count": 0, "reacted": false }],
  "edited_at": "timestamp",
  "deleted_at": "timestamp",
  "created_at": "timestamp"
}
```

## Handlers

### `ListConversations(w, r)`
`GET /api/conversations`

Returns all conversations the user is in, sorted by `updated_at DESC`.

### `StartConversation(w, r)`
`POST /api/conversations`

Creates a new conversation (or returns existing one) with another user. The recipient sees it as a "message request" (`is_accepted=false`) until they accept.

**Body:** `{"user_id": "uuid"}`

### `FriendSearch(w, r)`
`GET /api/conversations/friend-search?q=...`

Search friends to start a conversation with.

### `GetConversation(w, r)`
`GET /api/conversations/{id}`

### `GetMessages(w, r)`
`GET /api/conversations/{id}/messages`

Paginated, newest first. **Query params:** `before` (cursor timestamp), `limit` (default 50).

### `SendMessage(w, r)`
`POST /api/conversations/{id}/messages`

**Body:** `{"content": "string", "image_url": "string"}`

Stores the message and broadcasts it via WebSocket to all participants' active connections.

### `EditMessage(w, r)`
`PATCH /api/messages/{id}`

**Body:** `{"content": "string"}`. Sets `edited_at`.

### `DeleteMessage(w, r)`
`DELETE /api/messages/{id}`

Soft-deletes (sets `deleted_at`). Content replaced with placeholder in UI.

### `ReactMessage(w, r)` / `UnreactMessage(w, r)`
`POST /api/messages/{id}/react` / `DELETE /api/messages/{id}/react`

**Body (react):** `{"type": "string"}`

### `MarkRead(w, r)`
`POST /api/conversations/{id}/read`

Updates `last_read_at` for the caller.

### `AcceptRequest(w, r)`
`POST /api/conversations/{id}/accept`

Sets `is_accepted=true` for the recipient.

### `LeaveConversation(w, r)`
`DELETE /api/conversations/{id}`

Sets `left_at`. User no longer receives new messages.

## WebSocket

### `WebSocket(w, r)`
`GET /api/ws`

Upgrades the connection. Requires JWT via `?token=` query param (WebSocket clients cannot set headers).

The Hub sends JSON messages to connected clients:
```json
{
  "type": "new_message|message_edited|message_deleted|message_reaction|conversation_read",
  "conversation_id": "uuid",
  "data": { ... }
}
```
