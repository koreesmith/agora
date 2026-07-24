# Admin API

All endpoints require `Authorization: Bearer <token>` with `role=admin` or `role=moderator`.

Base path: `/api/admin`

---

## Settings

### `GET /api/admin/settings`
**Response 200:** Key-value settings object

### `PATCH /api/admin/settings`
**Body:** Any subset of setting keys (see [Admin Service](../backend/admin.md) for full list)
**Response 200:** Updated settings

### `GET /api/admin/stats`
**Response 200:**
```json
{
  "user_count": 0,
  "post_count": 0,
  "comment_count": 0,
  "report_count": 0,
  "pending_report_count": 0
}
```

---

## User Management

### `GET /api/admin/users?q=...`
**Query params:** `q` (search), `page`, `limit`
**Response 200:** `{"users": [...], "total": 0}`

### `PATCH /api/admin/users/{userID}/role`
**Body:** `{"role": "user|moderator|admin"}`
**Response 204**

### `DELETE /api/admin/users/{userID}`
Immediately deletes user and all content.
**Response 204**

### `POST /api/admin/users/{userID}/resend-verification`
**Response 200:** `{"message": "sent"}`

---

## Invites

### `GET /api/admin/invites`
**Response 200:** `[{ "id", "code", "created_by", "used_by", "expires_at", "created_at" }]`

### `POST /api/admin/invites`
Creates a single-use invite code.
**Response 201:** `{ "id", "code", "expires_at" }`

### `DELETE /api/admin/invites/{id}`
**Response 204**

---

## Audit Log

### `GET /api/admin/audit-log`
**Query params:** `page`, `limit`
**Response 200:** `[{ "id", "actor_username", "action", "target_username", "details", "created_at" }]`

---

## Federation

This section manages the legacy Agora-to-Agora `federated_instances` table only. For ActivityPub instance bans and AT Proto DID/domain blocklists, see [Moderation API](moderation.md#instance-bans--blocked-dids). For AT Proto's own instance-wide toggle and host overrides, see [AT Protocol Service](../backend/atproto.md#configuration).

### `GET /api/admin/federation/instances`
**Response 200:** `[{ "id", "domain", "name", "public_key", "is_blocked", "last_seen_at" }]`

### `POST /api/admin/federation/instances`
**Body:** `{"domain": "string"}`
**Response 201**

### `POST /api/admin/federation/instances/{id}/block`
**Response 204**

### `POST /api/admin/federation/instances/{id}/unblock`
**Response 204**

---

## Relays

Fediverse relay subscriptions (AGORA-220/223) — registered by `internal/federation.RegisterAdminRoutes`, not `internal/admin`, since subscribing requires signing as the instance actor. See [Federation Service → Fediverse relays](../backend/federation.md#fediverse-relays).

### `GET /api/admin/relays`
**Response 200:** `{ "relays": [{ "id", "inbox_url", "actor_url", "status", "created_at" }] }`

`status` is one of `pending` (Follow sent, no Accept/Reject yet — not an error, some relays never reply), `enabled`, `disabled`, `rejected`.

### `POST /api/admin/relays`
**Body:** `{"inbox_url": "https://relay.example.com/inbox"}` — must be `https://`.

Sends a `Follow` from the instance actor.

**Response 201:** `{ "id": "...", "message": "relay follow requested" }`

### `POST /api/admin/relays/{id}/enable`
Re-subscribes a disabled relay (fresh `Follow`). No-op, not an error, if already enabled.
**Response 200**

### `POST /api/admin/relays/{id}/disable`
Sends `Undo(Follow)`, keeps the row (unlike delete).
**Response 200**

### `DELETE /api/admin/relays/{id}`
Sends `Undo(Follow)` first if the subscription might still be live, then removes the relay entirely.
**Response 200**

---

## Rules

### `GET /api/admin/rules`
**Response 200:** `[{ "id", "text", "position" }]`

### `POST /api/admin/rules`
**Body:** `{"text": "string"}`
**Response 201:** Rule object

### `PATCH /api/admin/rules/{id}`
**Body:** `{"text": "string"}`
**Response 200**

### `DELETE /api/admin/rules/{id}`
**Response 204**

### `PATCH /api/admin/rules/{id}/move`
**Body:** `{"direction": "up|down"}`
**Response 204**

---

## Waitlist

### `GET /api/admin/waitlist`
**Response 200:** `[{ "id", "email", "name", "status", "created_at" }]`

### `POST /api/admin/waitlist/{id}/approve`
Sends invite email to waitlisted user.
**Response 204**

### `DELETE /api/admin/waitlist/{id}`
Reject waitlist entry.
**Response 204**
