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

## Drafts

Saved drafts of a post, kept on the server so they are the same on every device (AGORA-431). `data` is the composer's own fields as one JSON object, such as `visibility`, `group_id`, `content_warning`, `image_urls`, `image_alts`, `video_url`, `video_thumb_url`, `poll_options` and the network choices. The server doesn't interpret it, except that it records which of this instance's uploaded files it points to (in `image_urls`, `image_url`, `video_url` and `video_thumb_url`) so cleaning up unused media leaves them alone.

### `GET /api/drafts` 🔒
**Response 200:** `{"drafts": [{"id", "content", "data", "created_at", "updated_at"}], "max_drafts": 25}`, most recently changed first. A draft untouched for 90 days is not listed and is removed by a daily cleanup.

### `POST /api/drafts` 🔒
**Body:** `{"content": "string", "data": {...}}`. **Response 201:** the draft.
**Errors:** 400 when there is nothing to save (no text, no media and no poll option), or `data` isn't an object; 413 when it is too big (64 KB of data, 10,000 characters of text); 409 at the limit of 25 drafts.

### `PUT /api/drafts/{id}` 🔒
Replaces a draft's contents and moves it to the top. **Response 200:** the draft. **Errors:** 400 and 413 as above; 404 for a draft that isn't yours.

### `DELETE /api/drafts/{id}` 🔒
Idempotent; someone else's draft is left alone. **Response 200:** `{"deleted": true}`.

To post a draft, send `POST /api/posts` as usual with `"draft_id": "<id>"`. The draft is removed in the same request, and only if it is yours.

---

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
**Body:** `{"type": "like|love|laugh|wow|care|thankful|pride|sad|angry|dislike"}`
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
**Query params:** `offset`, `limit`
**Response 200:** `{"posts": [...], "pinned_posts": [...]}`

`posts` is the page, newest first, with every post in its usual place. On the **first page only** (`offset` 0 or absent), `pinned_posts` also lists the author's pinned posts (AGORA-447), newest pin first, each with `"pinned": true`. It holds only the pins the viewer may see: a pin on a post they can't see leaves no trace, not the post and not a gap. Clients that know about pins show `pinned_posts` first and leave those posts out of `posts`; clients that don't show the same profile they always did. Any post in any list carries `"pinned": true` when its author has pinned it.

---

## `POST /api/posts/{id}/pin` 🔒
Pin one of your own posts to the top of your profile. Idempotent. Quiet: nobody is notified and nothing is federated. At most **3** posts can be pinned at once; deleted posts don't count, so deleting a pinned post frees its slot.

**Response 200:** `{"pinned": true}`
**Errors:** 403 for someone else's post; 400 for a post that can't be pinned (a comment, a repost, a post written on someone's wall, or one made in a group or as a page); 404 for a missing or deleted post; 409 at the limit, with the message `You can pin up to 3 posts. Unpin one first.`

A pin never changes who can see the post.

## `DELETE /api/posts/{id}/pin` 🔒
Remove a pin. Idempotent. **Response 200:** `{"pinned": false}`

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

## `GET /api/reels`
One page of the Reels feed: posts that have a video, newest first, with nothing ranked. Videos the viewer has already watched are left out unless asked for.

**Query params:**
- `tab` — `following` (default) or `discover`
  - `following`: videos from you, your friends, pages you subscribe to, and Fediverse and Bluesky accounts you follow with "show in feed" on. The same audience rules as `GET /api/feed`.
  - `discover`: public videos from this instance and its peered Agora instances, the same pool as the public Explore feed. Never your own videos, private profiles, or accounts scheduled for deletion. Returns 404 when an admin has turned Discover off (`reels_discover_enabled`, also on `GET /api/instance`).
- `limit` (int, default 10, max 50)
- `cursor` (string) — the `next_cursor` of the previous page. Opaque.
- `include_viewed` (`true`) — include videos already marked viewed, for "watch older videos"

Both tabs also respect blocks, hidden posts and keyword mutes.

**Response 200:**
```json
{ "posts": [ ... ], "next_cursor": "string", "exhausted": false }
```
Posts have the same shape as in `GET /api/feed`, including `video_url`, `video_thumb_url`, `video_width`, `video_height` and `video_duration_ms`. `next_cursor` is empty on the last page, and `exhausted` is then `true`: nothing further to watch, which is the cue for "You're all caught up". Pages use a cursor rather than `page` or `offset`, so marking a video viewed between requests never makes the next page skip or repeat one.

**Errors:** 400 for an unknown `tab` or a malformed `cursor`; 404 for `discover` while it is turned off.

---

## `POST /api/reels/{id}/viewed`
Record that the viewer watched a reel, so it stops coming back. Call it after a few seconds of playback or on completion. Idempotent.

**Response 200:** `{"viewed": true}`
**Errors:** 404 when the post doesn't exist, has no video, or isn't visible to the viewer.

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
