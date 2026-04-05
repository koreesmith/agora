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

## Instance Bans 🛡️

### `GET /api/moderation/instance-bans`
**Response 200:** `[{ "id", "domain", "reason", "created_at" }]`

### `POST /api/moderation/instance-bans`
**Body:** `{"domain": "string", "reason": "string"}`
**Response 201**

### `DELETE /api/moderation/instance-bans/{id}`
**Response 204**

---

🛡️ = requires `role=moderator` or `role=admin`
