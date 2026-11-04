# Deployment

## Docker Compose (standard)

```bash
cp .env.example .env
# Set JWT_SECRET, INSTANCE_DOMAIN, and optionally SMTP_* values
docker compose up -d --build
```

Services started:
- `postgres:16` on internal port 5432 (also published on the host per `POSTGRES_PORT`, loopback-only by default)
- `redis:7` on internal port 6379
- `agora-backend` on internal port 8080
- `agora-frontend` (React + nginx) on internal port 80
- `nginx` reverse proxy on **port 80 / 443** (public)

### Host ports

The public and database host ports are set in `.env`:

| Variable | Default | Purpose |
| --- | --- | --- |
| `HTTP_PORT` | `80` | Host port for nginx plain HTTP |
| `HTTPS_PORT` | `443` | Host port for nginx HTTPS |
| `POSTGRES_PORT` | `127.0.0.1:5432` | Host binding for Postgres; use a bare port to expose on all interfaces |

Only change `HTTP_PORT` / `HTTPS_PORT` when running behind an external load
balancer / TLS-terminating proxy or alongside another web server. Two
caveats: `setup-ssl.sh` needs the host reachable on port 80 for the
Let's Encrypt HTTP-01 challenge, and a non-standard port becomes part of
every federated URL, so `INSTANCE_DOMAIN` must include it (e.g.
`https://yourdomain.com:8443`).

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
- [ ] `POSTGRES_PASSWORD` changed from the default `agora`
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

## Upgrading to v5.0.0

v5.0.0 adds Reels: a full-screen feed of short videos and a way to make them.
Back up `./data/postgres` and `./data/uploads` first, then update as above.

### What it costs you

Video is the most expensive thing an instance hosts, and Reels will make
people post more of it.

- **Disk.** A transcoded 2-minute video is roughly 20 MB (see
  [ADR-001](adr/001-video-storage-encoding-delivery.md)). A thousand videos is
  about 20 GB, ten thousand about 200 GB. Videos are kept after their post is
  deleted by its author, and are only removed from disk when a moderator or
  admin takes a post down.
- **Bandwidth.** A reel loops and is watched repeatedly, and the next one is
  loaded ahead of time, so a video is served more often than a photo is. nginx
  serves them straight from `./data/uploads` with range requests. If egress
  becomes a problem, a CDN or separate media origin can sit in front of
  `/uploads` without code changes.
- **CPU while uploading.** Each video is transcoded with `ffmpeg` (H.264, up to
  720p). At most two transcodes run at once. The runtime image already includes
  `ffmpeg`; if you run Agora outside Docker, it needs `ffmpeg` and `ffprobe` with
  the `libx264` and AAC encoders. Distribution builds that leave out `libx264`
  (for example Fedora and RHEL's `ffmpeg-free`) will transcode nothing.

### New settings

All in Admin, under **Settings**, **Reels and video**. They take effect
immediately, with no restart.

| Setting | Default | What it does |
|---|---|---|
| Reels Discover tab (`reels_discover_enabled`) | on | Offers the Discover tab, which shows public videos from this instance and its peers. Off leaves only the Following tab. |
| Longest video (`video_max_seconds`) | 120 | The longest video this instance accepts, 5 to 120 seconds. People can trim a longer video down to fit, so it limits what is kept, not what is chosen. |
| Video storage per account (`video_quota_mb`) | 0 (no limit) | Total video storage each account may use. An account at its limit must delete a video before uploading another. |

Admin, under **Storage**, now shows how much disk uploads use, with video
apart from images, and the accounts using the most video storage.

### On the first start

- **Two background passes** run once at startup over existing videos: one
  records each video's size and length, the other how much disk each takes (for
  storage limits). They run in the background, so startup isn't held up, and
  they are safe to repeat. On an instance with many thousands of videos they
  use some CPU for a short while.
- **A new index** on `posts(repost_of_id)` is built. Every feed counted a
  post's reposts by scanning the whole posts table, so the home feed, Explore
  and profiles get faster as well. On a very large posts table the build takes
  a moment during migration.

### Federation and moderation

- Reels are ordinary video posts, so they federate to the Fediverse and Bluesky
  exactly like existing video posts. Videos now carry their width, height and
  duration, and Bluesky videos carry their aspect ratio.
- A reported video can be played from the report in Admin.
- When a moderator or admin deletes **someone else's** video post, the video and
  its thumbnail are removed from disk and the takedown is written to the audit
  log. A person deleting their own post leaves the files in place.
- Discover shows the same pool as Explore: this instance and its peered Agora
  instances. Videos from the wider Fediverse and Bluesky only reach people who
  follow those accounts, in their Following tab.

### Mobile app

The v5.0.0 app needs a **new build**, not an over-the-air update: recording
video needs a microphone permission, which is native configuration. An older app
still works against a v5.0.0 server, and a v5.0.0 app against an older server
hides Reels.

### Version

The instance now reports version `5.0.0` in NodeInfo.

## If the instance freezes (v5.0.0)

If every page shows only "Loading..." (or "Can't reach the server") and `docker compose restart` fixes it, the server is up but stuck waiting for the database. v5.0.0 makes that visible and can recover from it on its own.

**What to look at**

* `docker compose ps`: the backend now shows `(unhealthy)` when it cannot reach the database, instead of staying `(healthy)` because its port is open.
* `curl -s http://localhost:8080/health` from inside the backend container (or `docker compose exec backend wget -qO- http://127.0.0.1:8080/health`): returns JSON with `ok`, `ping_ms` and the connection pool (`open`, `in_use`, `idle`, `max_open`, `wait_count`, `wait_seconds`). It answers 503 if the database does not reply within 3 seconds. A pool with `in_use` equal to `max_open` (25) and a growing `wait_count` means every connection is held and requests are queuing.
* `docker compose logs backend | grep "db pool"`: a line is logged (at most once a minute) when 20 or more of the 25 connections are in use or requests had to wait for one, so the start of a freeze is in the log.

**Before you restart, capture what it is stuck on**

1. What the database sees (the sessions holding connections and what they are waiting for):
   `docker compose exec postgres psql -U agora -c "select pid, state, wait_event_type, wait_event, now()-query_start as running, left(query,120) as query from pg_stat_activity where datname='agora' order by running desc limit 30;"`
2. Whether the host is starved: `docker stats --no-stream` (a video transcode using all the CPU can stall everything).
3. What every part of the server is doing: `docker compose kill -s SIGQUIT backend`. The server writes a dump of all its goroutines to its log and exits, and Docker restarts it. Then `docker compose logs backend > freeze.log` has the stack that was blocked, which says what held the connections.

Send those three with the time it froze when reporting it.

**Self-healing.** Set `HEALTH_WATCHDOG=4` in `.env` (off by default). The server then exits after 4 database checks in a row fail (one every 15 seconds, so about a minute of silence) and Docker's `restart: unless-stopped` starts it again, with a line in the log saying why. It does not fix the cause, it just removes the need to restart by hand.

## Upgrading to v4.0.0

Back up `./data/postgres` and `./data/uploads` first. Then read the points
below before running the update above.

### Before you pull

- **Database credentials moved to `.env`.** `docker-compose.yml` now reads
  `POSTGRES_DB`, `POSTGRES_USER` and `POSTGRES_PASSWORD` from `.env` and
  builds the backend's `DATABASE_URL` from them. If you changed the
  credentials by editing `docker-compose.yml`, copy them into `.env` first,
  or `git pull` will conflict on that file. A `DATABASE_URL` line left in
  `.env` is ignored. Leaving all three unset keeps the old `agora` values.
- **Postgres is published on the host.** The database now binds
  `127.0.0.1:5432` (`POSTGRES_PORT`). If something else on the host already
  uses 5432, set `POSTGRES_PORT` to a free port (for example
  `127.0.0.1:5433`) or the stack will not start.
- **Host ports are configurable.** `HTTP_PORT` and `HTTPS_PORT` default to
  80 and 443, so a standard install needs no change. See Host ports above.

### Federation with other Agora instances

Agora-to-Agora federation now runs entirely over ActivityPub, and the older
Agora-only protocol has been removed (see
[ADR-002](adr/002-agora-to-agora-federation-protocol.md)). Friend requests,
friends-only and friend-list posts, and messages between instances all need
both sides on v4.0.0.

- **Upgrade peers together.** A v3 instance can still follow and be followed
  over ActivityPub, but friend requests between it and a v4 instance will
  not go through.
- **Keep ActivityPub on.** Turning off the instance-wide Fediverse
  (ActivityPub) switch in Admin now also stops federation with other Agora
  instances.
- New instance setting `friend_requests_from`: `anyone` (default) or
  `peered_only`, which accepts friend requests only from instances listed in
  the Federation tab. It has no control on the Admin page yet; set it through
  the admin settings API (`PATCH /admin/settings`).

### On by default after upgrading

- **Signup challenge.** New signups solve a short proof-of-work check in the
  browser. Signups that don't (including older mobile app builds) are not
  rejected; they go to the waitlist for an admin to approve. Turn it off with
  the `signup_challenge` setting in Admin.
- **Signup limits.** Registration is rate limited per IP, and throwaway email
  domains are refused.
- **Remote media cache.** Profile pictures and covers from other servers are
  downloaded and served locally, up to 1 GB under `./data/uploads/remote`.
  The size limit, and whether the cache is on at all, are in Admin under
  Storage.
- **Getting started tour.** Every existing account sees the new tour once on
  its next sign-in, followed by What's New. Both can be skipped.

### nginx

The nginx config is built into its image, so the `--build` in the update
command is needed. Changes you may notice:

- The general API rate limit is higher (10 requests a second, burst 100), and
  a limited request now gets `429` instead of `503`.
- nginx trusts `X-Forwarded-For` from private address ranges (`10.0.0.0/8`,
  `172.16.0.0/12`, `192.168.0.0/16`) so rate limits apply per visitor behind
  an internal reverse proxy. If anything on those ranges can reach nginx
  directly and is not a proxy you control, review this before upgrading.
- Uploaded files are served with their real content type, which fixes video
  playback.
- Post and profile pages (`/post/...`, `/profile/...`) now go through the
  backend first, which fills in the link preview tags that messaging apps,
  Bluesky and Mastodon read, then falls back to the frontend if the backend
  is unavailable. The backend fetches the app's `index.html` from
  `http://frontend:80` on the compose network; set `FRONTEND_INTERNAL_URL` in
  `.env` only if your frontend lives somewhere else.

### After the first start

- The first start runs several one-time data repairs and can take longer than
  usual. Migrations take a database lock, so two backends starting at once
  wait for each other instead of colliding.
- The instance now reports version `4.0.0` in NodeInfo, and reports
  registrations as open only when the registration mode is Open.
