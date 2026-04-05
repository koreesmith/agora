# Albums Service

**Package:** `internal/albums`
**File:** `internal/albums/albums.go`

Photo albums with per-album visibility controls.

## Constructor

```go
func NewService(db *store.DB, media *media.Service) *Service
```

## Album Object

```json
{
  "id": "uuid",
  "owner_id": "uuid",
  "owner_username": "string",
  "title": "string",
  "description": "string",
  "cover_url": "string",
  "visibility": "public|friends|private",
  "photo_count": 0,
  "photos": [{
    "id": "uuid",
    "album_id": "uuid",
    "url": "string",
    "caption": "string",
    "position": 0,
    "created_at": "timestamp"
  }],
  "created_at": "timestamp"
}
```

## Handlers

### `ListAlbums(w, r)`
`GET /api/albums`

Returns the authenticated user's own albums.

### `CreateAlbum(w, r)`
`POST /api/albums`

**Body:** `{"title": "string", "description": "string", "visibility": "public|friends|private"}`

### `GetAlbum(w, r)`
`GET /api/albums/{id}`

Returns album with all photos. Applies visibility checks.

### `UpdateAlbum(w, r)`
`PATCH /api/albums/{id}`

Owner only. Accepts `title`, `description`, `visibility`.

### `DeleteAlbum(w, r)`
`DELETE /api/albums/{id}`

Owner only. Deletes album and all its photos.

### `AddPhoto(w, r)`
`POST /api/albums/{id}/photos`

**Body:** `{"url": "string", "caption": "string"}`

The URL should be a previously uploaded media URL (from `/api/media/upload`).

### `UpdatePhoto(w, r)`
`PATCH /api/albums/{id}/photos/{photoID}`

**Body:** `{"caption": "string", "position": 0}`

### `DeletePhoto(w, r)`
`DELETE /api/albums/{id}/photos/{photoID}`

### `ListUserAlbums(w, r)`
`GET /api/users/{username}/albums`

Returns albums for a given user, filtered by visibility:
- `public` albums — always visible
- `friends` albums — only if mutual friends
- `private` albums — never visible to others
