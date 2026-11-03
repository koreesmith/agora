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

## `GET /api/media/jobs/{id}` 🔒

Status of a video upload (`category=videos`), which transcodes in the background. Only the uploader can read their own job.

**Response 200:**
```json
{
  "status": "processing | done | failed",
  "url": "/uploads/videos/….mp4",
  "thumb_url": "/uploads/videos/…_thumb.jpg",
  "error": "",
  "width": 720,
  "height": 1280,
  "duration_ms": 12500
}
```
`width`, `height` and `duration_ms` describe the transcoded video and are 0 until the job is `done` (or if they couldn't be read). A post created with this job's `url` records them itself, so the client does not send them.

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
