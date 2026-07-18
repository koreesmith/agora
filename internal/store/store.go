package store

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

type DB struct {
	*sql.DB
}

func Open(dsn string) (*DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	return &DB{db}, nil
}

func (db *DB) Migrate() error {
	log.Println("store: running migrations")
	for i, m := range schema {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}
	log.Println("store: migrations complete")
	return nil
}

// NeedsSetup returns true if no admin user exists yet.
func (db *DB) NeedsSetup() (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

var schema = []string{
	`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
	`CREATE EXTENSION IF NOT EXISTS "pg_trgm"`,

	// ── Users ──────────────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS users (
		id                        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		username                  VARCHAR(50)  UNIQUE NOT NULL,
		email                     VARCHAR(255) UNIQUE NOT NULL,
		password_hash             TEXT         NOT NULL,
		display_name              VARCHAR(100) NOT NULL DEFAULT '',
		bio                       TEXT         NOT NULL DEFAULT '',
		avatar_url                TEXT         NOT NULL DEFAULT '',
		cover_url                 TEXT         NOT NULL DEFAULT '',
		location                  VARCHAR(100) NOT NULL DEFAULT '',
		website                   VARCHAR(255) NOT NULL DEFAULT '',
		role                      VARCHAR(20)  NOT NULL DEFAULT 'user'
		                            CHECK (role IN ('user','moderator','admin')),
		is_suspended              BOOLEAN      NOT NULL DEFAULT FALSE,
		suspension_reason         TEXT         NOT NULL DEFAULT '',
		email_verified            BOOLEAN      NOT NULL DEFAULT FALSE,
		email_verify_token        TEXT         NOT NULL DEFAULT '',
		email_verify_expires      TIMESTAMPTZ,
		password_reset_token      TEXT         NOT NULL DEFAULT '',
		password_reset_expires    TIMESTAMPTZ,
		profile_private           BOOLEAN      NOT NULL DEFAULT TRUE,
		deletion_requested_at     TIMESTAMPTZ,
		deletion_scheduled_at     TIMESTAMPTZ,
		is_remote                 BOOLEAN      NOT NULL DEFAULT FALSE,
		remote_instance           TEXT         NOT NULL DEFAULT '',
		remote_user_id            TEXT         NOT NULL DEFAULT '',
		federation_public_key     TEXT         NOT NULL DEFAULT '',
		federation_private_key    TEXT         NOT NULL DEFAULT '',
		email_notifications_enabled BOOLEAN    NOT NULL DEFAULT TRUE,
		created_at                TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		updated_at                TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	// Migration: add column if upgrading from older schema
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS email_notifications_enabled BOOLEAN NOT NULL DEFAULT TRUE`,
	`CREATE INDEX IF NOT EXISTS idx_users_username_trgm ON users USING gin(username gin_trgm_ops)`,
	`CREATE INDEX IF NOT EXISTS idx_users_dispname_trgm ON users USING gin(display_name gin_trgm_ops)`,
	`CREATE INDEX IF NOT EXISTS idx_users_remote        ON users(is_remote, remote_instance)`,

	// ── Friend groups ──────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS friend_groups (
		id         UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id    UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       VARCHAR(100) NOT NULL,
		created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		UNIQUE(user_id, name)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_fg_user ON friend_groups(user_id)`,

	`CREATE TABLE IF NOT EXISTS friend_group_members (
		group_id   UUID NOT NULL REFERENCES friend_groups(id) ON DELETE CASCADE,
		friend_id  UUID NOT NULL REFERENCES users(id)         ON DELETE CASCADE,
		PRIMARY KEY (group_id, friend_id)
	)`,

	// ── Friendships ────────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS friendships (
		id           UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		requester_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		addressee_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		status       VARCHAR(20) NOT NULL DEFAULT 'pending'
		               CHECK (status IN ('pending','accepted','declined','blocked')),
		created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE(requester_id, addressee_id),
		CHECK (requester_id <> addressee_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_fs_requester ON friendships(requester_id, status)`,
	`CREATE INDEX IF NOT EXISTS idx_fs_addressee ON friendships(addressee_id, status)`,

	// ── Posts ──────────────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS posts (
		id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		author_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		content         TEXT        NOT NULL DEFAULT '',
		image_url       TEXT        NOT NULL DEFAULT '',
		visibility      VARCHAR(20) NOT NULL DEFAULT 'friends'
		                  CHECK (visibility IN ('public','friends','group','private')),
		group_id        UUID        REFERENCES friend_groups(id) ON DELETE SET NULL,
		parent_id       UUID        REFERENCES posts(id)         ON DELETE CASCADE,
		repost_of_id    UUID        REFERENCES posts(id)         ON DELETE SET NULL,
		is_remote       BOOLEAN     NOT NULL DEFAULT FALSE,
		remote_post_id  TEXT        NOT NULL DEFAULT '',
		remote_instance TEXT        NOT NULL DEFAULT '',
		created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted_at      TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_posts_author  ON posts(author_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_posts_parent  ON posts(parent_id)`,
	`CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC)`,

	// ── Likes ──────────────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS likes (
		user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		post_id    UUID        NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (user_id, post_id)
	)`,

	// ── Reactions (AGORA-25) ──────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS reactions (
		user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		post_id         UUID        NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		reaction_type   VARCHAR(20) NOT NULL CHECK (reaction_type IN ('like','love','laugh','wow','angry','care','pride','thankful','vomit')),
		created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (user_id, post_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_reactions_post ON reactions(post_id)`,

	// ── Notifications ──────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS notifications (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		actor_id   UUID        REFERENCES users(id) ON DELETE SET NULL,
		type       VARCHAR(50) NOT NULL,
		post_id    UUID        REFERENCES posts(id) ON DELETE CASCADE,
		data       TEXT        NOT NULL DEFAULT '',
		read       BOOLEAN     NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_notif_user ON notifications(user_id, read, created_at DESC)`,

	// ── Reports ────────────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS reports (
		id                 UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
		reporter_id        UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		reported_user_id   UUID         REFERENCES users(id)  ON DELETE CASCADE,
		reported_post_id   UUID         REFERENCES posts(id)  ON DELETE CASCADE,
		reason             VARCHAR(100) NOT NULL,
		details            TEXT         NOT NULL DEFAULT '',
		status             VARCHAR(20)  NOT NULL DEFAULT 'pending'
		                     CHECK (status IN ('pending','reviewed','dismissed','actioned')),
		reviewed_by        UUID         REFERENCES users(id)  ON DELETE SET NULL,
		reviewed_at        TIMESTAMPTZ,
		review_notes       TEXT         NOT NULL DEFAULT '',
		created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status, created_at DESC)`,

	// ── Invite codes ───────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS invite_codes (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		code       TEXT        UNIQUE NOT NULL,
		created_by UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		used_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
		used_at    TIMESTAMPTZ,
		expires_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,

	// ── Instance settings ──────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS instance_settings (
		key        VARCHAR(100) PRIMARY KEY,
		value      TEXT         NOT NULL DEFAULT '',
		updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`INSERT INTO instance_settings (key, value) VALUES
		('instance_name',        'Agora'),
		('instance_description', 'A federated, privacy-first social network.'),
		('registration_mode',    'open'),
		('federation_enabled',   'false'),
		('deletion_grace_days',  '30'),
		('smtp_host',            ''),
		('smtp_port',            '587'),
		('smtp_user',            ''),
		('smtp_password',        ''),
		('smtp_from',            'noreply@localhost'),
		('smtp_enabled',         'false'),
		('logo_url',             ''),
		('atproto_enabled',      'false')
	ON CONFLICT (key) DO NOTHING`,

	// ── Federated instances ────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS federated_instances (
		id           UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		domain       TEXT        UNIQUE NOT NULL,
		name         TEXT        NOT NULL DEFAULT '',
		public_key   TEXT        NOT NULL DEFAULT '',
		instance_url TEXT        NOT NULL DEFAULT '',
		status       VARCHAR(20) NOT NULL DEFAULT 'active'
		               CHECK (status IN ('active','blocked','pending')),
		last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,

	// ── Audit log ──────────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS audit_log (
		id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		actor_id    UUID        REFERENCES users(id) ON DELETE SET NULL,
		action      TEXT        NOT NULL,
		target_type TEXT        NOT NULL DEFAULT '',
		target_id   TEXT        NOT NULL DEFAULT '',
		details     TEXT        NOT NULL DEFAULT '',
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor_id, created_at DESC)`,

	// ── Sessions ───────────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS sessions (
		id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash  TEXT        NOT NULL,
		user_agent  TEXT        NOT NULL DEFAULT '',
		ip_address  TEXT        NOT NULL DEFAULT '',
		last_active TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_user  ON sessions(user_id)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token_hash)`,

	// ── Community Groups ───────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS community_groups (
		id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		name        VARCHAR(100) NOT NULL,
		slug        VARCHAR(110) UNIQUE NOT NULL,
		description TEXT         NOT NULL DEFAULT '',
		cover_url   TEXT         NOT NULL DEFAULT '',
		avatar_url  TEXT         NOT NULL DEFAULT '',
		privacy     VARCHAR(20)  NOT NULL DEFAULT 'public'
		                CHECK (privacy IN ('public','private')),
		created_by  UUID         NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
		member_count INT         NOT NULL DEFAULT 1,
		post_count  INT          NOT NULL DEFAULT 0,
		created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_cg_slug    ON community_groups(slug)`,
	`CREATE INDEX IF NOT EXISTS idx_cg_created ON community_groups(created_at DESC)`,

	`CREATE TABLE IF NOT EXISTS community_group_members (
		group_id   UUID        NOT NULL REFERENCES community_groups(id) ON DELETE CASCADE,
		user_id    UUID        NOT NULL REFERENCES users(id)            ON DELETE CASCADE,
		role       VARCHAR(20) NOT NULL DEFAULT 'member'
		               CHECK (role IN ('owner','mod','member')),
		joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (group_id, user_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_cgm_user  ON community_group_members(user_id)`,
	`CREATE INDEX IF NOT EXISTS idx_cgm_group ON community_group_members(group_id, role)`,

	// community_group_posts links existing posts to a group
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS community_group_id UUID REFERENCES community_groups(id) ON DELETE SET NULL`,
	`CREATE INDEX IF NOT EXISTS idx_posts_community_group ON posts(community_group_id, created_at DESC)`,

	`CREATE TABLE IF NOT EXISTS community_group_invites (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		group_id   UUID        NOT NULL REFERENCES community_groups(id) ON DELETE CASCADE,
		token      TEXT        UNIQUE NOT NULL,
		created_by UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		max_uses   INT         NOT NULL DEFAULT 0,
		uses       INT         NOT NULL DEFAULT 0,
		expires_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_cgi_token ON community_group_invites(token)`,
	`CREATE INDEX IF NOT EXISTS idx_cgi_group ON community_group_invites(group_id)`,

	`CREATE TABLE IF NOT EXISTS community_group_join_requests (
		id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		group_id    UUID        NOT NULL REFERENCES community_groups(id) ON DELETE CASCADE,
		user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		message     TEXT        NOT NULL DEFAULT '',
		status      VARCHAR(20) NOT NULL DEFAULT 'pending'
		                CHECK (status IN ('pending','approved','rejected')),
		reviewed_by UUID        REFERENCES users(id) ON DELETE SET NULL,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		reviewed_at TIMESTAMPTZ,
		UNIQUE(group_id, user_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_cgjr_group ON community_group_join_requests(group_id, status)`,
	`CREATE INDEX IF NOT EXISTS idx_cgjr_user  ON community_group_join_requests(user_id)`,

	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS content_warning TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS cover_position TEXT NOT NULL DEFAULT '50% 50%'`,
	`ALTER TABLE community_groups ADD COLUMN IF NOT EXISTS cover_position TEXT NOT NULL DEFAULT '50% 50%'`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS link_url         TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS link_title       TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS link_description TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS link_image       TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS link_domain      TEXT NOT NULL DEFAULT ''`,

	`CREATE TABLE IF NOT EXISTS instance_rules (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		position   INT         NOT NULL DEFAULT 0,
		text       TEXT        NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_rules_position ON instance_rules(position ASC)`,

	// Outbound federation queue — retry table for failed/pending sends
	`CREATE TABLE IF NOT EXISTS federation_queue (
		id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		instance_url  TEXT        NOT NULL,
		payload       JSONB       NOT NULL,
		attempts      INT         NOT NULL DEFAULT 0,
		last_error    TEXT        NOT NULL DEFAULT '',
		next_attempt  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_fed_queue_next ON federation_queue(next_attempt ASC) WHERE attempts < 10`,

	// Track when remote user profiles were last synced
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS remote_synced_at TIMESTAMPTZ`,

	// Standard ActivityPub followers — remote actors (e.g. Mastodon accounts)
	// following a local user's public posts. Distinct from Agora's own
	// friendships/federation_queue, which serve the older Agora-to-Agora protocol.
	`CREATE TABLE IF NOT EXISTS ap_followers (
		id                  UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		followed_user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		follower_actor_url  TEXT        NOT NULL,
		follower_inbox_url  TEXT        NOT NULL,
		created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (followed_user_id, follower_actor_url)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_ap_followers_user ON ap_followers(followed_user_id)`,

	// Outbound ActivityPub delivery queue — separate from federation_queue
	// because standard HTTP Signatures must be computed at send time (fresh
	// Date header per attempt), not once at enqueue time.
	`CREATE TABLE IF NOT EXISTS ap_delivery_queue (
		id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		actor_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		inbox_url     TEXT        NOT NULL,
		activity      JSONB       NOT NULL,
		attempts      INT         NOT NULL DEFAULT 0,
		last_error    TEXT        NOT NULL DEFAULT '',
		next_attempt  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_ap_delivery_next ON ap_delivery_queue(next_attempt ASC) WHERE attempts < 10`,

	`CREATE TABLE IF NOT EXISTS albums (
		id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		owner_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title       VARCHAR(100) NOT NULL,
		description TEXT        NOT NULL DEFAULT '',
		cover_url   TEXT        NOT NULL DEFAULT '',
		visibility  VARCHAR(20) NOT NULL DEFAULT 'friends'
		                CHECK (visibility IN ('public','friends','private')),
		photo_count INT         NOT NULL DEFAULT 0,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_albums_owner   ON albums(owner_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_albums_public  ON albums(visibility, created_at DESC)`,

	`CREATE TABLE IF NOT EXISTS album_photos (
		id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		album_id    UUID        NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
		uploader_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		url         TEXT        NOT NULL,
		caption     TEXT        NOT NULL DEFAULT '',
		position    INT         NOT NULL DEFAULT 0,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_album_photos_album ON album_photos(album_id, position ASC, created_at ASC)`,

	// Pronouns support
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS pronouns VARCHAR(50) NOT NULL DEFAULT ''`,

	// ── Post notifications (AGORA-33) ─────────────────────────────────────
	// follower_id wants to be notified when followed_id posts
	`CREATE TABLE IF NOT EXISTS post_notifications (
		follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		followed_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (follower_id, followed_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_post_notif_followed ON post_notifications(followed_id)`,

	// ── Wall posts (AGORA-19) ──────────────────────────────────────────────
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS wall_user_id UUID REFERENCES users(id) ON DELETE CASCADE`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS wall_status  VARCHAR(20) NOT NULL DEFAULT 'approved'
		CHECK (wall_status IN ('approved','pending','rejected'))`,
	`CREATE INDEX IF NOT EXISTS idx_posts_wall ON posts(wall_user_id, wall_status, created_at DESC)`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS wall_approval_required BOOLEAN NOT NULL DEFAULT FALSE`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS expo_push_token TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS unsubscribe_token TEXT NOT NULL DEFAULT ''`,
	// Backfill unsubscribe tokens using uuid instead of gen_random_bytes
	`UPDATE users SET unsubscribe_token = replace(uuid_generate_v4()::text, '-', '') || replace(uuid_generate_v4()::text, '-', '') WHERE unsubscribe_token = ''`,

	// ── Polls (AGORA-5) ────────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS poll_options (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		post_id    UUID        NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		text       VARCHAR(200) NOT NULL,
		position   INT         NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_poll_options_post ON poll_options(post_id, position ASC)`,

	`CREATE TABLE IF NOT EXISTS poll_votes (
		user_id    UUID NOT NULL REFERENCES users(id)         ON DELETE CASCADE,
		option_id  UUID NOT NULL REFERENCES poll_options(id)  ON DELETE CASCADE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (user_id, option_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_poll_votes_option ON poll_votes(option_id)`,

	// ── Blocks (AGORA-45) ─────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS blocks (
		blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (blocker_id, blocked_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_blocks_blocker ON blocks(blocker_id)`,
	`CREATE INDEX IF NOT EXISTS idx_blocks_blocked ON blocks(blocked_id)`,

	// ── Direct Messages (AGORA-34) ─────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS conversations (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE TABLE IF NOT EXISTS conversation_participants (
		conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		user_id         UUID NOT NULL REFERENCES users(id)         ON DELETE CASCADE,
		last_read_at    TIMESTAMPTZ,
		read_receipts   BOOLEAN NOT NULL DEFAULT TRUE,
		is_accepted     BOOLEAN NOT NULL DEFAULT TRUE,
		PRIMARY KEY (conversation_id, user_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_conv_participants_user ON conversation_participants(user_id)`,
	`CREATE TABLE IF NOT EXISTS messages (
		id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		conversation_id UUID        NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		author_id       UUID        NOT NULL REFERENCES users(id)         ON DELETE CASCADE,
		content         TEXT        NOT NULL DEFAULT '',
		image_url       TEXT        NOT NULL DEFAULT '',
		edited_at       TIMESTAMPTZ,
		deleted_at      TIMESTAMPTZ,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages(conversation_id, created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS message_reactions (
		message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
		user_id    UUID NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
		reaction   VARCHAR(50) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (message_id, user_id)
	)`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS dm_privacy VARCHAR(20) NOT NULL DEFAULT 'everyone'`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS waitlist_status VARCHAR(20) NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS waitlist_token TEXT NOT NULL DEFAULT ''`,
	`CREATE INDEX IF NOT EXISTS idx_users_waitlist ON users(waitlist_status, created_at) WHERE waitlist_status = 'pending'`,
	`ALTER TABLE reactions DROP CONSTRAINT IF EXISTS reactions_reaction_type_check`,
	`ALTER TABLE reactions ADD CONSTRAINT reactions_reaction_type_check CHECK (reaction_type IN ('like','love','laugh','wow','angry','care','pride','thankful','vomit'))`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS poll_expires_at TIMESTAMPTZ`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS poll_multiple_choice BOOLEAN NOT NULL DEFAULT FALSE`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS poll_allows_new_options BOOLEAN NOT NULL DEFAULT FALSE`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS hide_timeline BOOLEAN NOT NULL DEFAULT FALSE`,
	// AGORA-74: Enhanced moderation
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS suspension_expires_at TIMESTAMPTZ`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS suspension_notes TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS banned_at TIMESTAMPTZ`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS ban_reason TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS ban_notes TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE reports ADD COLUMN IF NOT EXISTS violation_type VARCHAR(50) NOT NULL DEFAULT ''`,
	`ALTER TABLE reports ADD COLUMN IF NOT EXISTS rule_id UUID REFERENCES instance_rules(id) ON DELETE SET NULL`,
	`ALTER TABLE reports ADD COLUMN IF NOT EXISTS reported_comment_id UUID REFERENCES posts(id) ON DELETE CASCADE`,
	`CREATE TABLE IF NOT EXISTS instance_bans (
		id           UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		instance     TEXT        NOT NULL UNIQUE,
		reason       TEXT        NOT NULL DEFAULT '',
		notes        TEXT        NOT NULL DEFAULT '',
		banned_by    UUID        REFERENCES users(id) ON DELETE SET NULL,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`ALTER TABLE reports ALTER COLUMN reason SET DEFAULT ''`,

	// ── Email change (AGORA-81) ────────────────────────────────────────────
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS pending_email TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS email_change_token TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS email_change_expires TIMESTAMPTZ`,

	// ── Online presence (AGORA-91) ─────────────────────────────────────────
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS last_active_at TIMESTAMPTZ`,

	// ── Album friend-list privacy (AGORA-64) ──────────────────────────────
	`ALTER TABLE albums DROP CONSTRAINT IF EXISTS albums_visibility_check`,
	`ALTER TABLE albums ADD CONSTRAINT albums_visibility_check CHECK (visibility IN ('public','friends','group','private'))`,
	`ALTER TABLE albums ADD COLUMN IF NOT EXISTS friend_group_id UUID REFERENCES friend_groups(id) ON DELETE SET NULL`,

	// ── Custom feeds (AGORA-99) ────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS custom_feeds (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		owner_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name       VARCHAR(100) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_custom_feeds_owner ON custom_feeds(owner_id, created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS custom_feed_filters (
		id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		feed_id     UUID        NOT NULL REFERENCES custom_feeds(id) ON DELETE CASCADE,
		filter_type VARCHAR(30) NOT NULL CHECK (filter_type IN ('friend_group','community_group','exclude_friend','exclude_group','post_type')),
		value       TEXT        NOT NULL,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	// AGORA-111/AGORA-146: filter_type's allowed set is defined once, further
	// down (search custom_feed_filters_filter_type_check) — it must stay a
	// single statement covering every currently-valid value. This migration
	// runner re-executes the whole schema list on every startup with no
	// "already applied" tracking, so an earlier, narrower version of this
	// same ALTER TABLE...ADD CONSTRAINT would permanently fail once any row
	// used a value only the later version allows (exactly what happened when
	// AGORA-146 added a second such statement instead of widening this one).
	`CREATE INDEX IF NOT EXISTS idx_custom_feed_filters_feed ON custom_feed_filters(feed_id)`,

	// ── Multi-photo posts (AGORA-93) ───────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS post_photos (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		post_id    UUID        NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		url        TEXT        NOT NULL,
		position   INT         NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_post_photos_post ON post_photos(post_id, position ASC)`,

	// ── Pages (AGORA-106) ─────────────────────────────────────────────────
	`CREATE TABLE IF NOT EXISTS pages (
		id               UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
		slug             VARCHAR(50)  UNIQUE NOT NULL,
		display_name     VARCHAR(100) NOT NULL,
		bio              TEXT         NOT NULL DEFAULT '',
		avatar_url       TEXT         NOT NULL DEFAULT '',
		cover_url        TEXT         NOT NULL DEFAULT '',
		cover_position   TEXT         NOT NULL DEFAULT '50% 50%',
		page_type        VARCHAR(30)  NOT NULL DEFAULT ''
		                   CHECK (page_type IN ('band','business','organization','creator','')),
		owner_id         UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		privacy          VARCHAR(20)  NOT NULL DEFAULT 'public'
		                   CHECK (privacy IN ('public','private')),
		subscriber_count INT          NOT NULL DEFAULT 0,
		post_count       INT          NOT NULL DEFAULT 0,
		is_verified      BOOLEAN      NOT NULL DEFAULT FALSE,
		is_featured      BOOLEAN      NOT NULL DEFAULT FALSE,
		created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
		updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_pages_slug    ON pages(slug)`,
	`CREATE INDEX IF NOT EXISTS idx_pages_owner   ON pages(owner_id)`,
	`CREATE INDEX IF NOT EXISTS idx_pages_popular ON pages(subscriber_count DESC) WHERE privacy = 'public'`,

	`CREATE TABLE IF NOT EXISTS page_subscribers (
		page_id    UUID        NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
		user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (page_id, user_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_page_subs_user ON page_subscribers(user_id)`,
	`CREATE INDEX IF NOT EXISTS idx_page_subs_page ON page_subscribers(page_id)`,

	// Link posts to a page
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS page_id UUID REFERENCES pages(id) ON DELETE SET NULL`,
	`CREATE INDEX IF NOT EXISTS idx_posts_page ON posts(page_id, created_at DESC)`,

	// ── Feed interaction tracking (AGORA-102) ─────────────────────────────
	`CREATE TABLE IF NOT EXISTS feed_interactions (
		id               UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		target_user_id   UUID        REFERENCES users(id) ON DELETE CASCADE,
		post_id          UUID        REFERENCES posts(id) ON DELETE CASCADE,
		interaction_type VARCHAR(30) NOT NULL
		                   CHECK (interaction_type IN ('like','comment','repost','profile_view','link_click','post_view')),
		created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_feed_int_user    ON feed_interactions(user_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_feed_int_target  ON feed_interactions(target_user_id, created_at DESC)`,

	// ── AGORA-128: page reporting (must come after pages table is created) ────
	`ALTER TABLE reports ADD COLUMN IF NOT EXISTS reported_page_id UUID REFERENCES pages(id) ON DELETE CASCADE`,

	// ── AGORA-119: video posts ─────────────────────────────────────────────
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS video_url       TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS video_thumb_url TEXT NOT NULL DEFAULT ''`,

	// ── AGORA-113: page analytics events ──────────────────────────────────
	`CREATE TABLE IF NOT EXISTS page_analytics_events (
		id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		page_id    UUID        NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
		event_type VARCHAR(30) NOT NULL
		             CHECK (event_type IN ('subscribe','unsubscribe','post_view','post_like','post_comment','post_repost')),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_pae_page_time ON page_analytics_events(page_id, created_at DESC)`,
	`ALTER TABLE custom_feeds ADD COLUMN IF NOT EXISTS smart_ranking BOOLEAN NOT NULL DEFAULT false`,

	// ── AGORA-137: async video transcoding jobs ───────────────────────────
	`CREATE TABLE IF NOT EXISTS video_transcode_jobs (
		id              UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		status          VARCHAR(20) NOT NULL DEFAULT 'processing'
		                  CHECK (status IN ('processing','done','failed')),
		video_url       TEXT        NOT NULL DEFAULT '',
		video_thumb_url TEXT        NOT NULL DEFAULT '',
		error           TEXT        NOT NULL DEFAULT '',
		created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_video_jobs_user ON video_transcode_jobs(user_id, created_at DESC)`,

	// AGORA-145: per-account opt-out of standard ActivityPub federation,
	// separate from the instance-wide federation_enabled toggle.
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS activitypub_enabled BOOLEAN NOT NULL DEFAULT true`,

	// AGORA-147: remote-actor identity for standard-ActivityPub user stubs
	// (distinct from remote_user_id/remote_instance, which the older custom
	// protocol's stubs use), plus idempotency for inbound remote posts/replies.
	// The posts unique index also fixes a pre-existing bug in the old custom
	// protocol's handleInboundPost, whose bare ON CONFLICT DO NOTHING had no
	// matching constraint to target — Postgres's bare ON CONFLICT DO NOTHING
	// applies to any unique-constraint violation on the table, so this index
	// makes that existing code correctly dedupe too.
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS ap_actor_url TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS ap_inbox_url TEXT NOT NULL DEFAULT ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ap_actor_url ON users(ap_actor_url) WHERE ap_actor_url != ''`,
	// Defensive cleanup before adding the constraint below: the old protocol's
	// dedup bug means duplicate (remote_post_id, remote_instance) rows may
	// already exist. Keep the earliest row per pair, drop the rest. No-op if
	// there are no duplicates (idempotent, safe to run on every startup).
	`DELETE FROM posts a USING posts b
	 WHERE a.is_remote = true AND a.remote_post_id != ''
	   AND b.is_remote = true AND b.remote_post_id != ''
	   AND a.remote_post_id = b.remote_post_id AND a.remote_instance = b.remote_instance
	   AND a.id > b.id`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_posts_remote_unique ON posts(remote_post_id, remote_instance) WHERE is_remote = true AND remote_post_id != ''`,

	// AGORA-115: standard ActivityPub federation for Pages. Each page gets
	// its own actor identity and keypair, independent of any member's
	// personal user actor. Followers/delivery queue are separate tables from
	// ap_followers/ap_delivery_queue (both NOT NULL FKs to users) rather than
	// widening those constraints on a live, populated table.
	`ALTER TABLE pages ADD COLUMN IF NOT EXISTS federation_public_key TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE pages ADD COLUMN IF NOT EXISTS federation_private_key TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE pages ADD COLUMN IF NOT EXISTS activitypub_enabled BOOLEAN NOT NULL DEFAULT true`,

	`CREATE TABLE IF NOT EXISTS page_remote_subscribers (
		id                 UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		page_id            UUID        NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
		follower_actor_url TEXT        NOT NULL,
		follower_inbox_url TEXT        NOT NULL,
		created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (page_id, follower_actor_url)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_page_remote_subs_page ON page_remote_subscribers(page_id)`,

	`CREATE TABLE IF NOT EXISTS page_ap_delivery_queue (
		id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		actor_page_id UUID        NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
		inbox_url     TEXT        NOT NULL,
		activity      JSONB       NOT NULL,
		attempts      INT         NOT NULL DEFAULT 0,
		last_error    TEXT        NOT NULL DEFAULT '',
		next_attempt  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_page_ap_delivery_next ON page_ap_delivery_queue(next_attempt ASC) WHERE attempts < 10`,

	// AGORA-112: page team membership/roles. Referenced extensively by
	// internal/pages/pages.go (ListMembers, InviteMember, AcceptInvite,
	// SetMemberRole, RemoveMember, hasRole) but had no migration anywhere in
	// this file — every one of those handlers has been failing at runtime
	// with a "relation page_members does not exist" error. The page owner
	// itself is tracked via pages.owner_id, not a row here; only invited
	// admin/editor members get a page_members row (accepted=false until the
	// invite is accepted).
	`CREATE TABLE IF NOT EXISTS page_members (
		page_id    UUID        NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
		user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role       VARCHAR(20) NOT NULL CHECK (role IN ('admin', 'editor')),
		accepted   BOOLEAN     NOT NULL DEFAULT false,
		joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (page_id, user_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_page_members_user ON page_members(user_id)`,

	// AGORA-146: outbound fediverse follows — the reverse of ap_followers
	// ("who follows me"), this is "who I follow" on the fediverse. accepted
	// stays false until the remote instance's Accept arrives (async, unlike
	// ap_followers which only ever records already-confirmed followers).
	`CREATE TABLE IF NOT EXISTS ap_following (
		id                 UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		follower_user_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		followed_actor_url TEXT        NOT NULL,
		followed_inbox_url TEXT        NOT NULL,
		accepted           BOOLEAN     NOT NULL DEFAULT false,
		created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (follower_user_id, followed_actor_url)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_ap_following_actor ON ap_following(followed_actor_url)`,

	// AGORA-146/195: custom-feed filter types surface followed fediverse and
	// (later) Bluesky accounts through the existing custom-feeds engine
	// rather than a new timeline UI — same drop-and-readd pattern AGORA-111
	// used to add include_page/exclude_page. This must stay the *only*
	// DROP+ADD pair for this constraint in the whole migration list: since
	// every statement here replays on every boot (no per-migration tracking
	// table), a second drop-and-narrower-readd pair added later for a new
	// filter type would transiently drop back to this older, narrower CHECK
	// and fail on any row already using the newer type — a bug Agora's own
	// AGORA-195 introduced by adding a second pair instead of widening this
	// one, fixed here.
	// Extend the list in place here going forward.
	`ALTER TABLE custom_feed_filters DROP CONSTRAINT IF EXISTS custom_feed_filters_filter_type_check`,
	`ALTER TABLE custom_feed_filters ADD CONSTRAINT custom_feed_filters_filter_type_check
		CHECK (filter_type IN ('friend_group','community_group','exclude_friend','exclude_group','post_type','include_page','exclude_page','fediverse_account','fediverse_all','atproto_account','atproto_all'))`,

	// AGORA-164: remote-actor stubs (created by upsertRemoteAPUser) never
	// explicitly set profile_private, which defaults to TRUE — every
	// followed fediverse account's stub was created private, so GetPost's
	// non-author access check 403'd its permalink for everyone (the post
	// still rendered fine inside a custom feed, which doesn't check
	// profile_private). upsertRemoteAPUser itself now sets it to false for
	// new/updated stubs; this backfills rows created before that fix.
	`UPDATE users SET profile_private = false WHERE is_remote = true AND ap_actor_url != '' AND profile_private = true`,

	// AGORA-164: per-account opt-out for the fediverse_post notification
	// specifically, distinct from activitypub_enabled (which controls
	// whether YOUR OWN posts federate, not whether you're notified about
	// accounts you follow).
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS fediverse_notifications_enabled BOOLEAN NOT NULL DEFAULT true`,

	// AGORA-166: per-followed-account notification opt-in. Defaults false —
	// following a fediverse account should not, by itself, start notifying
	// you of their posts, mirroring how following a local profile doesn't
	// either. fediverse_notifications_enabled above is still checked too and
	// remains the global kill switch; both must be true for a notification
	// to fire.
	`ALTER TABLE ap_following ADD COLUMN IF NOT EXISTS notify BOOLEAN NOT NULL DEFAULT false`,

	// AGORA-170: records a remote actor's Block of a local user, keyed by
	// inbox URL (not just actor URL) so enqueueAPDelivery — which only ever
	// sees an inbox URL, not the actor behind it — can filter every outbound
	// delivery path (followers broadcast, direct replies, mentions, likes,
	// announces) from one central place, rather than needing a guard at each
	// of its dozen-plus call sites. The fediverse-facing analog of the local
	// blocks table, which only governs local-to-local visibility.
	`CREATE TABLE IF NOT EXISTS ap_blocked_by (
		id                  UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		local_user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		blocker_actor_url   TEXT        NOT NULL,
		blocker_inbox_url   TEXT        NOT NULL DEFAULT '',
		created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (local_user_id, blocker_actor_url)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_ap_blocked_by_inbox ON ap_blocked_by(local_user_id, blocker_inbox_url)`,

	// ── AT Protocol / Bluesky identity (AGORA-187) ────────────────────────────
	// Separate columns from federation_public_key/federation_private_key —
	// AT Proto signs with secp256k1, not RSA, and the key material has no PEM
	// convention of its own (stored as hex-encoded raw bytes instead).
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS atproto_did TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS atproto_private_key TEXT NOT NULL DEFAULT ''`,

	// ── AT Protocol repo storage (AGORA-189) ──────────────────────────────────
	// atproto_repo_head is the current signed commit CID (as text) — the
	// entry point for reopening a user's repo/MST across requests, mirroring
	// how a git ref points at a commit rather than the tree contents
	// themselves. atproto_blocks is the content-addressed block store the MST
	// and every record/commit object actually live in; scoped by user_id
	// (rather than a single global cid-keyed table) so an account deletion
	// can drop a user's entire repo with one DELETE, without having to walk
	// the MST to figure out which blocks are reachable from their root.
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS atproto_repo_head TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS atproto_blocks (
		user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		cid      TEXT NOT NULL,
		data     BYTEA NOT NULL,
		PRIMARY KEY (user_id, cid)
	)`,

	// ── AT Protocol post records (AGORA-190) ──────────────────────────────────
	// Maps an Agora post to the repo record that federates it — the rkey
	// (TID) CreateRecord mints is only ever produced at creation time, so it
	// has to be persisted then or it's unrecoverable later. record_cid is
	// stored alongside for AGORA-199/201's reply/like strong-refs, which
	// need to point at a specific record version, not just "whatever's at
	// this rkey right now."
	`CREATE TABLE IF NOT EXISTS atproto_posts (
		post_id     UUID PRIMARY KEY REFERENCES posts(id) ON DELETE CASCADE,
		user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		rkey        TEXT NOT NULL,
		record_cid  TEXT NOT NULL,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,

	// ── AT Proto firehose (AGORA-191) ─────────────────────────────────────────
	// atproto_repo_rev tracks each repo's last-emitted rev string (distinct
	// from atproto_repo_head, the commit *CID* used to reopen the MST) —
	// firehose commit events carry a "since" field naming the *previous* rev,
	// which nothing else persists. atproto_firehose_events is the durable,
	// replayable event log a subscribeRepos client's cursor reconnects
	// against — stored in Postgres rather than indigo's own disk/Pebble
	// persister options, consistent with the rest of Agora's storage.
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS atproto_repo_rev TEXT NOT NULL DEFAULT ''`,
	`CREATE SEQUENCE IF NOT EXISTS atproto_firehose_seq`,
	`CREATE TABLE IF NOT EXISTS atproto_firehose_events (
		seq        BIGINT PRIMARY KEY,
		data       BYTEA NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,

	// ── Unified connections (AGORA-182) ───────────────────────────────────────
	// show_in_feed opts a specific fediverse follow into the main feed —
	// default off, since a followed account's posts otherwise only surface in
	// a friend list or custom feed the user deliberately built. Mirrors
	// notify's per-follow, off-by-default shape rather than a global switch,
	// so a few noisy accounts don't force an all-or-nothing choice.
	`ALTER TABLE ap_following ADD COLUMN IF NOT EXISTS show_in_feed BOOLEAN NOT NULL DEFAULT false`,

	// ── AT Proto opt-out toggle (AGORA-193) ───────────────────────────────────
	// Independent pair of flags from activitypub_enabled — a user/instance may
	// want ActivityPub on and AT Proto off or vice versa, different networks
	// with different tradeoffs. Per-account defaults true (opt-out), matching
	// activitypub_enabled's own default; the instance-wide flag defaults off
	// (checked in atprotoEnabled() below, absent-key-means-off) since — unlike
	// AGORA-156's AP toggle, which defaulted on to avoid yanking discoverability
	// out from under instances that already had federation configured — no
	// instance has AT Proto configured yet, so there's nothing to preserve by
	// defaulting it on.
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS atproto_enabled BOOLEAN NOT NULL DEFAULT true`,

	// ── Native Bluesky following (AGORA-195) ──────────────────────────────────
	// Deliberately not reusing ap_following: an AT Proto follow is an
	// app.bsky.graph.follow record written into the local user's own repo (an
	// outbound repo write), not an inbox-delivered activity requiring an
	// Accept/Reject handshake — no "accepted" boolean, since AT Proto follows
	// are asymmetric and unilateral. rkey/record_cid mirror atproto_posts'
	// shape, needed to delete the right repo record on unfollow. display_name/
	// avatar_url are cached from the resolve-time profile lookup for display —
	// there's no per-DID "cached remote user" row the way fediverse follows
	// get one, since that's populated by inbound Note ingestion, which has no
	// AT Proto equivalent until AGORA-197.
	`CREATE TABLE IF NOT EXISTS at_following (
		id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
		local_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		remote_did    TEXT        NOT NULL,
		remote_handle TEXT        NOT NULL DEFAULT '',
		display_name  TEXT        NOT NULL DEFAULT '',
		avatar_url    TEXT        NOT NULL DEFAULT '',
		rkey          TEXT        NOT NULL DEFAULT '',
		record_cid    TEXT        NOT NULL DEFAULT '',
		created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE (local_user_id, remote_did)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_at_following_user ON at_following(local_user_id)`,

	// atproto_account/atproto_all custom-feed filter types (analogous to
	// fediverse_account/fediverse_all, AGORA-146) were added to the single
	// authoritative filter_type CHECK constraint above (search
	// custom_feed_filters_filter_type_check) rather than a new drop-and-readd
	// pair here — see that statement's own comment for why a second pair
	// would be a latent bug, not just redundant.

	// ── Bluesky post ingestion (AGORA-197) ────────────────────────────────────
	// Cached local stub for a followed Bluesky account, mirroring
	// ap_actor_url's shape/index — keyed by DID (stable) rather than handle
	// (which can change), same reasoning ap_actor_url uses actor URL over a
	// display name.
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS atproto_remote_did TEXT NOT NULL DEFAULT ''`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_atproto_remote_did ON users(atproto_remote_did) WHERE atproto_remote_did != ''`,

	// AGORA-198: notify on a followed Bluesky account's new posts, mirroring
	// fediverse_notifications_enabled (global kill switch) and
	// ap_following.notify (AGORA-166's per-follow opt-in, default false since
	// following alone shouldn't start notifying you).
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS atproto_notifications_enabled BOOLEAN NOT NULL DEFAULT true`,
	`ALTER TABLE at_following ADD COLUMN IF NOT EXISTS notify BOOLEAN NOT NULL DEFAULT false`,

	// AGORA-199: a Bluesky reply strong-ref needs both a URI and a CID —
	// remote_post_id already carries the URI (shared with fediverse's own
	// remote_post_id convention), but AP has no CID equivalent, so this
	// column is AT-Proto-specific rather than folded into the existing one.
	`ALTER TABLE posts ADD COLUMN IF NOT EXISTS remote_post_cid TEXT NOT NULL DEFAULT ''`,

	// AGORA-201: tracks outbound app.bsky.feed.like/repost records this
	// instance wrote into a local user's own repo, keyed by the target post
	// (not a separate row id) since a user can only like/repost a given post
	// once — needed to find the record's rkey again on unlike/unrepost,
	// mirroring atproto_posts' role for app.bsky.feed.post records.
	`CREATE TABLE IF NOT EXISTS atproto_reactions (
		post_id    UUID        NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		kind       VARCHAR(10) NOT NULL CHECK (kind IN ('like','repost')),
		rkey       TEXT        NOT NULL,
		record_cid TEXT        NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (post_id, user_id, kind)
	)`,
}
