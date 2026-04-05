# State Management

**File:** `frontend/src/store/auth.ts`

Uses [Zustand](https://zustand.docs.pmnd.rs/) for client-side auth state. Server state (feed, profiles, notifications, etc.) is managed by React Query.

## Auth Store

```typescript
interface User {
  id: string
  username: string
  email: string
  display_name: string
  pronouns: string
  bio: string
  avatar_url: string
  cover_url: string
  cover_position: string
  location: string
  website: string
  role: 'user' | 'moderator' | 'admin'
  profile_private: boolean
  wall_approval_required: boolean
}

interface AuthState {
  user: User | null
  token: string | null
  isAuthenticated: boolean

  setAuth: (user: User, token: string) => void
  updateUser: (updates: Partial<User>) => void
  logout: () => void
}
```

### `setAuth(user, token)`
Saves user and token to both Zustand state and `localStorage`. Called after successful login or register.

### `updateUser(updates)`
Merges `updates` into the current user. Persists to `localStorage`. Called after `PATCH /api/users/me`.

### `logout()`
Clears `user` and `token` from Zustand state and removes them from `localStorage`.

### Persistence

On store initialization, user and token are loaded from `localStorage`:

```typescript
// Hydration on load
const stored = localStorage.getItem('user')
const token = localStorage.getItem('token')
if (stored && token) {
  state.user = JSON.parse(stored)
  state.token = token
  state.isAuthenticated = true
}
```

## Usage

```typescript
import { useAuthStore } from '../store/auth'

// In a component:
const { user, isAuthenticated, logout } = useAuthStore()

// After login:
useAuthStore.getState().setAuth(user, token)

// After profile update:
useAuthStore.getState().updateUser({ display_name: 'New Name' })
```

## WebSocket Hook

**File:** `frontend/src/hooks/useWebSocket.ts`

Manages a WebSocket connection to `/api/ws` for real-time DM updates.

```typescript
function useWebSocket(onMessage: (msg: WSMessage) => void): void
```

Features:
- Auto-connects when user is authenticated
- Reconnects automatically with exponential backoff on disconnect
- Sends periodic ping frames to keep connection alive
- Cleans up on unmount
- Passes JWT via `?token=` query parameter
