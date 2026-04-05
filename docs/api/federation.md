# Federation API

These endpoints are used by remote Agora instances and do not require auth (signature verification is used instead).

---

## `GET /.well-known/agora-instance`

Public instance metadata. Used by remote instances to discover public key and instance info.

**Response 200:**
```json
{
  "domain": "agora.example.com",
  "name": "My Agora Instance",
  "description": "string",
  "public_key": "base64-encoded Ed25519 public key",
  "api_version": "1",
  "user_count": 42,
  "software": "agora",
  "rules": ["Be kind", "No spam"]
}
```

---

## `POST /federation/inbox`

Receive a signed activity from a remote instance.

**Headers:**
- `X-Agora-Signature: <base64 Ed25519 signature over request body>`

**Body:**
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

**Response 200** on success. **Response 401** if signature invalid. **Response 403** if instance is banned.

---

## `GET /federation/users/{handle}`

Look up a local user by username for federation.

**Response 200:** Public user profile including `public_key`

---

## `GET /federation/search?q=...`

Search local public users. Used by remote instances to find users.

**Response 200:** `[{ "username", "display_name", "avatar_url", "handle" }]`

---

## `GET /federation/lookup?handle=user@instance.com`

Resolve a remote `user@instance` handle. Fetches the remote instance's info and then the user data.

**Response 200:** User profile from remote instance

---

## Activity Data Payloads

### `post`
```json
{
  "id": "remote-post-id",
  "content": "string",
  "image_url": "string",
  "created_at": "timestamp"
}
```

### `delete_post`
```json
{ "post_id": "remote-post-id" }
```

### `friend_request`
```json
{
  "from_handle": "user@remote.instance",
  "to_username": "local_username"
}
```

### `friend_accept`
```json
{
  "from_handle": "user@remote.instance",
  "to_username": "local_username"
}
```

### `profile_update`
```json
{
  "display_name": "string",
  "bio": "string",
  "avatar_url": "string"
}
```
