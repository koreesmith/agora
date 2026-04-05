# Quick Start

## Prerequisites

- **Docker & Docker Compose** (recommended)
- OR: Go 1.22+, PostgreSQL 16, Redis 7, Node.js 20+

## Docker (fastest)

```bash
git clone <repo-url> agora && cd agora
cp .env.example .env
# Edit .env — set JWT_SECRET and INSTANCE_DOMAIN at minimum
docker compose up -d --build
```

Visit **http://localhost** and sign in as **admin / admin**.

> **Important:** Change the admin password immediately. Registration is locked until you do.

## Local Development

### Backend

```bash
# Requires Go 1.22, Postgres, Redis already running
cp .env.example .env   # edit as needed
make tidy              # download Go modules
make run               # starts on :8080
```

### Frontend

```bash
cd frontend
npm install
npm run dev            # starts on :3000, proxies /api → :8080
```

Visit **http://localhost:3000**.

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `INSTANCE_DOMAIN` | Yes | — | Public URL, e.g. `https://agora.example.com` |
| `JWT_SECRET` | Yes | — | Random secret for signing JWTs |
| `HTTP_ADDR` | No | `:8080` | Listen address |
| `INSTANCE_NAME` | No | `Agora` | Display name for your instance |
| `DATABASE_URL` | No | see below | PostgreSQL DSN |
| `REDIS_URL` | No | `redis://localhost:6379` | Redis connection |
| `UPLOAD_DIR` | No | `./data/uploads` | Where uploaded files are stored |
| `ENVIRONMENT` | No | `development` | `development` or `production` |
| `ALLOWED_ORIGINS` | No | `*` | Comma-separated CORS origins |
| `SMTP_HOST` | No | — | SMTP hostname |
| `SMTP_PORT` | No | — | SMTP port (usually 587) |
| `SMTP_USER` | No | — | SMTP username |
| `SMTP_PASSWORD` | No | — | SMTP password |
| `SMTP_FROM` | No | — | From address for outbound email |
| `SMTP_ENABLED` | No | `false` | Enable email sending |

Default `DATABASE_URL`: `postgres://agora:agora@localhost/agora?sslmode=disable`

## First-Run Setup

On first start with no admin user, visiting any page redirects to `/setup`. There you create the initial admin account. Alternatively the `POST /api/setup` endpoint handles this programmatically.

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make run` | Build and run the backend |
| `make build` | Compile binary to `./bin/agora` |
| `make tidy` | Tidy Go modules |
| `make test` | Run tests |
