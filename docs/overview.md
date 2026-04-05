# Agora Developer Documentation

Agora is an open-source, federated, privacy-first social network. It uses Facebook-style mutual friendships (not follows), friend groups for fine-grained post visibility, and opt-in cross-instance federation with Ed25519 signatures.

## Key Features

- **Mutual friendships** — both users must accept before either sees the other's private content
- **Friend groups** — organize friends into named lists; target posts to specific groups
- **Post visibility** — public, friends-only, specific friend group, or private
- **Reactions** — like, love, laugh, wow, angry, care, pride, thankful, vomit
- **Single-click like / hold-to-pick reactions** — tap to like, hold to choose a reaction
- **Community groups** — public or private groups with owner/mod/member roles
- **Photo albums** — with per-album visibility controls
- **Direct messages** — real-time via WebSocket
- **Wall posts** — post on another user's wall, with optional approval workflow
- **Polls** — multi-choice with optional expiry
- **Federation** — Ed25519-signed ActivityPub-style protocol between Agora instances
- **Moderation** — reports, suspension, banning, instance bans
- **GDPR** — data export (ZIP) and account deletion (30-day grace or immediate)

## Documentation Index

| Section | Description |
|---------|-------------|
| [Quick Start](getting-started) | Run Agora locally in minutes |
| [Deployment](deployment) | Docker, SSL, environment variables |
| [System Architecture](architecture) | How all pieces fit together |
| [Database Schema](database) | All tables and their columns |
| **Backend Services** | |
| [Auth](backend/auth) | JWT, registration, login, email verification |
| [Users](backend/users) | Profiles, GDPR export, deletion |
| [Friends](backend/friends) | Mutual friend requests, friend groups |
| [Feed](backend/feed) | Posts, comments, likes, reactions, reposts, polls |
| [Notifications](backend/notifications) | In-app and email notifications |
| [Groups](backend/groups) | Community groups |
| [Albums](backend/albums) | Photo albums |
| [Direct Messages](backend/dm) | Real-time DMs via WebSocket |
| [Blocks](backend/blocks) | User blocking |
| [Moderation](backend/moderation) | Reports, suspension, banning |
| [Admin](backend/admin) | Instance settings, user management |
| [Federation](backend/federation) | Cross-instance protocol |
| [Search](backend/search) | User and post search |
| [Media](backend/media) | File uploads and serving |
| **API Reference** | |
| [Auth API](api/auth) | `/api/auth/*` endpoints |
| [Users API](api/users) | `/api/users/*` endpoints |
| [Feed API](api/feed) | `/api/feed`, `/api/posts/*` endpoints |
| [Friends API](api/friends) | `/api/friends/*` endpoints |
| [Notifications API](api/notifications) | `/api/notifications/*` endpoints |
| [Groups API](api/groups) | `/api/groups/*` endpoints |
| [Albums API](api/albums) | `/api/albums/*` endpoints |
| [DM API](api/dm) | `/api/conversations/*`, `/api/ws` |
| [Moderation API](api/moderation) | `/api/reports/*`, `/api/moderation/*` |
| [Admin API](api/admin) | `/api/admin/*` endpoints |
| [Federation API](api/federation) | `/.well-known/agora-instance`, `/federation/*` |
| [Search API](api/search) | `/api/search/*` endpoints |
| [Media API](api/media) | `/api/media/upload` |
| **Frontend** | |
| [Frontend Overview](frontend/overview) | React app structure and tech stack |
| [API Client](frontend/api-client) | Typed Axios client — all methods |
| [State Management](frontend/state) | Zustand auth store |

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.22, chi v5 router |
| Database | PostgreSQL 16 (pg_trgm, uuid-ossp) |
| Cache | Redis 7 |
| Frontend | React 18, TypeScript, Tailwind CSS, Vite |
| Auth | JWT HS256 + bcrypt |
| Real-time | Gorilla WebSocket |
| Email | SMTP via gomail |
| File storage | Local disk (`./data/uploads`) |
| Reverse proxy | nginx |
| Federation | Ed25519 signed REST activities |
