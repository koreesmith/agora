# System Architecture

## Repository Layout

```
agora/
├── cmd/server/main.go          # Entry point — wires all services, starts HTTP server
├── internal/                   # All backend packages
│   ├── config/                 # Environment-variable configuration
│   ├── store/                  # PostgreSQL connection + schema migrations
│   ├── auth/                   # JWT auth, register, login, email verification
│   ├── users/                  # User profiles, GDPR export/deletion
│   ├── friends/                # Mutual friend requests + friend groups
│   ├── feed/                   # Posts, comments, likes, reactions, reposts, polls
│   ├── notifications/          # In-app + SMTP email notifications
│   ├── search/                 # Local user & post search
│   ├── moderation/             # Reports, suspension, banning
│   ├── admin/                  # Instance settings, user management, invites, audit log
│   ├── federation/             # Ed25519 cross-instance protocol
│   ├── media/                  # File upload processing and serving
│   ├── groups/                 # Community groups
│   ├── albums/                 # Photo albums
│   ├── dm/                     # Direct messages + WebSocket hub
│   ├── blocks/                 # User blocking
│   └── ctxkeys/                # Shared context key constants
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
  ├── federation.NewService(db, cfg, feedSvc, userSvc) ← fedSvc
  ├── dm.New(db)                                    ← dmSvc
  ├── blocks.New(db)                                ← blocksSvc
  │
  ├── friendSvc.SetFed(fedSvc)   ← broadcast friend events
  ├── feedSvc.SetFed(fedSvc)     ← broadcast post events
  └── userSvc.SetFed(fedSvc)     ← broadcast profile updates
```

## HTTP Router Structure

The chi router is configured in `cmd/server/main.go`:

```
/health                             → liveness probe
/uploads/*                          → static file server (media)
/docs/*                             → static file server (documentation)

/.well-known/agora-instance         → federation instance info
/federation/inbox                   → receive signed activities
/federation/users/{handle}          → federated user lookup
/federation/search                  → cross-instance search
/federation/lookup                  → resolve user@instance handle

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
  │   ├── /reports, /moderation/*
  │   ├── /media/upload
  │   ├── /albums/*
  │   ├── /conversations/*, /messages/*, /ws
  │   └── /blocks/*
  │
  └── (admin-only — requires role=admin|moderator)
      └── /admin/*
```

## Authentication Flow

```
1. POST /api/auth/register  →  validate → hash password (bcrypt) → insert user → return JWT
2. POST /api/auth/login     →  verify password → return JWT + user data
3. All subsequent requests  →  Authorization: Bearer <token>
4. authSvc.Middleware       →  validate JWT → add userID/role to request context
5. ctxkeys.UserID           →  downstream handlers read from context
```

## Federation Flow

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
| Federation queue retry | `fedSvc.StartBackgroundSync()` | continuous |
| Account deletion cleanup | `userSvc.StartDeletionCleanup()` | periodic |
