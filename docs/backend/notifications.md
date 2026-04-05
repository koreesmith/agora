# Notifications Service

**Package:** `internal/notifications`
**File:** `internal/notifications/notifications.go`

Handles in-app notifications and SMTP email notifications.

## Constructors

```go
func NewEmailService(db *store.DB, cfg *config.Config) *EmailService
func NewService(db *store.DB, email *EmailService) *Service
```

## Notification Types

| Type | Trigger |
|------|---------|
| `friend_request` | Someone sent you a friend request |
| `friend_accepted` | Your friend request was accepted |
| `post_like` | Someone liked your post |
| `post_comment` | Someone commented on your post |
| `post_mention` | You were @mentioned in a post or comment |
| `comment_like` | Someone liked your comment |
| `comment_reply` | Someone replied to your comment |
| `new_report` | Content was reported (admins only) |
| `post_update` | A user you follow via post-notify published a post |

## Internal Functions (called by other services)

### `Create(userID, actorID, notifType, postID, data)`
Creates an in-app notification. Does not send email.

### `SendEmail(userID, subject, htmlBody)`
Sends an SMTP email to the user **if**:
- `SMTP_ENABLED=true`
- User's `email_notifications_enabled=true`
- User's email is verified

### `SendEmailVerification(userID, email, displayName, token)`
Sends the email verification link. Always sent regardless of `email_notifications_enabled`.

## Handlers

### `List(w, r)`
`GET /api/notifications`

**Query params:**
- `page` (int)
- `limit` (int, default 20, max 100)

**Response:**
```json
{
  "notifications": [{
    "id": "uuid",
    "type": "string",
    "read": false,
    "actor": { "id", "username", "display_name", "avatar_url" },
    "post_id": "uuid",
    "data": {},
    "created_at": "timestamp"
  }],
  "total": 0
}
```

### `UnreadCount(w, r)`
`GET /api/notifications/unread-count`

**Response:** `{"count": 5}`

### `MarkRead(w, r)`
`POST /api/notifications/{id}/read`

### `MarkAllRead(w, r)`
`POST /api/notifications/read-all`

### `GetEmailPrefs(w, r)`
`GET /api/notifications/email-preferences`

**Response:** `{"email_notifications_enabled": true}`

### `UpdateEmailPrefs(w, r)`
`PUT /api/notifications/email-preferences`

**Body:** `{"email_notifications_enabled": true}`

### `OneClickUnsubscribe(w, r)`
`POST /api/notifications/unsubscribe`

Called by email clients that support RFC 8058 one-click unsubscribe. Sets `email_notifications_enabled=false`. The `unsubscribe_token` is passed as a form field.

### `UnsubscribePage(w, r)`
`GET /api/notifications/unsubscribe?token=...`

Returns a simple HTML page confirming unsubscription. Suitable for a browser link in email footers.
