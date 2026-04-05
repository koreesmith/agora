# Feed Service

**Package:** `internal/feed`
**File:** `internal/feed/feed.go`

Handles posts, comments, likes, reactions, reposts, polls, wall posts, and link previews.

## Constructor

```go
func NewService(db *store.DB, notif *notifications.Service, media *media.Service) *Service
func (s *Service) SetFed(fed fedSender)      // wire federation broadcaster
func (s *Service) SetAlbums(a *albums.Service) // wire albums service
```

## Post Object

```json
{
  "id": "uuid",
  "author_id": "uuid",
  "author_username": "string",
  "author_display_name": "string",
  "author_avatar_url": "string",
  "content": "string (markdown)",
  "image_url": "string",
  "content_warning": "string",
  "visibility": "public|friends|group|private",
  "friend_list_id": "uuid",
  "community_group_id": "uuid",
  "community_group_name": "string",
  "community_group_slug": "string",
  "repost_of_id": "uuid",
  "reposted_by": { "user object" },
  "wall_user_id": "uuid",
  "wall_status": "pending|approved|rejected",
  "link_url": "string",
  "link_title": "string",
  "link_description": "string",
  "link_image": "string",
  "link_domain": "string",
  "like_count": 0,
  "comment_count": 0,
  "repost_count": 0,
  "liked": false,
  "reaction": "string (current user's reaction type, if any)",
  "reposted": false,
  "poll_options": [{ "id": "uuid", "text": "string", "votes": 0, "voted": false }],
  "poll_expires_at": "timestamp",
  "poll_multiple_choice": false,
  "is_remote": false,
  "remote_post_id": "string",
  "remote_instance": "string",
  "edited_at": "timestamp",
  "created_at": "timestamp"
}
```

## Handlers

### `GetFeed(w, r)`
`GET /api/feed`

Returns paginated posts visible to the authenticated user. Respects visibility rules, friend relationships, and blocks.

**Query params:**
- `page` (int, default 1)
- `list_id` (uuid) — filter to a specific friend group

### `CreatePost(w, r)`
`POST /api/posts`

**Body:**
```json
{
  "content": "string",
  "image_url": "string",
  "content_warning": "string",
  "visibility": "public|friends|group|private",
  "friend_list_id": "uuid",
  "community_group_id": "uuid",
  "wall_user_id": "uuid",
  "link_url": "string",
  "poll_options": ["Option A", "Option B"],
  "poll_expires_at": "timestamp",
  "poll_multiple_choice": false
}
```

Extracts `@mentions` from content and sends `post_mention` notifications. Broadcasts to federation if public.

### `GetPost(w, r)`
`GET /api/posts/{id}`

Returns single post. Returns `403` if caller lacks visibility access.

### `DeletePost(w, r)`
`DELETE /api/posts/{id}`

Soft-deletes (sets `deleted_at`). Author or admin only.

### `EditPost(w, r)`
`PATCH /api/posts/{id}`

**Body:** `{"content": "...", "image_url": "...", "visibility": "...", "content_warning": "..."}`

Sets `edited_at`. Broadcasts update to federation.

### `LikePost(w, r)` / `UnlikePost(w, r)`
`POST /api/posts/{id}/like` / `DELETE /api/posts/{id}/like`

Creates/deletes a like. Sends `post_like` notification to author on like.

### `ReactPost(w, r)` / `UnreactPost(w, r)`
`POST /api/posts/{id}/react` / `DELETE /api/posts/{id}/react`

One reaction per user per post. Replaces any existing reaction.

**Body (react):** `{"type": "like|love|laugh|wow|angry|care|pride|thankful|vomit"}`

### `GetReactions(w, r)`
`GET /api/posts/{id}/reactions`

Returns reactions grouped by type with user lists.

### `Repost(w, r)`
`POST /api/posts/{id}/repost`

Creates a new post with `repost_of_id` set. Body: `{"content": "optional quote text"}`.

### `GetComments(w, r)` / `CreateComment(w, r)`
`GET /api/posts/{id}/comments` / `POST /api/posts/{id}/comments`

Comments are posts with `parent_id` set. Creating sends `post_comment` notification.

**Body (create):** `{"content": "...", "image_url": "..."}`

### `DeleteComment(w, r)` / `EditComment(w, r)`
`DELETE /api/posts/{id}/comments/{commentID}` / `PATCH /api/posts/{id}/comments/{commentID}`

### `GetUserPosts(w, r)`
`GET /api/users/{username}/posts`

Returns a user's posts. Applies visibility and friendship checks.

### `GetWall(w, r)`
`GET /api/users/{username}/wall`

Returns approved wall posts for the given user.

### `GetWallQueue(w, r)`
`GET /api/users/me/wall-queue`

Returns pending wall posts awaiting the authenticated user's approval.

### `WallApprove(w, r)` / `WallReject(w, r)`
`POST /api/posts/{id}/wall-approve` / `POST /api/posts/{id}/wall-reject`

### `PollVote(w, r)` / `PollUnvote(w, r)`
`POST /api/posts/{id}/poll/vote` / `DELETE /api/posts/{id}/poll/vote`

**Body (vote):** `{"option_id": "uuid"}`

### `PollAddOption(w, r)`
`POST /api/posts/{id}/poll/options`

**Body:** `{"text": "New option"}`

### `GetLinkPreview(w, r)`
`GET /api/preview?url=...`

Fetches and returns Open Graph metadata for a URL.

## Visibility Rules

| Post visibility | Who can see |
|----------------|------------|
| `public` | Everyone (including unauthenticated, federated) |
| `friends` | Author's accepted friends |
| `group` | Members of the specified friend group |
| `private` | Author only |

Wall posts additionally check `wall_status = 'approved'` for public display.
