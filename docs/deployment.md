# Deployment

## Docker Compose (standard)

```bash
cp .env.example .env
# Set JWT_SECRET, INSTANCE_DOMAIN, and optionally SMTP_* values
docker compose up -d --build
```

Services started:
- `postgres:16` on internal port 5432
- `redis:7` on internal port 6379
- `agora-backend` on internal port 8080
- `agora-frontend` (React + nginx) on internal port 80
- `nginx` reverse proxy on **port 80** (public)

## SSL / HTTPS

```bash
./setup-ssl.sh yourdomain.com your@email.com
docker compose -f docker-compose.ssl.yml up -d
```

The script:
1. Configures nginx with your domain
2. Starts nginx on port 80 to serve the ACME challenge
3. Runs Certbot to obtain a Let's Encrypt certificate
4. Generates `docker-compose.ssl.yml`

The `certbot` container checks for renewal every 12 hours. Certificates auto-renew when within 30 days of expiry.

### SSL Architecture

```
Internet
   │
   ▼
nginx (80 → 443 redirect, 443 TLS termination)
   │
   ├── /uploads/*   →  disk
   ├── /docs/*      →  docs/ directory
   └── /*           →  frontend container
                           ├── /api/*  →  backend :8080
                           └── /*      →  React SPA
```

## Production Checklist

- [ ] `JWT_SECRET` set to a long random string
- [ ] `INSTANCE_DOMAIN` set to your public HTTPS URL
- [ ] Admin password changed from default `admin`
- [ ] `ENVIRONMENT=production`
- [ ] SMTP configured if you want email verification/notifications
- [ ] `ALLOWED_ORIGINS` set to your domain
- [ ] Upload directory backed up (contains all user-uploaded files)
- [ ] PostgreSQL data directory backed up

## Data Persistence

All persistent data lives in Docker volumes:
- `postgres_data` — database
- `./data/uploads` — user-uploaded files (bind-mounted)

Back up both before any major upgrade.

## Scaling

The backend is stateless except for:
- WebSocket connections (DMs) — sticky sessions required if running multiple instances
- Upload files — use shared storage (NFS, S3 proxy, etc.) if running multiple instances

## Updating

```bash
git pull
docker compose down
docker compose up -d --build
```

Migrations run automatically on startup via `db.Migrate()`.
