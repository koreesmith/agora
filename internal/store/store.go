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
		('logo_url',             '')
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
	// AGORA-111: add page filter types
	`ALTER TABLE custom_feed_filters DROP CONSTRAINT IF EXISTS custom_feed_filters_filter_type_check`,
	`ALTER TABLE custom_feed_filters ADD CONSTRAINT custom_feed_filters_filter_type_check
		CHECK (filter_type IN ('friend_group','community_group','exclude_friend','exclude_group','post_type','include_page','exclude_page'))`,
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
}
