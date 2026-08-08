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
| `is_remote` | boolean | User from federated instance (fediverse or Bluesky) |
| `remote_instance` | text | Domain of remote instance — literal `bsky.app` for a Bluesky account stub |
| `remote_user_id` | text | ID on remote instance |
| `public_key` | text | Unused since AGORA-330. Held an Ed25519 key for the removed Agora-to-Agora transport. |
| `private_key` | text | Unused since AGORA-330, as above. |
| `ap_actor_url` | text | This user's own ActivityPub actor URL, or (for a remote stub) the followed/interacted-with actor's URL |
| `ap_inbox_url` | text | Corresponding inbox URL |
| `federation_public_key` / `federation_private_key` | text | Per-actor RSA keypair (PEM), used for standard ActivityPub HTTP Signatures. Load-bearing, and not to be confused with the disused `public_key`/`private_key` above. Lazily generated on first use. |
| `activitypub_enabled` | boolean | Per-account opt-out for standard ActivityPub (default `true`) |
| `fediverse_notifications_enabled` | boolean | Global kill switch for notifications about *followed* fediverse accounts' new posts (default `true`) — independent of `activitypub_enabled`, which governs whether your own posts federate |
| `emojis` | jsonb | Resolved Mastodon custom-emoji shortcode → image URL map for this user's `display_name`/`bio` (default `{}`) |
| `atproto_did` | text | This user's `did:web:username.instance.tld` |
| `atproto_private_key` | text | secp256k1 signing key, hex-encoded (not PEM) |
| `atproto_repo_head` | text | Current signed AT Proto repo commit CID |
| `atproto_repo_rev` | text | Last-emitted repo revision string |
| `atproto_enabled` | boolean | Per-account opt-out for AT Proto/Bluesky (default `true`) |
| `atproto_remote_did` | text | For a cached remote Bluesky stub, the followed/interacted-with account's DID (stable across handle changes — unique index `idx_users_atproto_remote_did`) |
| `atproto_notifications_enabled` | boolean | Global kill switch for notifications about followed Bluesky accounts (default `true`) |
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
| `remote_post_id` | text | ID on remote instance — an AT-URI (`at://did/.../rkey`) for a Bluesky-origin post |
| `remote_post_cid` | text | Record CID, paired with `remote_post_id` to make a full AT Proto strong-ref (Bluesky-origin posts only) |
| `remote_instance` | text | Domain of remote instance — literal `bsky.app` for Bluesky |
| `emojis` | jsonb | Resolved Mastodon custom-emoji shortcode → image URL map for this post's content (default `{}`) |
| `edited_at` | timestamptz | |
| `deleted_at` | timestamptz | Soft delete |
| `created_at` | timestamptz | |

**`post_hashtags`** — `post_id UUID FK→posts`, `tag TEXT` (lowercased, no `#`), `PRIMARY KEY (post_id, tag)`. Populated from AT Proto richtext facets and extracted fediverse/local content (AGORA-213); powers exact-match `#tag` search (`SearchPosts`).

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
| `type` | text | `like`, `love`, `laugh`, `wow`, `care`, `thankful`, `pride`, `sad`, `angry`, `dislike` (CHECK-constrained) |
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
| `remote_message_id` | text | AGORA-323: the sender's own object id for a message from another instance. Unique where non-empty, which is what makes redelivery idempotent. Empty for a local message. |
| `edited_at` | timestamptz | |
| `deleted_at` | timestamptz | Soft delete |
| `created_at` | timestamptz | |

### `message_reactions`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `message_id` | uuid FK→messages | |
| `user_id` | uuid FK→users | |
| `type` | text | Same CHECK-constrained set as `reactions.type`. Stored the raw emoji glyph before v4.0.0; existing rows were remapped to type names. |
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

### `post_audience`

Who a limited-audience post from another Agora instance was addressed to (AGORA-342). The post itself is stored with `visibility = 'private'`, so it is hidden by every feed filter by default and becomes visible only through a row here. A friends-only post needs no rows: both ends can derive that audience from the friendship. A friend-list post cannot, since the author's list does not exist on this instance, and ADR-002 deliberately keeps its membership off the wire.

| Column | Type | Description |
|--------|------|-------------|
| `post_id` | uuid FK→posts | Part of PK, `ON DELETE CASCADE` |
| `user_id` | uuid FK→users | Part of PK, `ON DELETE CASCADE` |

---

### `federated_instances`

| Column | Type | Description |
|--------|------|-------------|
| `id` | uuid PK | |
| `domain` | text UNIQUE | |
| `public_key` | text | Blanked and no longer written by AGORA-330. Held the peer's key for the removed transport; the column is retained so an upgrade need not rewrite the table. |
| `name` | text | Instance display name |
| `direction` | varchar(10) | `outbound` (an admin added them), `inbound` (they contacted us first), `mutual`, or `unknown` for rows predating AGORA-321 |
| `is_blocked` | boolean | Blocks this peer. See `instance_bans` below for the unified fediverse block, which also covers ActivityPub |
| `last_seen_at` | timestamptz | |
| `created_at` | timestamptz | |

---

### `custom_feed_filters`

Not fediverse/Bluesky-specific infrastructure, but this is how a followed fediverse or Bluesky account surfaces in a custom feed rather than getting a dedicated timeline of its own — `id` uuid PK, `feed_id` FK→custom_feeds, `filter_type`, `value`, `created_at`. `filter_type` includes (among others) `fediverse_account`/`fediverse_all`/`exclude_fediverse_account` and `atproto_account`/`atproto_all`/`exclude_atproto_account`.

---

## ActivityPub (fediverse) tables

See [Federation Service](backend/federation.md) for how these fit together.

### `ap_followers`

Remote actors following a **local** user. `id` uuid PK, `followed_user_id` FK→users, `follower_actor_url`, `follower_inbox_url`, `created_at`. **Unique:** `(followed_user_id, follower_actor_url)`.

### `ap_following`

Local users following a **remote** fediverse actor — the reverse of `ap_followers`. `id` uuid PK, `follower_user_id` FK→users, `followed_actor_url`, `followed_inbox_url`, `accepted` boolean (false until the remote `Accept` arrives), `notify` boolean default `false` (per-follow notification opt-in, AGORA-166), `created_at`. **Unique:** `(follower_user_id, followed_actor_url)`.

### `ap_delivery_queue` / `page_ap_delivery_queue` / `instance_ap_delivery_queue`

Outbound ActivityPub delivery queues. HTTP Signatures must be computed at *send* time (a fresh `Date` header per attempt), not once at enqueue, which is why the activity is stored unsigned and the drain signs it. (Agora once had a second queue, `federation_queue`, for the custom Agora-to-Agora transport; AGORA-330 dropped both it and the transport.) Same shape for all three, differing only in owner column: `actor_user_id` FK→users / `actor_page_id` FK→pages / none (there's only ever one instance actor). Columns: `id`, owner FK, `inbox_url`, `activity` jsonb, `attempts` int, `last_error`, `next_attempt`, `created_at`.

### `ap_blocked_by`

Records a remote actor's `Block` of a local user, keyed by inbox URL (not just actor URL) so `enqueueAPDelivery` can filter every outbound path from one central place. `id`, `local_user_id` FK→users, `blocker_actor_url`, `blocker_inbox_url`, `created_at`. **Unique:** `(local_user_id, blocker_actor_url)`.

### `page_remote_subscribers`

A Page's own followers, mirroring `ap_followers` but scoped to a Page instead of a user. `id`, `page_id` FK→pages, `follower_actor_url`, `follower_inbox_url`, `created_at`. **Unique:** `(page_id, follower_actor_url)`.

### `quote_authorizations`

FEP-044f quote-post grants (AGORA-255) — served at `GET /federation/users/{handle}/posts/{postID}/quote-authorizations/{authID}` for a remote server to verify a quote against. `id`, `post_id` FK→posts, `quoting_actor_url`, `quoting_object_url`, `created_at`. **Unique:** `(post_id, quoting_object_url)`.

### `instance_bans`

The unified fediverse instance-block list (AGORA-177) — also reused as the AT Proto PDS-host block scope. `id` uuid PK, `instance` text UNIQUE, `reason`, `notes`, `banned_by` FK→users, `created_at`.

### `relays`

Fediverse relay subscriptions (AGORA-220). `id` uuid PK, `inbox_url` text UNIQUE, `actor_url` (resolved lazily on first successful profile fetch), `status` (`pending`\|`enabled`\|`rejected`\|`disabled`), `added_by` FK→users, `created_at`.

---

## AT Protocol (Bluesky) tables

Agora's own PDS state — see [AT Protocol Service](backend/atproto.md) for how these fit together.

### `atproto_blocks`

The content-addressed blockstore backing every user's repo Merkle Search Tree (MST) and blobs (avatars, post images). `user_id` FK→users, `cid` text, `data` bytea. **Primary key:** `(user_id, cid)`.

### `atproto_posts`

Maps an Agora post/comment to its AT Proto record. `post_id` uuid PK, FK→posts, `user_id` FK→users, `rkey`, `record_cid`, `created_at`.

### `atproto_reactions`

Outbound like/repost record mapping — the AT Proto equivalent of the legacy `likes` table and repost-via-`posts.repost_of_id`. `post_id` FK→posts, `user_id` FK→users, `kind` (`like`\|`repost`), `rkey`, `record_cid`, `created_at`. **Primary key:** `(post_id, user_id, kind)`.

### `atproto_firehose_events`

The durable, replayable log backing `com.atproto.sync.subscribeRepos` — every commit event this instance has ever emitted, so a reconnecting relay can resume from its own `cursor` instead of Agora replaying everything or dropping missed events. `seq` bigint PK (from `atproto_firehose_seq`), `data` bytea (a serialized `XRPCStreamEvent`), `created_at`.

### `at_following`

Local users' native (non-bridged) Bluesky follows — analogous to `ap_following`, but AT Proto follows are unilateral with no accept/reject handshake. `id` uuid PK, `local_user_id` FK→users, `remote_did`, `remote_handle`, `display_name`, `avatar_url`, `rkey`, `record_cid`, `notify` boolean default `false`, `show_in_feed` boolean default `false` (AGORA-236), `created_at`. **Unique:** `(local_user_id, remote_did)`.

### `blocked_dids`

DID-scoped AT Proto block list (AGORA-205) — the counterpart to `instance_bans`' domain scope, since AT Proto identity is DID-first. `id` uuid PK, `did` text UNIQUE, `reason`, `notes`, `blocked_by` FK→users, `created_at`.

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
