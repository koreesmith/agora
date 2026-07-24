# Admin Service

**Package:** `internal/admin`
**File:** `internal/admin/admin.go`

Instance-wide settings, user management, invite codes, audit log, federation management, and rules. All endpoints require `role=admin` or `role=moderator`.

## Constructor

```go
func NewService(db *store.DB, cfg *config.Config, notif *notifications.Service) *Service
```

## Handlers

### `GetSettings(w, r)` / `UpdateSettings(w, r)`
`GET /api/admin/settings` / `PATCH /api/admin/settings`

Reads/writes `instance_settings` table. Updateable keys:

| Key | Type | Description |
|-----|------|-------------|
| `instance_name` | string | Display name |
| `instance_description` | string | About text |
| `registration_mode` | `open\|invite\|closed` | Who can register |
| `federation_enabled` | `true\|false` | Enable the legacy Agora-to-Agora protocol |
| `activitypub_enabled` | `true\|false` | Enable standard ActivityPub (fediverse) — defaults **on** |
| `atproto_enabled` | `true\|false` | Enable native AT Protocol (Bluesky) — defaults **off**, since no instance has a bot/relay configured out of the box. See [AT Protocol Service](atproto.md). |
| `deletion_grace_days` | int string | Days before deletion |
| `smtp_host` | string | SMTP server |
| `smtp_port` | string | |
| `smtp_user` | string | |
| `smtp_password` | string | |
| `smtp_from` | string | From address |
| `smtp_enabled` | `true\|false` | Enable email |
| `user_invites_enabled` | `true\|false` | Let regular users send invites, not just admins |
| `logo_url` | string | Instance logo |

Related AT Proto/fediverse-specific `instance_settings` keys (`atproto_relay_host`, `atproto_appview_host`, `atproto_bot_pds_host`) exist but are **not** in this admin-editable allowlist — they're set directly in the database by an operator, not through the admin panel. See [AT Protocol Service → Configuration](atproto.md#configuration).

### `GetStats(w, r)`
`GET /api/admin/stats`

**Response:**
```json
{
  "user_count": 0,
  "post_count": 0,
  "comment_count": 0,
  "report_count": 0,
  "pending_report_count": 0
}
```

### `ListUsers(w, r)`
`GET /api/admin/users?q=...`

Paginated list of all users with search.

### `SetRole(w, r)`
`PATCH /api/admin/users/{userID}/role`

**Body:** `{"role": "user|moderator|admin"}`

### `DeleteUser(w, r)`
`DELETE /api/admin/users/{userID}`

Immediately deletes the user and all their content.

### `ResendVerification(w, r)`
`POST /api/admin/users/{userID}/resend-verification`

### `ListInvites(w, r)` / `CreateInvite(w, r)` / `RevokeInvite(w, r)`
`GET /api/admin/invites` / `POST /api/admin/invites` / `DELETE /api/admin/invites/{id}`

Manage registration invite codes.

### `GetAuditLog(w, r)`
`GET /api/admin/audit-log`

Returns admin actions log, newest first. **Query params:** `page`, `limit`.

### Federation Management

Manages `federated_instances` — the legacy Agora-to-Agora protocol's known-instance table. This is **not** the same block mechanism as ActivityPub instance bans below; see [Federation Service → Instance blocking](federation.md#inbound-activities) for how the two now unify at enforcement time.

`GET /api/admin/federation/instances` — list known instances
`POST /api/admin/federation/instances` — add instance **Body:** `{"domain": "..."}`
`POST /api/admin/federation/instances/{id}/block` — block instance
`POST /api/admin/federation/instances/{id}/unblock` — unblock instance

### Fediverse Instance Bans & AT Proto Blocklists

Distinct from Federation Management above — these are enforced against **both** ActivityPub actors and (for the domain-scoped list) Bluesky accounts' handle domains. Endpoints require `role=moderator` or `role=admin` but live under `/api/moderation/*`, not `/api/admin/*` — see [Moderation Service](moderation.md#instance-bans) and [Moderation API](../api/moderation.md#instance-bans).

### Relays

`internal/federation.RegisterAdminRoutes` — admin-only management of fediverse relay subscriptions, registered separately from `admin.RegisterRoutes` because subscribing to a relay requires signing as the instance actor (AGORA-219), machinery that lives in `internal/federation`, not `internal/admin`. See [Federation Service → Fediverse relays](federation.md#fediverse-relays) and [Admin API → Relays](../api/admin.md#relays).

### Rules

Instance rules displayed on the registration page and `/about`.

`GET /api/admin/rules` — list rules
`POST /api/admin/rules` — **Body:** `{"text": "string"}`
`PATCH /api/admin/rules/{id}` — **Body:** `{"text": "string"}`
`DELETE /api/admin/rules/{id}`
`PATCH /api/admin/rules/{id}/move` — **Body:** `{"direction": "up|down"}`

### Waitlist

When `registration_mode=closed`, users can join a waitlist.

`GET /api/admin/waitlist` — list pending waitlist entries
`POST /api/admin/waitlist/{id}/approve` — sends invite email
`DELETE /api/admin/waitlist/{id}` — reject
