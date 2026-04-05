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

## Developer Documentation

Full developer documentation is available at **/docs** when running the server, and in the [`docs/`](docs/) directory.

- [Quick Start & Local Dev](docs/getting-started.md)
- [System Architecture](docs/architecture.md)
- [Database Schema](docs/database.md)
- [Backend Services](docs/backend/)
- [API Reference](docs/api/)
- [Frontend](docs/frontend/)
- [Deployment](docs/deployment.md)

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
│   ├── feed/                   # Posts, comments, likes, reactions, reposts
│   ├── notifications/          # In-app + SMTP email notifications
│   ├── search/                 # User search (local)
│   ├── moderation/             # Reports, suspension
│   ├── admin/                  # Settings, user mgmt, invites, audit log
│   ├── federation/             # Ed25519 cross-instance protocol
│   ├── media/                  # File uploads
│   ├── groups/                 # Community groups
│   ├── albums/                 # Photo albums
│   ├── dm/                     # Direct messages + WebSocket
│   └── blocks/                 # User blocking
├── frontend/
│   ├── src/
│   │   ├── api/                # Typed API client
│   │   ├── store/              # Zustand auth store
│   │   ├── components/         # Layout, Feed, PostCard, etc.
│   │   └── pages/              # All route pages
│   ├── Dockerfile.frontend
│   └── nginx.conf              # Reverse proxy to backend
├── docs/                       # Developer documentation (served at /docs)
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

---

## SSL / HTTPS Setup

Agora includes an automated SSL setup using Let's Encrypt and Certbot.

### Prerequisites

- A domain name with DNS pointing to your server's IP
- Port 80 and 443 open on your firewall
- Docker installed

### One-time setup

```bash
./setup-ssl.sh yourdomain.com you@email.com
```

This script will:
1. Configure nginx with your domain
2. Temporarily start nginx on port 80 to serve the ACME challenge
3. Run certbot to obtain a certificate from Let's Encrypt
4. Generate `docker-compose.ssl.yml` with the full SSL stack

### Start with SSL

```bash
docker compose -f docker-compose.ssl.yml up -d
```

### Renewal

The `certbot` container runs automatically and checks for renewal every 12 hours. Let's Encrypt certificates are valid for 90 days; certbot renews them when they're within 30 days of expiry. No manual action needed.

### Architecture

```
Internet
   │
   ▼
nginx (ports 80/443)          ← SSL termination, rate limiting
   │
   ├── /uploads/*             ← served directly from disk
   ├── /docs/*                ← developer documentation
   │
   └── everything else ──────→ frontend container (port 80)
                                      │
                                      ├── /api/* ──→ backend (port 8080)
                                      └── /* ──────→ React SPA
```
