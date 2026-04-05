# Auth Service

**Package:** `internal/auth`
**File:** `internal/auth/auth.go`

Handles user registration, login, JWT issuance, email verification, password reset, and HTTP middleware for authenticated requests.

## Constructor

```go
func NewService(db *store.DB, cfg *config.Config, notif *notifications.Service) *Service
```

## Middleware

### `Middleware(next http.Handler) http.Handler`

Validates the Bearer JWT in the `Authorization` header (or `?token=` query param). On success, sets `ctxkeys.UserID` and `ctxkeys.UserRole` in the request context. Returns `401` if missing or invalid.

### `RequireAdmin(next http.Handler) http.Handler`

Must be chained after `Middleware`. Returns `403` unless the user's role is `admin` or `moderator`.

## Handlers

### `SetupStatus(w, r)`
`GET /api/setup`
Returns `{"needs_setup": true}` if no admin user exists yet.

### `RunSetup(w, r)`
`POST /api/setup`
Creates the first admin account. Only works when `needs_setup` is true.

**Body:**
```json
{ "username": "admin", "password": "secret" }
```

### `Register(w, r)`
`POST /api/auth/register`
Creates a new user. Enforces registration mode:
- `open` — anyone can register
- `invite` — requires a valid `invite_code`
- `closed` — registration disabled

**Body:**
```json
{
  "username": "string",
  "email": "string",
  "password": "string",
  "invite_code": "string (optional)"
}
```

**Returns:** `{"token": "...", "user": {...}}`

### `Login(w, r)`
`POST /api/auth/login`
Verifies credentials, returns JWT + user object. Returns `401` for wrong password, `403` for suspended/banned accounts.

**Body:**
```json
{ "email": "string", "password": "string" }
```

### `Me(w, r)`
`GET /api/auth/me`
Returns the authenticated user's full profile.

### `ChangePassword(w, r)`
`POST /api/auth/change-password`
Requires `current_password` and `new_password`.

### `VerifyEmail(w, r)`
`GET /api/auth/verify-email?token=...`
Marks email as verified. Token sent during registration.

### `ForgotPassword(w, r)`
`POST /api/auth/forgot-password`
Sends a password reset email if SMTP is enabled. Body: `{"email": "..."}`.

### `ResetPassword(w, r)`
`POST /api/auth/reset-password`
Body: `{"token": "...", "password": "..."}`. Token expires after 1 hour.

### `RequestEmailChange(w, r)`
`POST /api/auth/request-email-change`
Sends a verification email to the new address. Body: `{"email": "..."}`.

### `VerifyEmailChange(w, r)`
`GET /api/auth/verify-email-change?token=...`
Confirms the email change after user clicks the link.

### `SendUserInvite(w, r)`
`POST /api/invites/send`
Sends an invite email to a given address. Body: `{"email": "..."}`.

### `WaitlistAccept(w, r)`
`GET /api/auth/waitlist/accept?token=...`
Accepts a waitlisted user and sends an invite link.

## JWT Details

- Algorithm: HS256
- Signed with `config.JWTSecret`
- Claims: `sub` (userID), standard expiry
- No refresh tokens — clients re-login on expiry
