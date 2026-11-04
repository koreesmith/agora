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

### Videos (`category=videos`)

A video is accepted as MP4, MOV, AVI, MKV or WebM, up to 2 GB, and as long as the instance allows (2 minutes unless an admin lowered it; `GET /api/instance` reports it as `video_max_seconds`). It transcodes in the background. The response is a job to poll, not a URL:

```json
{ "job_id": "…", "status": "processing" }
```

Poll [`GET /api/media/jobs/{id}`](#get-apimediajobsid-) for the result, then use its `url` and `thumb_url` as `video_url` and `video_thumb_url` in `POST /api/posts`.

**Optional form fields**, for cutting a clip down and choosing its cover:

| Field | Meaning |
|---|---|
| `trim_start_ms` | Where the clip starts in the original video, in milliseconds. Default 0. |
| `trim_end_ms` | Where it ends. Default the end of the video. |
| `cover_ms` | The cover frame's time **within the trimmed clip**, in milliseconds. Default is the frame one second in. |

The length limit applies to the part that is kept, so a longer video can be uploaded as long as the trimmed range is within it. Without trim fields the whole video has to fit, as before. The trim happens during the one transcode, with audio and video kept in step.

A request is refused with 400 and a message when:
- a field is not a whole number of milliseconds, 0 or more;
- the trim starts after the end of the video, ends more than half a second past it, ends at or before where it starts, or leaves less than half a second;
- the part kept is longer than the instance's limit;
- the account is already at its video storage limit (`video_quota_mb`), which is checked before the upload is read.

A `cover_ms` outside the clip is not an error: the usual frame is used instead. Keeping the whole video (start 0, end at or near the end) is the same as no trim. The job's `width`, `height` and `duration_ms` describe the trimmed result.

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
