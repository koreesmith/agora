# Federation Service

**Package:** `internal/federation`
**File:** `internal/federation/federation.go`

Implements Agora's Ed25519-signed cross-instance protocol for distributing posts, friend requests, and profile updates between Agora instances.

## Constructor

```go
func NewService(db *store.DB, cfg *config.Config, feed *feed.Service, users *users.Service) *Service
func (s *Service) StartBackgroundSync(ctx context.Context)  // retry queue processor
```

## Configuration

Federation is **disabled by default**. Enable it in Admin → Settings → `federation_enabled=true`.

When enabled:
- `/.well-known/agora-instance` is publicly accessible
- `/federation/inbox` accepts signed activities from remote instances
- Only `visibility=public` posts are federated

## Public Endpoints

### `InstanceInfo(w, r)`
`GET /.well-known/agora-instance`

Returns public metadata about this instance:
```json
{
  "domain": "agora.example.com",
  "name": "My Agora",
  "description": "string",
  "public_key": "base64-encoded Ed25519 public key",
  "api_version": "1",
  "user_count": 42,
  "software": "agora",
  "rules": ["Be kind", "No spam"]
}
```

### `Inbox(w, r)`
`POST /federation/inbox`

Receives signed activities from remote instances. Validates the Ed25519 signature before processing.

**Activity format:**
```json
{
  "type": "post|delete_post|friend_request|friend_accept|profile_update",
  "actor": "username@remote.instance",
  "instance": "remote.instance",
  "public_key": "base64",
  "timestamp": "ISO8601",
  "data": { ... }
}
```

Signature: `X-Agora-Signature` header, base64-encoded Ed25519 signature over the request body.

### `GetUser(w, r)`
`GET /federation/users/{handle}`

Returns a user's public profile for federation lookup.

### `Search(w, r)`
`GET /federation/search?q=...`

Searches local public users. Used by remote instances to find users to friend.

### `LookupUser(w, r)`
`GET /federation/lookup?handle=user@instance.com`

Resolves a `user@instance` handle — fetches instance info, then queries the remote instance for the user.

## Outbound Broadcasting

### `BroadcastToFriendInstances(userID, activity)`

Called by feed/friends/users services after mutations. Finds all distinct remote instances where `userID` has accepted friends, then delivers the signed activity to each.

### `SendToUserInstance(remoteInstance, activity)`

Direct delivery to a specific instance. On failure, queues in `federation_queue` for retry.

## Signature Verification

`verifyActivity(activity, signature)`:
1. Fetch remote instance public key from their `/.well-known/agora-instance`
2. Cache the public key in `federated_instances` table
3. Verify Ed25519 signature over the raw request body

## Background Sync

`StartBackgroundSync(ctx)` polls `federation_queue` for failed deliveries. Retries with backoff. Abandons after 10 attempts and logs the failure.

## Activity Types

| Type | Payload |
|------|---------|
| `post` | New public post |
| `delete_post` | Delete a remote post |
| `friend_request` | Mutual friend request |
| `friend_accept` | Accept a pending request |
| `profile_update` | User profile changed |
