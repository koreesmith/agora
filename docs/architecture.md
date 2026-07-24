# System Architecture

## Repository Layout

```
agora/
├── cmd/server/main.go          # Entry point — wires all services, starts HTTP server
├── internal/                   # All backend packages
│   ├── config/                  # Environment-variable configuration
│   ├── store/                   # PostgreSQL connection + schema migrations
│   ├── auth/                    # JWT auth, register, login, email verification
│   ├── users/                   # User profiles, GDPR export/deletion
│   ├── friends/                 # Mutual friend requests + friend groups
│   ├── feed/                    # Posts, comments, likes, reactions, reposts, polls
│   ├── notifications/           # In-app + SMTP email notifications
│   ├── search/                  # Local user & post search (+ hashtags)
│   ├── moderation/              # Reports, suspension, banning, instance bans, DID blocks
│   ├── admin/                   # Instance settings, user management, invites, audit log
│   ├── federation/              # ActivityPub (fediverse) + legacy Ed25519 protocol + relays
│   ├── atproto/                 # AT Protocol / Bluesky — Agora as its own PDS
│   ├── media/                   # File upload processing and serving
│   ├── groups/                  # Community groups
│   ├── albums/                  # Photo albums
│   ├── dm/                      # Direct messages + WebSocket hub
│   ├── blocks/                  # User blocking
│   └── ctxkeys/                 # Shared context key constants
├── frontend/
│   ├── src/
│   │   ├── api/index.ts        # Typed Axios API client (all endpoints)
│   │   ├── store/auth.ts       # Zustand auth store
│   │   ├── components/         # React components
│   │   ├── pages/              # Route-level pages
│   │   ├── hooks/              # useWebSocket and other hooks
│   │   └── utils/              # Helpers (reactions, mention parsing, etc.)
│   ├── package.json
│   ├── vite.config.ts
│   └── tailwind.config.js
├── docs/                       # Developer documentation (served at /docs)
├── nginx/                      # Reverse proxy config
├── Dockerfile                  # Go binary multi-stage builder
├── docker-compose.yml          # Local dev stack
├── docker-compose.ssl.yml      # Production SSL stack
├── Makefile
└── .env.example
```

## Request Lifecycle

```
Client (Browser / Mobile)
        │
        ▼
   nginx (port 80/443)
        │
        ├── /uploads/*  ──────────────────────────→  disk (./data/uploads)
        ├── /docs/*  ─────────────────────────────→  docs/ directory (static HTML/MD)
        │
        └── everything else ──→  frontend container (React SPA, port 3000)
                                         │
                                         ├── /api/*  ──→  Go backend (:8080)
                                         └── /*  ──────→  React SPA (index.html)
```

## Service Dependency Graph

```
main.go
  ├── config.Load()
  ├── store.Open()  ──────────────────────────────→  PostgreSQL
  │
  ├── notifications.NewEmailService(db, cfg)
  ├── notifications.NewService(db, emailSvc)        ← notifSvc
  ├── media.NewService(cfg.UploadDir)               ← mediaSvc
  ├── users.NewService(db, mediaSvc)                ← userSvc
  ├── auth.NewService(db, cfg, notifSvc)            ← authSvc
  ├── friends.NewService(db, notifSvc)              ← friendSvc
  ├── feed.NewService(db, notifSvc, mediaSvc)       ← feedSvc
  ├── groups.NewService(db, notifSvc)               ← groupSvc
  ├── albums.NewService(db, mediaSvc)               ← albumsSvc
  ├── feedSvc.SetAlbums(albumsSvc)
  ├── search.NewService(db)                         ← searchSvc
  ├── moderation.NewService(db, notifSvc)           ← modSvc
  ├── admin.NewService(db, cfg, notifSvc)           ← adminSvc
  ├── federation.NewService(db, cfg)                ← fedSvc      (feed/users wire in via SetFed, not constructor args)
  ├── atproto.NewService(db, cfg, notifSvc)          ← atprotoSvc
  ├── dm.New(db)                                    ← dmSvc
  ├── blocks.New(db)                                ← blocksSvc
  │
  ├── friendSvc.SetFed(fedSvc)        ← broadcast friend events (ActivityPub)
  ├── feedSvc.SetFed(fedSvc)          ← broadcast post events (ActivityPub)
  ├── feedSvc.SetAtproto(atprotoSvc)  ← broadcast post events (AT Proto)
  ├── userSvc.SetFed(fedSvc)          ← broadcast profile updates (ActivityPub)
  └── userSvc.SetAtproto(atprotoSvc)  ← broadcast profile updates (AT Proto)
```

`feed.Service`/`users.Service` don't import `federation`/`atproto` directly — each declares a small structural interface (`fedSender`/`atprotoSender` etc.) satisfied by the real service, avoiding an import cycle. See [Federation Service](backend/federation.md) and [AT Protocol Service](backend/atproto.md).

## HTTP Router Structure

The chi router is configured in `cmd/server/main.go`:

```
/health                             → liveness probe
/uploads/*                          → static file server (media)
/docs/*                             → static file server (documentation)

/.well-known/agora-instance          → legacy federation instance info
/.well-known/webfinger, /host-meta, /nodeinfo, /nodeinfo/2.0  → ActivityPub discovery
/.well-known/did.json, /atproto-did  → AT Proto identity (per-user, by Host header)
/federation/inbox                    → receive signed activities (both ActivityPub and legacy)
/federation/users/{handle}, /pages/{slug}, /instance   → actor documents (users, pages, instance actor)
/federation/search                   → cross-instance search (legacy protocol)
/federation/lookup, /ap-lookup        → resolve user@instance handle (legacy / standard ActivityPub)
/xrpc/com.atproto.*                  → AT Proto sync/repo endpoints (public — relays, AppViews, clients)

/api/
  ├── (public)
  │   ├── /setup                    → first-run setup
  │   ├── /auth/register
  │   ├── /auth/login
  │   ├── /auth/verify-email
  │   ├── /auth/forgot-password
  │   ├── /auth/reset-password
  │   ├── /auth/verify-email-change
  │   ├── /notifications/unsubscribe
  │   └── /instance                 → public instance info
  │
  ├── (authenticated — requires JWT)
  │   ├── /auth/me, /auth/change-password, /auth/request-email-change
  │   ├── /users/*
  │   ├── /friends/*, /friend-groups/*
  │   ├── /feed, /posts/*
  │   ├── /groups/*
  │   ├── /notifications/*
  │   ├── /search/*
  │   ├── /reports
  │   ├── /media/upload
  │   ├── /albums/*
  │   ├── /conversations/*, /messages/*, /ws
  │   ├── /blocks/*
  │   ├── /federation/ap-lookup, /follow, /following, /follow/{id}/notify   → fediverse follows (Agora's own frontend only)
  │   └── /atproto/*                → Bluesky follows/search/lookup (Agora's own frontend only)
  │
  ├── (moderator or admin — requires role=admin|moderator)
  │   └── /moderation/*             → reports, suspensions, bans, instance bans, blocked DIDs
  │
  └── (admin-only — requires role=admin)
      ├── /admin/*                  → settings, users, invites, audit log, legacy federated_instances
      └── /admin/relays/*           → fediverse relay subscriptions (registered by internal/federation)
```

See [Federation API](api/federation.md), [AT Protocol API](api/atproto.md), and [Moderation API](api/moderation.md) for full request/response shapes.

## Authentication Flow

```
1. POST /api/auth/register  →  validate → hash password (bcrypt) → insert user → return JWT
2. POST /api/auth/login     →  verify password → return JWT + user data
3. All subsequent requests  →  Authorization: Bearer <token>
4. authSvc.Middleware       →  validate JWT → add userID/role to request context
5. ctxkeys.UserID           →  downstream handlers read from context
```

## Federation Flow

The legacy Agora-to-Agora protocol, unchanged since before ActivityPub support — see [Federation Service → Legacy protocol](backend/federation.md#legacy-agora-to-agora-protocol) for how a shared `POST /federation/inbox` routes between this and standard ActivityPub:

```
Outbound:
  Service (feed/friends/users)
      └── fedSvc.BroadcastToFriendInstances(userID, activity)
              └── signs activity with Ed25519 private key
              └── POST to remote /federation/inbox
              └── on failure: queues in federation_queue for retry (up to 10 attempts)

Inbound:
  POST /federation/inbox
      └── fedSvc.verifyActivity()  →  fetch remote public key from /.well-known/agora-instance
      └── validate Ed25519 signature
      └── route by activity.Type: post | delete_post | friend_request | friend_accept | profile_update
```

Standard ActivityPub (Mastodon and the rest of the fediverse) uses per-actor RSA keys and HTTP Signatures instead, and AT Proto (Bluesky) uses a completely different model — a per-user signed repo, not activity delivery at all. See [Federation Service](backend/federation.md) and [AT Protocol Service](backend/atproto.md) for both.

## AT Proto Flow

```
Outbound (a user's own repo):
  Service (feed/users)
      └── atprotoSvc.BroadcastPost / DeliverLike / SyncProfile / ...
              └── lockRepo(userID) — per-user commit mutex
              └── writes an app.bsky.* record, advances the repo head
              └── appends a firehose #commit event — same DB transaction as the head update
              └── broadcasts to any live subscribeRepos subscriber (e.g. bsky.network)

Inbound (a followed Bluesky account's content):
  StartBlueskyIngestion poller (every 5 min per followed DID)
      └── app.bsky.feed.getAuthorFeed / getPostThread / getLikes / getRepostedBy
              └── isBlueskyActorBlocked() check
              └── ingest into posts / reactions, same tables ActivityPub ingestion uses
```

Agora never subscribes to Bluesky's own network-wide firehose — see [AT Protocol Service → Firehose](backend/atproto.md#firehose) for why.

## Real-Time Direct Messages

```
Client  ──WebSocket──→  /api/ws  →  dm.Hub
                                      ├── register(conn)
                                      ├── unregister(conn)
                                      └── broadcast(conversationID, message)
                                              └── send to all participants' connections
```

## Background Jobs

| Job | Service | Interval |
|-----|---------|----------|
| Federation queue retry (legacy + ActivityPub + pages) | `fedSvc.StartBackgroundSync()` | continuous |
| Account deletion cleanup | `userSvc.StartDeletionCleanup()` | periodic |
| Interaction pruning | `interactionsSvc.StartPruner()` | periodic |
| AT Proto relay crawl registration + reconfirmation | `atprotoSvc.StartRelayCrawl()` | on startup, backoff on failure (cap 24h), reconfirm every 6h |
| AT Proto followed-account ingestion (posts/replies/likes/reposts) | `atprotoSvc.StartBlueskyIngestion()` | every 5 min per followed DID |
