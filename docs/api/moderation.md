# Moderation API

All endpoints require `Authorization: Bearer <token>`.
Endpoints under `/api/moderation/*` additionally require `role=moderator` or `role=admin`.

---

## Reports

### `POST /api/reports`
Any authenticated user can submit a report.

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
**Response 201:** Report object

### `GET /api/moderation/reports` 🛡️
**Query params:** `status` (pending|reviewed|dismissed|actioned), `page`
**Response 200:** `{"reports": [...], "total": 0}`

### `POST /api/moderation/reports/{id}/review` 🛡️
**Body:** `{"status": "reviewed|dismissed|actioned", "review_notes": "string"}`
**Response 200:** Updated report object

---

## User Actions 🛡️

### `GET /api/moderation/users?filter=suspended|banned`
**Response 200:** `[...user objects with suspension/ban details...]`

### `POST /api/moderation/users/{userID}/suspend`
**Body:** `{"reason": "string"}`
**Response 204**

### `POST /api/moderation/users/{userID}/unsuspend`
**Response 204**

### `POST /api/moderation/users/{userID}/ban`
**Body:** `{"reason": "string"}`
**Response 204**

### `POST /api/moderation/users/{userID}/unban`
**Response 204**

---

## Instance Bans & Blocked DIDs 🛡️

Instance bans are enforced against both ActivityPub actors on the banned domain and, for the AT Proto side, a Bluesky handle's own domain — see [Federation Service](../backend/federation.md#inbound-activities) and [AT Protocol Service](../backend/atproto.md#blocking).

### `GET /api/moderation/instance-bans`
**Response 200:** `{ "bans": [{ "id", "instance", "reason", "notes", "created_at", "banned_by" }] }`

### `POST /api/moderation/instance-bans`
**Body:** `{"instance": "bad.instance.com", "reason": "string", "notes": "string"}`
**Response 201:** `{ "id": "...", "message": "instance banned" }`

### `DELETE /api/moderation/instance-bans/{id}`
**Response 200:** `{ "message": "instance unbanned" }`

### `GET /api/moderation/blocked-dids`
**Response 200:** `{ "blocks": [{ "id", "did", "reason", "notes", "created_at", "blocked_by" }] }`

### `POST /api/moderation/blocked-dids`
**Body:** `{"did": "did:plc:...", "reason": "string", "notes": "string"}`
**Response 201:** `{ "id": "...", "message": "DID blocked" }`

### `DELETE /api/moderation/blocked-dids/{id}`
**Response 200:** `{ "message": "DID unblocked" }`

---

🛡️ = requires `role=moderator` or `role=admin`
