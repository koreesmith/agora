# Moderation Service

**Package:** `internal/moderation`
**File:** `internal/moderation/moderation.go`

Content reports, user suspension, banning, fediverse instance bans, and AT Proto DID blocklists.

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

**Body:** `{"instance": "bad.instance.com", "reason": "string", "notes": "string"}`

Blocks all traffic from the given domain — inbound `Follow`/replies/`Like`/`Announce`, mention resolution, outbound follow, and the legacy protocol's inbox, regardless of whether this instance has ever interacted with it before (AGORA-177 unified what used to be two separately-checked, inconsistently-enforced mechanisms — see [Federation Service](federation.md#inbound-activities)). The input is normalized (scheme/trailing slash stripped, lowercased) so a pasted URL and a bare domain hit the same row. Also doubles as the AT Proto PDS-host block scope — see below.

### `UnbanInstance(w, r)`
`DELETE /api/moderation/instance-bans/{id}`

## Blocked Bluesky DIDs (AGORA-205)

The AT Proto counterpart to instance bans, but DID-scoped rather than domain-scoped — AT Proto identity is DID-first, so blocking one specific Bluesky account doesn't require (and can't rely on) a domain. Enforced at every inbound AT Proto ingestion path (posts, replies, likes/reposts, search results) and against outbound follow — see [AT Protocol Service → Blocking](atproto.md#blocking).

### `ListBlockedDIDs(w, r)`
`GET /api/moderation/blocked-dids`

### `BlockDID(w, r)`
`POST /api/moderation/blocked-dids`

**Body:** `{"did": "did:plc:...", "reason": "string", "notes": "string"}`

### `UnblockDID(w, r)`
`DELETE /api/moderation/blocked-dids/{id}`
