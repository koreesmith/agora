# Frontend Overview

**Location:** `frontend/`

React 18 SPA built with TypeScript, Tailwind CSS, and Vite. Communicates with the Go backend entirely through the REST API.

## Tech Stack

| Package | Version | Purpose |
|---------|---------|---------|
| `react` | 18.3.1 | UI framework |
| `react-dom` | 18.3.1 | DOM rendering |
| `react-router-dom` | 6.23.0 | Client-side routing |
| `typescript` | 5.4.5 | Type safety |
| `axios` | 1.6.8 | HTTP client |
| `@tanstack/react-query` | 5.32.0 | Server state caching |
| `zustand` | 4.5.2 | Client state (auth) |
| `tailwindcss` | 3.4.3 | Utility CSS |
| `vite` | 5.2.10 | Build tool |
| `lucide-react` | 0.378.0 | Icon set |
| `date-fns` | 3.6.0 | Date formatting |

## Directory Structure

```
frontend/src/
├── main.tsx                      # Entry point — renders App into #root
├── App.tsx                       # Root with React Router routes
│
├── api/
│   └── index.ts                  # All API calls (see API Client docs)
│
├── store/
│   └── auth.ts                   # Zustand auth store (user + token)
│
├── hooks/
│   └── useWebSocket.ts           # WebSocket hook for real-time DMs
│
├── utils/
│   ├── reactions.ts              # Reaction type → emoji/label mapping
│   ├── gif.ts                    # GIF handling utilities
│   ├── handle.ts                 # Parse user@instance handles
│   └── mentions.ts               # @mention parsing helpers
│
├── components/
│   ├── layout/
│   │   └── Layout.tsx            # App shell: sidebar, nav, chat windows
│   ├── feed/
│   │   ├── PostCard.tsx          # Renders a single post
│   │   ├── CreatePost.tsx        # Post composer with visibility controls
│   │   ├── CommentsSection.tsx   # Comments thread
│   │   ├── MentionDropdown.tsx   # @mention autocomplete dropdown
│   │   ├── ReportModal.tsx       # Report content form
│   │   └── useMentions.ts        # Hook: detect and resolve @mentions in text
│   ├── common/
│   │   ├── CoverPhoto.tsx        # Cover image with position editing
│   │   ├── FriendListModal.tsx   # Friend list picker
│   │   └── ChatWindows.tsx       # Floating DM windows
│   └── groups/
│       └── CreateGroupModal.tsx  # Group creation form
│
└── pages/
    ├── LoginPage.tsx
    ├── RegisterPage.tsx
    ├── VerifyEmailPage.tsx
    ├── ForgotPasswordPage.tsx
    ├── ResetPasswordPage.tsx
    ├── ChangePasswordPage.tsx
    ├── SetupPage.tsx             # First-run admin setup
    ├── FeedPage.tsx              # Main home feed
    ├── ProfilePage.tsx           # User profile (own or other)
    ├── FriendsPage.tsx           # Friend list and requests
    ├── NotificationsPage.tsx
    ├── GroupPage.tsx             # Community group view
    ├── GroupsListPage.tsx        # Browse groups
    ├── AlbumPage.tsx             # Photo album view
    ├── SettingsPage.tsx          # Account settings
    ├── AdminPage.tsx             # Instance admin panel
    ├── ModerationPage.tsx        # Reports and moderation
    ├── SearchPage.tsx
    ├── PostPage.tsx              # Single post permalink
    └── SupportPage.tsx           # App Store support page
```

## Routing

Routes are defined in `App.tsx` using React Router v6:

- `/` — Feed (requires auth)
- `/login` — Login
- `/register` — Register
- `/setup` — First-run setup
- `/profile/:username` — User profile
- `/friends` — Friends list
- `/notifications` — Notifications
- `/groups` — Group list
- `/groups/:slug` — Group detail
- `/albums/:id` — Album
- `/settings` — Settings
- `/admin` — Admin panel
- `/moderation` — Moderation panel
- `/search` — Search
- `/posts/:id` — Single post
- `/support` — Support page

## Authentication Guard

A wrapper component checks `useAuthStore().isAuthenticated`. Unauthenticated access to protected routes redirects to `/login`.

## Data Fetching Pattern

The app uses **React Query** for server state. Example:

```typescript
const { data, isLoading } = useQuery({
  queryKey: ['feed', page],
  queryFn: () => feedApi.getFeed({ page })
})
```

Mutations use `useMutation` with `queryClient.invalidateQueries` for cache invalidation.
