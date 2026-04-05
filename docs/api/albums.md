# Albums API

All endpoints require `Authorization: Bearer <token>`.

---

## `GET /api/albums`
Own albums only.
**Response 200:** `[...album objects...]`

## `POST /api/albums`
**Body:** `{"title": "string", "description": "string", "visibility": "public|friends|private"}`
**Response 201:** Album object

## `GET /api/albums/{id}`
**Response 200:** Album object (with `photos` array)

## `PATCH /api/albums/{id}`
Owner only. Accepts: `title`, `description`, `visibility`
**Response 200:** Updated album object

## `DELETE /api/albums/{id}`
Owner only.
**Response 204**

## `POST /api/albums/{id}/photos`
**Body:** `{"url": "/uploads/albums/...", "caption": "string"}`
**Response 201:** Photo object

## `PATCH /api/albums/{id}/photos/{photoID}`
**Body:** `{"caption": "string", "position": 0}`
**Response 200:** Updated photo object

## `DELETE /api/albums/{id}/photos/{photoID}`
**Response 204**

## `GET /api/users/{username}/albums`
Returns public (and friends-only for friends) albums for the given user.
**Response 200:** `[...album objects (without photos array)...]`

---

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
