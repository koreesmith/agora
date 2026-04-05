# Moderation Service

**Package:** `internal/moderation`
**File:** `internal/moderation/moderation.go`

Content reports, user suspension, banning, and instance bans.

## Constructor

```go
func NewService(db *store.DB, notif *notifications.Service) *Service
```

## Report Object

```json
{
  "id": "uuid",
  "violation_type": "string",
  "details": "string",
  "rule_id": "uuid",
  "rule_text": "string",
  "status": "pending|reviewed|dismissed|actioned",
  "reporter_username": "string",
  "reported_user_username": "string",
  "reported_post_id": "uuid",
  "post_content": "string",
  "review_notes": "string",
  "reviewed_by": "string",
  "reviewed_at": "timestamp",
  "created_at": "timestamp"
}
```

## Handlers

### `CreateReport(w, r)`
`POST /api/reports`

Any authenticated user can report content.

**Body:**
```json
{
  "reported_user_id": "uuid",
  "reported_post_id": "uuid",
  "reported_comment_id": "uuid",
  "violation_type": "string",
  "details": "string",
  "rule_id": "uuid"
}
```

Sends a `new_report` notification to all admins/moderators.

### `ListReports(w, r)`
`GET /api/moderation/reports`

Moderator/admin only. **Query params:** `status` (pending|reviewed|dismissed|actioned).

### `ReviewReport(w, r)`
`POST /api/moderation/reports/{id}/review`

**Body:** `{"status": "reviewed|dismissed|actioned", "review_notes": "string"}`

### `ListModeratedUsers(w, r)`
`GET /api/moderation/users`

**Query params:** `filter` (suspended|banned)

### `SuspendUser(w, r)`
`POST /api/moderation/users/{userID}/suspend`

Sets `is_suspended=true`. User can still log in but sees a suspension notice.

**Body:** `{"reason": "string"}`

### `UnsuspendUser(w, r)`
`POST /api/moderation/users/{userID}/unsuspend`

### `BanUser(w, r)`
`POST /api/moderation/users/{userID}/ban`

Permanent ban. User is logged out and cannot log back in.

**Body:** `{"reason": "string"}`

### `UnbanUser(w, r)`
`POST /api/moderation/users/{userID}/unban`

### `ListInstanceBans(w, r)`
`GET /api/moderation/instance-bans`

### `BanInstance(w, r)`
`POST /api/moderation/instance-bans`

**Body:** `{"domain": "bad.instance.com", "reason": "string"}`

Blocks all federation traffic from the given domain.

### `UnbanInstance(w, r)`
`DELETE /api/moderation/instance-bans/{id}`
