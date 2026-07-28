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
                           ├── /api/*     →  backend :8080
                           ├── /xrpc/*    →  backend :8080 (AT Proto firehose, websocket)
                           └── /*         →  React SPA
```

### AT Proto (Bluesky) wildcard subdomains

Every local user's AT Proto identity is served on its own subdomain
(`username.yourdomain.com`), so multi-tenant AT Proto support needs a
**wildcard** DNS record and — depending on your setup — a wildcard TLS
certificate. `setup-ssl.sh`'s certbot invocation only covers your bare
domain (HTTP-01 challenge, which cannot issue wildcards at all), so this is
an extra step beyond the base SSL setup above.

1. **DNS**: add a `*.yourdomain.com` record pointing at the same target as
   your existing domain record.
2. **TLS**, which path you need depends on what's in front of your origin
   server:
   - **Behind Cloudflare (or a similar proxy), "Full" mode (not strict)**:
     nothing else to do. Cloudflare's free Universal SSL automatically
     covers the first-level wildcard once the DNS record above is proxied,
     and in "Full" mode Cloudflare doesn't validate that your origin
     certificate's hostname matches — your existing single-domain
     certificate from `setup-ssl.sh` keeps working as-is.
   - **Behind Cloudflare, "Full (strict)" mode**: your origin certificate's
     hostname *is* validated, so it needs to cover the wildcard too. Either
     generate a free Cloudflare Origin CA certificate for
     `yourdomain.com` + `*.yourdomain.com` (SSL/TLS → Origin Server →
     Create Certificate in the Cloudflare dashboard — not the "Edge
     Certificates" or "Client Certificates" tabs, which are for different
     purposes), or use the DNS-01 option below.
   - **No CDN/proxy in front (serving browsers directly)**: you need a real
     wildcard Let's Encrypt certificate, which requires the DNS-01
     challenge type instead of `setup-ssl.sh`'s HTTP-01 — e.g. `certbot
     certonly --dns-<your-provider-plugin> -d yourdomain.com -d
     *.yourdomain.com`, using whichever certbot DNS plugin matches your DNS
     host (Cloudflare, Route53, DigitalOcean, etc. — see [certbot's DNS
     plugin list](https://eff-certbot.readthedocs.io/en/stable/using.html#dns-plugins)).
     This isn't scripted in `setup-ssl.sh` since the right plugin/credentials
     are specific to your DNS provider, and not every self-hoster uses the
     same one. Requesting both names in one invocation lands the cert at the
     same `data/certbot/conf/live/yourdomain.com/` path the nginx templates
     already point at — no other config changes needed once it's issued.

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
