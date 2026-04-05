# Users Service

**Package:** `internal/users`
**File:** `internal/users/users.go`

Manages user profiles, avatar/cover uploads, GDPR data export, and account deletion.

## Constructor

```go
func NewService(db *store.DB, media *media.Service) *Service
func (s *Service) SetFed(fed fedSender)  // wire federation broadcaster
func (s *Service) StartDeletionCleanup(ctx context.Context)  // background job
```

## Profile Object

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
  "profile_private": false,
  "hide_timeline": false,
  "wall_approval_required": false,
  "role": "user|moderator|admin",
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

## Handlers

### `GetProfile(w, r)`
`GET /api/users/{username}`

Returns a user's profile. If `profile_private=true` and the caller is not a mutual friend, limited data is returned (no bio, location, etc.).

Blocked users see a `404`. Remote users are fetchable if their data has been synced via federation.

### `UpdateProfile(w, r)`
`PATCH /api/users/me`

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

Broadcasts profile update to federation.

### `UploadAvatar(w, r)`
`POST /api/users/me/avatar`

Multipart form with field `avatar`. Resized to 400×400 JPEG. Returns `{"avatar_url": "..."}`.

### `UploadCover(w, r)`
`POST /api/users/me/cover`

Multipart form with field `cover`. Resized to 1400×500 JPEG. Returns `{"cover_url": "..."}`.

### `ExportData(w, r)`
`GET /api/users/me/export`

Returns a ZIP archive containing JSON files:
- `profile.json` — user's own profile data
- `posts.json` — all posts
- `comments.json` — all comments
- `friends.json` — friend list
- `messages.json` — direct messages

### `RequestDeletion(w, r)`
`POST /api/users/me/request-deletion`

Sets `deletion_requested_at` to now. Account is deleted after the grace period configured in admin settings (default 30 days). Sends a confirmation email.

### `CancelDeletion(w, r)`
`DELETE /api/users/me/request-deletion`

Clears `deletion_requested_at`.

### `DeleteImmediately(w, r)`
`POST /api/users/me/delete-immediately`

Deletes the account immediately. Requires `{"confirm": true}` in body.

### `Discover(w, r)`
`GET /api/users/discover`

Returns a list of public users for discovery (excludes private profiles, bots, remote users).

### `MentionSearch(w, r)`
`GET /api/users/mention-search?q=...`

Returns up to 10 users matching the query by username or display name. Used for `@mention` autocomplete.

### `EnablePostNotify(w, r)` / `DisablePostNotify(w, r)`
`POST /api/users/{username}/notify` / `DELETE /api/users/{username}/notify`

Subscribe/unsubscribe to notifications when the given user creates a post.

## Background Job: StartDeletionCleanup

Runs periodically, finds users where `deletion_requested_at + grace_days < now`, and deletes them. Cascades to posts, friendships, messages, etc.
