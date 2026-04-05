# Media API

---

## `POST /api/media/upload` 🔒

Upload a file for use in posts, albums, or profile.

**Request:** `multipart/form-data`
- `file` — the file to upload
- `category` — one of: `posts`, `albums`, `avatar`, `cover`

**Limits:**
- Max size: 50MB
- Accepted types: JPEG, PNG, GIF, WebP, HEIC/HEIF

**Response 200:**
```json
{ "url": "/uploads/posts/a1b2c3d4e5f6.jpg" }
```

Use the returned URL as the `image_url` value in `POST /api/posts` or `POST /api/albums/{id}/photos`.

---

## `GET /uploads/{filename}` (public)

Serves uploaded files directly. No authentication required.

Files are organized into subdirectories by category:
- `/uploads/avatars/...`
- `/uploads/covers/...`
- `/uploads/posts/...`
- `/uploads/albums/...`

---

🔒 = requires `Authorization: Bearer <token>`
