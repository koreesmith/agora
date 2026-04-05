# Database Schema

PostgreSQL 16 with extensions `uuid-ossp` (UUID generation) and `pg_trgm` (trigram full-text search).

Migrations run automatically at startup via `store.Migrate()` in `internal/store/store.go`.

## Tables

### `users`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | Generated with `uuid_generate_v4()` |
| `username` | text UNIQUE NOT NULL | Lowercase alphanumeric + underscore |
| `email` | text UNIQUE NOT NULL | |
| `password_hash` | text | bcrypt hash; empty for remote users |
| `display_name` | text | Public display name |
| `pronouns` | text | Optional pronouns |
| `bio` | text | Profile bio |
| `avatar_url` | text | Relative URL to upload |
| `cover_url` | text | Relative URL to cover image |
| `cover_position` | text | CSS `background-position` value |
| `location` | text | |
| `website` | text | |
| `role` | text | `user`, `moderator`, or `admin` |
| `profile_private` | boolean | Friends-only profile |
| `hide_timeline` | boolean | Hide post timeline on profile |
| `wall_approval_required` | boolean | Require approval for wall posts |
| `is_suspended` | boolean | Temporarily suspended |
| `suspension_reason` | text | Reason shown to user |
| `is_banned` | boolean | Permanently banned |
| `ban_reason` | text | |
| `is_remote` | boolean | User from federated instance |
| `remote_instance` | text | Domain of remote instance |
| `remote_user_id` | text | ID on remote instance |
| `public_key` | text | Ed25519 public key (base64) |
| `private_key` | text | Ed25519 private key (admin user only) |
| `email_verified` | boolean | |
| `email_verify_token` | text | |
| `email_change_token` | text | |
| `pending_email` | text | Email awaiting verification |
| `password_reset_token` | text | |
| `password_reset_expires` | timestamptz | |
| `email_notifications_enabled` | boolean | |
| `unsubscribe_token` | text | For one-click email unsubscribe |
| `deletion_requested_at` | timestamptz | When account deletion was requested |
| `created_at` | timestamptz | |

**Indexes:** trigram indexes on `username` and `display_name` for fast ILIKE search.

---

### `friendships`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `requester_id` | uuid FK→users | User who sent the request |
| `addressee_id` | uuid FK→users | User who received the request |
| `status` | text | `pending`, `accepted`, `declined`, `blocked` |
| `created_at` | timestamptz | |

**Unique:** `(requester_id, addressee_id)`

---

### `friend_groups`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `owner_id` | uuid FK→users | |
| `name` | text | Display name of the list |
| `created_at` | timestamptz | |

### `friend_group_members`

| Column | Type | Description |
|--------|------|-------------|
| `group_id` | uuid FK→friend_groups | |
| `friend_id` | uuid FK→users | |

---

### `posts`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `author_id` | uuid FK→users | |
| `parent_id` | uuid FK→posts | Non-null for comments |
| `repost_of_id` | uuid FK→posts | Non-null for reposts |
| `content` | text | Markdown content |
| `image_url` | text | Attached image |
| `content_warning` | text | CW/spoiler label |
| `visibility` | text | `public`, `friends`, `group`, `private` |
| `friend_list_id` | uuid FK→friend_groups | For `group` visibility |
| `community_group_id` | uuid FK→community_groups | If posted in a group |
| `wall_user_id` | uuid FK→users | If posted on a user's wall |
| `wall_status` | text | `pending`, `approved`, `rejected` |
| `link_url` | text | Attached link URL |
| `link_title` | text | |
| `link_description` | text | |
| `link_image` | text | |
| `link_domain` | text | |
| `is_remote` | boolean | From federated instance |
| `remote_post_id` | text | ID on remote instance |
| `remote_instance` | text | Domain of remote instance |
| `edited_at` | timestamptz | |
| `deleted_at` | timestamptz | Soft delete |
| `created_at` | timestamptz | |

---

### `likes`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `post_id` | uuid FK→posts | |
| `user_id` | uuid FK→users | |
| `created_at` | timestamptz | |

**Unique:** `(post_id, user_id)`

---

### `reactions`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `post_id` | uuid FK→posts | |
| `user_id` | uuid FK→users | |
| `type` | text | `like`, `love`, `laugh`, `wow`, `angry`, `care`, `pride`, `thankful`, `vomit` |
| `created_at` | timestamptz | |

**Unique:** `(post_id, user_id)`

---

### `notifications`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `user_id` | uuid FK→users | Recipient |
| `actor_id` | uuid FK→users | Who triggered it |
| `type` | text | `friend_request`, `friend_accepted`, `post_like`, `post_comment`, `post_mention`, `comment_like`, `comment_reply`, `new_report`, `post_update` |
| `post_id` | uuid FK→posts | Related post (if any) |
| `data` | jsonb | Extra data (e.g. comment text preview) |
| `read` | boolean | |
| `created_at` | timestamptz | |

---

### `reports`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `reporter_id` | uuid FK→users | |
| `reported_user_id` | uuid FK→users | |
| `reported_post_id` | uuid FK→posts | |
| `reported_comment_id` | uuid FK→posts | |
| `violation_type` | text | Type of violation |
| `details` | text | Reporter's description |
| `rule_id` | uuid FK→instance_rules | |
| `status` | text | `pending`, `reviewed`, `dismissed`, `actioned` |
| `review_notes` | text | Moderator notes |
| `reviewed_by` | uuid FK→users | |
| `reviewed_at` | timestamptz | |
| `created_at` | timestamptz | |

---

### `invite_codes`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `code` | text UNIQUE | The invite code string |
| `created_by` | uuid FK→users | |
| `used_by` | uuid FK→users | Null if unused |
| `expires_at` | timestamptz | |
| `created_at` | timestamptz | |

---

### `instance_settings`

| Column | Type | Description |
|--------|------|-------------|
| `key` | text PK | Setting name |
| `value` | text | Setting value |

Key values include: `instance_name`, `instance_description`, `registration_mode`, `federation_enabled`, `deletion_grace_days`, `smtp_*`, `logo_url`.

---

### `community_groups`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `name` | text | |
| `slug` | text UNIQUE | URL-safe lowercase hyphenated |
| `description` | text | |
| `cover_url` | text | |
| `cover_position` | text | |
| `avatar_url` | text | |
| `privacy` | text | `public` or `private` |
| `created_by` | uuid FK→users | |
| `created_at` | timestamptz | |

### `community_group_members`

| Column | Type | Description |
|--------|------|-------------|
| `group_id` | uuid FK→community_groups | |
| `user_id` | uuid FK→users | |
| `role` | text | `owner`, `mod`, `member` |
| `joined_at` | timestamptz | |

### `community_group_invites`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `group_id` | uuid FK→community_groups | |
| `token` | text UNIQUE | Invite link token |
| `created_by` | uuid FK→users | |
| `max_uses` | int | 0 = unlimited |
| `use_count` | int | |
| `expires_at` | timestamptz | |
| `created_at` | timestamptz | |

### `community_group_join_requests`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `group_id` | uuid FK→community_groups | |
| `user_id` | uuid FK→users | |
| `message` | text | Optional request message |
| `status` | text | `pending`, `approved`, `rejected` |
| `created_at` | timestamptz | |

---

### `albums`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `owner_id` | uuid FK→users | |
| `title` | text | |
| `description` | text | |
| `cover_url` | text | |
| `visibility` | text | `public`, `friends`, `private` |
| `created_at` | timestamptz | |

### `album_photos`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `album_id` | uuid FK→albums | |
| `url` | text | |
| `caption` | text | |
| `position` | int | Sort order |
| `created_at` | timestamptz | |

---

### `conversations`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `created_at` | timestamptz | |
| `updated_at` | timestamptz | Updated on new message |

### `conversation_participants`

| Column | Type | Description |
|--------|------|-------------|
| `conversation_id` | uuid FK→conversations | |
| `user_id` | uuid FK→users | |
| `last_read_at` | timestamptz | For unread count |
| `is_accepted` | boolean | False until user accepts DM request |
| `read_receipts` | boolean | Whether to share read status |
| `left_at` | timestamptz | When user left |

### `messages`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `conversation_id` | uuid FK→conversations | |
| `sender_id` | uuid FK→users | |
| `content` | text | |
| `image_url` | text | |
| `edited_at` | timestamptz | |
| `deleted_at` | timestamptz | Soft delete |
| `created_at` | timestamptz | |

### `message_reactions`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `message_id` | uuid FK→messages | |
| `user_id` | uuid FK→users | |
| `type` | text | Reaction emoji/type |
| `created_at` | timestamptz | |

---

### `blocks`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `blocker_id` | uuid FK→users | |
| `blocked_id` | uuid FK→users | |
| `created_at` | timestamptz | |

**Unique:** `(blocker_id, blocked_id)`

---

### `poll_options`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `post_id` | uuid FK→posts | |
| `text` | text | Option label |
| `position` | int | Sort order |
| `created_at` | timestamptz | |

### `poll_votes`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `post_id` | uuid FK→posts | |
| `option_id` | uuid FK→poll_options | |
| `user_id` | uuid FK→users | |
| `created_at` | timestamptz | |

---

### `federation_queue`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `target_instance` | text | Domain to deliver to |
| `activity` | jsonb | Activity payload |
| `attempts` | int | Delivery attempt count |
| `last_attempt_at` | timestamptz | |
| `created_at` | timestamptz | |

Retried up to 10 times with backoff. Abandoned after 10 failures.

### `federated_instances`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `domain` | text UNIQUE | |
| `public_key` | text | Ed25519 public key (base64) |
| `name` | text | Instance display name |
| `is_blocked` | boolean | |
| `last_seen_at` | timestamptz | |
| `created_at` | timestamptz | |

---

### `audit_log`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `actor_id` | uuid FK→users | Admin who performed action |
| `action` | text | Action type |
| `target_user_id` | uuid FK→users | Affected user (if any) |
| `details` | jsonb | Extra detail |
| `created_at` | timestamptz | |

---

### `instance_rules`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `text` | text | Rule content |
| `position` | int | Display order |
| `created_at` | timestamptz | |

### `instance_bans`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `domain` | text UNIQUE | Banned domain |
| `reason` | text | |
| `created_by` | uuid FK→users | |
| `created_at` | timestamptz | |

---

### `post_notifications`

| Column | Type | Description |
|--------|------|-------------|
| `user_id` | uuid FK→users | Subscriber |
| `author_id` | uuid FK→users | Author being followed |
| `created_at` | timestamptz | |

**Unique:** `(user_id, author_id)`

---

### `waitlist`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `email` | text UNIQUE | |
| `name` | text | |
| `status` | text | `pending`, `approved`, `rejected` |
| `created_at` | timestamptz | |
