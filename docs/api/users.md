# Users API

All endpoints require `Authorization: Bearer <token>` unless noted.

---

## `GET /api/users/{username}`
Get a user's profile. Public profiles are accessible without auth.

**Response 200:** Profile object (see below)
**Errors:** `404` user not found or blocked

## `PATCH /api/users/me`
Update own profile.

**Body (all optional):**
```json
{
  "display_name": "string",
  "pronouns": "string",
  "bio": "string",
  "location": "string",
  "website": "string",
  "profile_private": false,
  "hide_timeline": false,
  "wall_approval_required": false,
  "cover_position": "50% 50%"
}
```
**Response 200:** Updated profile object

## `POST /api/users/me/avatar`
Multipart form. Field: `avatar`. Resized to 400×400 JPEG.
**Response 200:** `{"avatar_url": "/uploads/avatars/..."}`

## `POST /api/users/me/cover`
Multipart form. Field: `cover`. Resized to 1400×500 JPEG.
**Response 200:** `{"cover_url": "/uploads/covers/..."}`

## `GET /api/users/me/export`
Download GDPR data export as a ZIP file.
**Response 200:** `Content-Type: application/zip`

## `POST /api/users/me/request-deletion`
Request account deletion. Account deleted after grace period.
**Response 200:** `{"deletion_at": "timestamp"}`

## `DELETE /api/users/me/request-deletion`
Cancel a pending deletion request.
**Response 204**

## `POST /api/users/me/delete-immediately`
**Body:** `{"confirm": true}`
**Response 204**

## `GET /api/users/discover`
Returns public profiles for discovery.
**Response 200:** `[...profile objects...]`

## `GET /api/users/mention-search?q=...`
Autocomplete search for @mentions. Returns up to 10 results.
**Response 200:** `[{ "id", "username", "display_name", "avatar_url" }]`

## `POST /api/users/{username}/notify`
Subscribe to post notifications for a user.
**Response 204**

## `DELETE /api/users/{username}/notify`
Unsubscribe from post notifications.
**Response 204**

---

## Profile Object

```json
{
  "id": "uuid",
  "username": "string",
  "email": "string (own profile only)",
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
  "is_remote": false,
  "remote_instance": "string",
  "friend_status": "self|accepted|pending|pending_incoming|declined|blocked|none",
  "friend_count": 0,
  "post_notifications_enabled": false,
  "is_blocked": false,
  "email_verified": true,
  "deletion_requested_at": "timestamp",
  "created_at": "timestamp"
}
```
