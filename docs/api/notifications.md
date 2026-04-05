# Notifications API

All endpoints require `Authorization: Bearer <token>` unless noted.

---

## `GET /api/notifications`
**Query params:** `page` (int), `limit` (int, default 20)

**Response 200:**
```json
{
  "notifications": [{
    "id": "uuid",
    "type": "friend_request|friend_accepted|post_like|post_comment|post_mention|comment_like|comment_reply|new_report|post_update",
    "read": false,
    "actor": { "id", "username", "display_name", "avatar_url" },
    "post_id": "uuid",
    "data": {},
    "created_at": "timestamp"
  }],
  "total": 0
}
```

## `GET /api/notifications/unread-count`
**Response 200:** `{"count": 5}`

## `POST /api/notifications/{id}/read`
**Response 204**

## `POST /api/notifications/read-all`
**Response 204**

## `GET /api/notifications/email-preferences`
**Response 200:** `{"email_notifications_enabled": true}`

## `PUT /api/notifications/email-preferences`
**Body:** `{"email_notifications_enabled": true}`
**Response 200:** `{"email_notifications_enabled": true}`

## `POST /api/notifications/unsubscribe` (no auth)
One-click unsubscribe from email notifications (RFC 8058).

**Body:** `unsubscribe_token=<token>` (form-encoded)
**Response 200**

## `GET /api/notifications/unsubscribe?token=...` (no auth)
Returns an HTML confirmation page for unsubscribing via browser link.
