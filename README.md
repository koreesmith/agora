# Agora

An open-source, federated, privacy-first social network. Facebook-style mutual
friendships (not follows), friend groups, fine-grained post visibility, and
opt-in cross-instance federation with Ed25519 signatures.

## Quick start

```bash
cp .env.example .env          # edit JWT_SECRET and INSTANCE_DOMAIN
docker compose up -d --build
```

Visit http://localhost and sign in as **admin / admin**.  
⚠️ You must change the admin password before other users can register.

## Development

```bash
# Backend (requires Go 1.22, Postgres, Redis running)
make tidy
make run

# Frontend
cd frontend && npm install && npm run dev
# → http://localhost:3000 (proxies /api to :8080)
```

## Architecture

```
agora/
├── cmd/server/main.go          # Entry point
├── internal/
│   ├── config/                 # Env config
│   ├── store/                  # DB connection + schema migrations
│   ├── auth/                   # JWT auth, register, login, email verify
│   ├── users/                  # Profiles, GDPR export/deletion
│   ├── friends/                # Mutual friend requests + friend groups
│   ├── feed/                   # Posts, comments, likes, reposts
│   ├── notifications/          # In-app + SMTP email notifications
│   ├── search/                 # User search (local)
│   ├── moderation/             # Reports, suspension
│   ├── admin/                  # Settings, user mgmt, invites, audit log
│   ├── federation/             # Ed25519 cross-instance protocol
│   └── media/                  # File uploads
├── frontend/
│   ├── src/
│   │   ├── api/                # Typed API client
│   │   ├── store/              # Zustand auth store
│   │   ├── components/         # Layout, Feed, PostCard, etc.
│   │   └── pages/              # All route pages
│   ├── Dockerfile.frontend
│   └── nginx.conf              # Reverse proxy to backend
├── Dockerfile                  # Go binary builder
├── docker-compose.yml
├── Makefile
└── .env.example
```

## Stack

| Layer    | Technology                         |
|----------|------------------------------------|
| Backend  | Go 1.22, chi router                |
| Database | PostgreSQL 16 (pg_trgm, uuid-ossp) |
| Cache    | Redis 7                            |
| Frontend | React 18, TypeScript, Tailwind CSS |
| Auth     | JWT (HS256), bcrypt                |
| Email    | SMTP via gomail                    |
| Files    | Local disk (./data/uploads)        |
| Proxy    | nginx (docker) / any TLS terminator|
| Federation | Ed25519 signed REST activities   |

## Federation

Federation is **disabled by default**. Enable it in the Admin panel.

When enabled, your instance exposes `/.well-known/agora-instance` and
`/federation/inbox`. Remote instances can:
- Discover your public key
- Send signed activities (posts, friend requests, accepts)
- Search your public users

Only `public` visibility posts are federated.

## Default admin

Username: `admin`  
Password: `admin`  
Registration is locked until this password is changed.
