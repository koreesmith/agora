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
    "comment_id": "uuid",
    "data": {},
    "created_at": "timestamp"
  }],
  "total": 0
}
```

**`comment_id` (AGORA-460).** Present on `comment_like`, `comment_reaction`, `comment_reply` and `post_comment` notifications: the comment the notification is about, so a client can open `post_id` at it (web: `/post/<post_id>?comment=<comment_id>`, app: `/post/<post_id>?commentId=<comment_id>`). `post_id` is always the post the comment is on, never the comment. It is absent when no comment was recorded: reaction, reply and comment notifications made before this existed, and a comment that has since been deleted. A like on a comment always has one (older ones too, since a like was stored against the comment). Likes and reactions on different comments of the same post are separate notifications; replies and comments on a post still merge into one, whose `comment_id` is the newest. The push notification's data carries the same `comment_id` (empty string if none).

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
