# DM API

All endpoints require `Authorization: Bearer <token>`.

---

## Conversations

### `GET /api/conversations`
**Response 200:** `[...conversation objects...]`

### `POST /api/conversations`
Start a new conversation (or return existing one).
**Body:** `{"user_id": "uuid"}`
**Response 201:** Conversation object

### `GET /api/conversations/friend-search?q=...`
Search friends to start a DM with.
**Response 200:** `[{ "id", "username", "display_name", "avatar_url" }]`

### `GET /api/conversations/{id}`
**Response 200:** Conversation object

### `POST /api/conversations/{id}/accept`
Accept a message request.
**Response 204**

### `DELETE /api/conversations/{id}`
Leave the conversation.
**Response 204**

### `POST /api/conversations/{id}/read`
Mark all messages as read.
**Response 204**

---

## Messages

### `GET /api/conversations/{id}/messages`
**Query params:** `before` (ISO8601 cursor), `limit` (default 50)
**Response 200:** `[...message objects... (newest first)]`

### `POST /api/conversations/{id}/messages`
**Body:** `{"content": "string", "image_url": "string"}`
**Response 201:** Message object. Also broadcasts via WebSocket.

### `PATCH /api/messages/{id}`
Edit a message. Sender only.
**Body:** `{"content": "string"}`
**Response 200:** Updated message object

### `DELETE /api/messages/{id}`
Soft-delete. Sender only.
**Response 204**

### `POST /api/messages/{id}/react`
**Body:** `{"type": "string"}`
**Response 200:** Updated reactions

### `DELETE /api/messages/{id}/react`
**Response 200:** Updated reactions

---

## WebSocket

### `GET /api/ws?token=<jwt>`

Upgrades to WebSocket. JWT must be passed as `?token=` query parameter (browsers cannot set headers for WebSocket connections).

**Server → Client messages:**
```json
{
  "type": "new_message",
  "conversation_id": "uuid",
  "data": { "message object" }
}
```
```json
{
  "type": "message_edited|message_deleted|message_reaction",
  "conversation_id": "uuid",
  "data": { ... }
}
```
```json
{
  "type": "conversation_read",
  "conversation_id": "uuid",
  "data": { "user_id": "uuid", "read_at": "timestamp" }
}
```

---

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
