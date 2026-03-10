package database

import (
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

func Connect(databaseURL string) (*sqlx.DB, error) {
	db, err := sqlx.Connect("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	return db, nil
}

func Migrate(db *sqlx.DB) error {
	log.Println("Running database migrations...")
	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	log.Println("Migrations complete")
	return nil
}

func SeedAdmin(db *sqlx.DB) error {
	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM users WHERE username = 'admin'")
	if err != nil || count > 0 {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO users (username, email, password_hash, display_name, role, email_verified, must_change_password)
		VALUES ('admin', 'admin@localhost', $1, 'Administrator', 'admin', true, true)
	`, string(hash))
	if err != nil {
		return fmt.Errorf("seeding admin: %w", err)
	}
	log.Println("Default admin account created (username: admin, password: admin) — CHANGE THIS IMMEDIATELY")
	return nil
}

var migrations = []string{
	`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
	`CREATE EXTENSION IF NOT EXISTS "pg_trgm"`,

	// Users
	`CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		username VARCHAR(50) UNIQUE NOT NULL,
		email VARCHAR(255) UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		display_name VARCHAR(100),
		bio TEXT DEFAULT '',
		avatar_url TEXT DEFAULT '',
		cover_url TEXT DEFAULT '',
		location VARCHAR(100) DEFAULT '',
		website VARCHAR(255) DEFAULT '',
		role VARCHAR(20) DEFAULT 'user' CHECK (role IN ('user', 'moderator', 'admin')),
		is_suspended BOOLEAN DEFAULT FALSE,
		suspension_reason TEXT DEFAULT '',
		email_verified BOOLEAN DEFAULT FALSE,
		must_change_password BOOLEAN DEFAULT FALSE,
		profile_private BOOLEAN DEFAULT TRUE,
		federated_id TEXT DEFAULT '',
		home_instance TEXT DEFAULT '',
		is_remote BOOLEAN DEFAULT FALSE,
		deletion_requested_at TIMESTAMPTZ,
		deletion_scheduled_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		updated_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`,
	`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
	`CREATE INDEX IF NOT EXISTS idx_users_username_trgm ON users USING gin(username gin_trgm_ops)`,
	`CREATE INDEX IF NOT EXISTS idx_users_display_name_trgm ON users USING gin(display_name gin_trgm_ops)`,

	// Email verification tokens
	`CREATE TABLE IF NOT EXISTS email_verification_tokens (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token TEXT NOT NULL UNIQUE,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	// Password reset tokens
	`CREATE TABLE IF NOT EXISTS password_reset_tokens (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token TEXT NOT NULL UNIQUE,
		expires_at TIMESTAMPTZ NOT NULL,
		used BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	// Friend groups
	`CREATE TABLE IF NOT EXISTS friend_groups (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name VARCHAR(100) NOT NULL,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	// Friend group members
	`CREATE TABLE IF NOT EXISTS friend_group_members (
		group_id UUID NOT NULL REFERENCES friend_groups(id) ON DELETE CASCADE,
		friend_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		PRIMARY KEY (group_id, friend_id)
	)`,

	// Friendships
	`CREATE TABLE IF NOT EXISTS friendships (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		addressee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		status VARCHAR(20) DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'blocked')),
		created_at TIMESTAMPTZ DEFAULT NOW(),
		updated_at TIMESTAMPTZ DEFAULT NOW(),
		UNIQUE(requester_id, addressee_id)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_friendships_requester ON friendships(requester_id)`,
	`CREATE INDEX IF NOT EXISTS idx_friendships_addressee ON friendships(addressee_id)`,

	// Posts
	`CREATE TABLE IF NOT EXISTS posts (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		media_urls TEXT[] DEFAULT '{}',
		visibility VARCHAR(20) DEFAULT 'friends' CHECK (visibility IN ('public', 'friends', 'group', 'private')),
		group_id UUID REFERENCES friend_groups(id) ON DELETE SET NULL,
		repost_of UUID REFERENCES posts(id) ON DELETE SET NULL,
		federated_id TEXT DEFAULT '',
		home_instance TEXT DEFAULT '',
		is_remote BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		updated_at TIMESTAMPTZ DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	)`,

	`CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author_id)`,
	`CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC)`,

	// Comments
	`CREATE TABLE IF NOT EXISTS comments (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		parent_id UUID REFERENCES comments(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		media_urls TEXT[] DEFAULT '{}',
		federated_id TEXT DEFAULT '',
		created_at TIMESTAMPTZ DEFAULT NOW(),
		deleted_at TIMESTAMPTZ
	)`,

	`CREATE INDEX IF NOT EXISTS idx_comments_post ON comments(post_id)`,

	// Likes
	`CREATE TABLE IF NOT EXISTS likes (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		post_id UUID REFERENCES posts(id) ON DELETE CASCADE,
		comment_id UUID REFERENCES comments(id) ON DELETE CASCADE,
		created_at TIMESTAMPTZ DEFAULT NOW(),
		UNIQUE(user_id, post_id),
		UNIQUE(user_id, comment_id)
	)`,

	// Notifications
	`CREATE TABLE IF NOT EXISTS notifications (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
		type VARCHAR(50) NOT NULL,
		entity_type VARCHAR(50) DEFAULT '',
		entity_id UUID,
		content TEXT DEFAULT '',
		read BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	`CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, created_at DESC)`,

	// Reports
	`CREATE TABLE IF NOT EXISTS reports (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		reported_user_id UUID REFERENCES users(id) ON DELETE CASCADE,
		reported_post_id UUID REFERENCES posts(id) ON DELETE CASCADE,
		reported_comment_id UUID REFERENCES comments(id) ON DELETE CASCADE,
		reason VARCHAR(100) NOT NULL,
		details TEXT DEFAULT '',
		status VARCHAR(20) DEFAULT 'pending' CHECK (status IN ('pending', 'reviewed', 'dismissed', 'actioned')),
		reviewed_by UUID REFERENCES users(id),
		reviewed_at TIMESTAMPTZ,
		review_notes TEXT DEFAULT '',
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	`CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status, created_at DESC)`,

	// Invite codes
	`CREATE TABLE IF NOT EXISTS invite_codes (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		code TEXT NOT NULL UNIQUE,
		created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		used_by UUID REFERENCES users(id),
		used_at TIMESTAMPTZ,
		expires_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	// Instance settings
	`CREATE TABLE IF NOT EXISTS instance_settings (
		key VARCHAR(100) PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	// Seed default instance settings
	`INSERT INTO instance_settings (key, value) VALUES
		('instance_name', 'Agora'),
		('instance_description', 'A federated social network focused on privacy and community.'),
		('registration_mode', 'open'),
		('federation_enabled', 'false'),
		('max_post_length', '5000'),
		('max_bio_length', '500'),
		('deletion_grace_days', '30'),
		('smtp_host', ''),
		('smtp_port', '587'),
		('smtp_user', ''),
		('smtp_password', ''),
		('smtp_from', 'noreply@localhost'),
		('smtp_enabled', 'false')
	ON CONFLICT (key) DO NOTHING`,

	// Federated instances
	`CREATE TABLE IF NOT EXISTS federated_instances (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		domain TEXT NOT NULL UNIQUE,
		name TEXT DEFAULT '',
		public_key TEXT NOT NULL,
		instance_url TEXT NOT NULL,
		status VARCHAR(20) DEFAULT 'active' CHECK (status IN ('active', 'blocked', 'pending')),
		last_seen_at TIMESTAMPTZ DEFAULT NOW(),
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	// Audit log
	`CREATE TABLE IF NOT EXISTS audit_log (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
		action TEXT NOT NULL,
		target_type TEXT DEFAULT '',
		target_id TEXT DEFAULT '',
		details JSONB DEFAULT '{}',
		created_at TIMESTAMPTZ DEFAULT NOW()
	)`,

	`CREATE INDEX IF NOT EXISTS idx_audit_log_actor ON audit_log(actor_id, created_at DESC)`,
}
