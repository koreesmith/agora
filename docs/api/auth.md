# Auth API

Base path: `/api/auth`

All requests/responses use `Content-Type: application/json`.

Authenticated endpoints require `Authorization: Bearer <token>` header.

---

## `POST /api/setup`
First-run setup. Only works when no admin exists.

**Body:** `{"username": "string", "password": "string"}`
**Response 200:** `{"token": "...", "user": {...}}`

## `GET /api/setup`
**Response 200:** `{"needs_setup": true|false}`

---

## `POST /api/auth/register`
**Body:**
```json
{
  "username": "string (alphanumeric + underscore)",
  "email": "string",
  "password": "string (min 8 chars)",
  "invite_code": "string (if registration_mode=invite)"
}
```
**Response 200:** `{"token": "...", "user": {...}}`
**Errors:** `400` invalid input, `409` username/email taken, `403` registration closed

## `POST /api/auth/login`
**Body:** `{"email": "string", "password": "string"}`
**Response 200:** `{"token": "...", "user": {...}}`
**Errors:** `401` wrong credentials, `403` suspended/banned

## `GET /api/auth/me` 🔒
**Response 200:** User object

## `POST /api/auth/change-password` 🔒
**Body:** `{"current_password": "string", "new_password": "string"}`
**Response 204**

## `POST /api/auth/forgot-password`
**Body:** `{"email": "string"}`
**Response 200:** `{"message": "If that email exists, a reset link was sent."}`

## `POST /api/auth/reset-password`
**Body:** `{"token": "string", "password": "string"}`
**Response 200:** `{"token": "...", "user": {...}}`
**Errors:** `400` invalid/expired token

## `GET /api/auth/verify-email?token=...`
**Response 200:** `{"message": "Email verified."}`

## `POST /api/auth/request-email-change` 🔒
**Body:** `{"email": "string"}`
**Response 200:** `{"message": "Verification email sent."}`

## `GET /api/auth/verify-email-change?token=...`
**Response 200:** `{"message": "Email updated."}`

## `POST /api/invites/send` 🔒
**Body:** `{"email": "string"}`
**Response 200:** `{"message": "Invite sent."}`

## `GET /api/auth/waitlist/accept?token=...`
**Response 200:** Redirect to registration page with invite code

---

🔒 = requires `Authorization: Bearer <token>`

## User Object

```json
{
  "id": "uuid",
  "username": "string",
  "email": "string",
  "display_name": "string",
  "pronouns": "string",
  "bio": "string",
  "avatar_url": "string",
  "cover_url": "string",
  "cover_position": "string",
  "location": "string",
  "website": "string",
  "role": "user|moderator|admin",
  "profile_private": false,
  "hide_timeline": false,
  "wall_approval_required": false,
  "email_verified": true,
  "created_at": "timestamp"
}
```
