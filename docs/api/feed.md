# Feed API

All endpoints require `Authorization: Bearer <token>`.

---

## `GET /api/feed`
Get paginated feed for the authenticated user.

**Query params:**
- `page` (int, default 1)
- `list_id` (uuid) — filter to a specific friend group

**Response 200:** `{"posts": [...], "total": 0}`

---

## `POST /api/posts`
Create a new post.

**Body:**
```json
{
  "content": "string",
  "image_url": "string",
  "content_warning": "string",
  "visibility": "public|friends|group|private",
  "friend_list_id": "uuid (required if visibility=group)",
  "community_group_id": "uuid",
  "wall_user_id": "uuid",
  "link_url": "string",
  "poll_options": ["Option A", "Option B"],
  "poll_expires_at": "ISO8601 timestamp",
  "poll_multiple_choice": false
}
```
**Response 201:** Post object

## `GET /api/posts/{id}`
**Response 200:** Post object
**Errors:** `403` no visibility access, `404` not found

## `DELETE /api/posts/{id}`
Author or admin only. Soft-deletes.
**Response 204**

## `PATCH /api/posts/{id}`
**Body:** `{"content": "string", "image_url": "string", "visibility": "...", "content_warning": "..."}`
**Response 200:** Updated post object

---

## `POST /api/posts/{id}/like`
**Response 200:** `{"like_count": 5}`

## `DELETE /api/posts/{id}/like`
**Response 200:** `{"like_count": 4}`

## `POST /api/posts/{id}/react`
**Body:** `{"type": "like|love|laugh|wow|angry|care|pride|thankful|vomit"}`
**Response 200:** Reaction summary

## `DELETE /api/posts/{id}/react`
**Response 200:** Reaction summary

## `GET /api/posts/{id}/reactions`
**Response 200:**
```json
{
  "like": [{ "id", "username", "display_name", "avatar_url" }],
  "love": [...],
  ...
}
```

---

## `POST /api/posts/{id}/repost`
**Body:** `{"content": "optional quote"}` (empty for a plain repost)
**Response 201:** New post object

---

## `GET /api/posts/{id}/comments`
**Response 200:** `[...post objects...]`

## `POST /api/posts/{id}/comments`
**Body:** `{"content": "string", "image_url": "string"}`
**Response 201:** Comment (post) object

## `DELETE /api/posts/{id}/comments/{commentID}`
**Response 204**

## `PATCH /api/posts/{id}/comments/{commentID}`
**Body:** `{"content": "string"}`
**Response 200:** Updated comment object

---

## `GET /api/users/{username}/posts`
**Query params:** `page`, `limit`
**Response 200:** `{"posts": [...], "total": 0}`

## `GET /api/users/{username}/wall`
**Response 200:** `{"posts": [...]}`

## `GET /api/users/me/wall-queue`
**Response 200:** `{"posts": [...]}` (pending wall posts)

## `POST /api/posts/{id}/wall-approve`
**Response 204**

## `POST /api/posts/{id}/wall-reject`
**Response 204**

---

## `POST /api/posts/{id}/poll/vote`
**Body:** `{"option_id": "uuid"}`
**Response 200:** Updated poll options with vote counts

## `DELETE /api/posts/{id}/poll/vote`
**Response 200:** Updated poll options

## `POST /api/posts/{id}/poll/options`
**Body:** `{"text": "New option"}`
**Response 200:** Updated poll options

---

## `GET /api/preview?url=...`
Fetch Open Graph metadata for a URL.

**Response 200:**
```json
{
  "url": "string",
  "title": "string",
  "description": "string",
  "image": "string",
  "domain": "string"
}
```
