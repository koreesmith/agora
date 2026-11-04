# Media Service

**Package:** `internal/media`
**File:** `internal/media/media.go`

Handles file uploads with automatic resizing and HEIC conversion. Serves uploaded files as static assets.

## Constructor

```go
func NewService(uploadDir string) *Service
```

`uploadDir` defaults to `./data/uploads` (from `config.UploadDir`).

## Upload Categories and Dimensions

| Category | Max dimensions | Notes |
|----------|---------------|-------|
| `avatar` | 400×400 | Square crop |
| `cover` | 1400×500 | Banner crop |
| `posts` | 2400×2400 | Preserves aspect ratio |
| `albums` | 2400×2400 | Preserves aspect ratio |

## Handlers

### `Upload(w, r)`
`POST /api/media/upload`

Multipart form upload. Field name: `file`. Additional form field: `category`.

**Response:**
```json
{ "url": "/uploads/posts/a1b2c3d4.jpg" }
```

**Limits:**
- Max file size: 50MB
- Accepted types: JPEG, PNG, GIF, WebP, HEIC/HEIF

**Processing:**
1. Detect HEIC/HEIF → convert to JPEG via `heif-convert` CLI tool
2. GIF → preserved as-is (not re-encoded)
3. All others → decoded, resized to category max, re-encoded as JPEG at quality 88

### `FileServer() http.Handler`

Returns an `http.FileServer` rooted at `uploadDir`. Mounted at `/uploads` in the router.

## Internal Helper

### `SaveUpload(r *http.Request, category string) (url string, err error)`

Used by other services (e.g., `users.UploadAvatar`) to process and save a file from a multipart request without going through the `POST /api/media/upload` endpoint.

## Video (`category=videos`)

Videos use a separate, asynchronous path: the raw upload is saved and checked
inline, then transcoded by `ffmpeg` in the background (`StartVideoJob`,
`runTranscode`). See [ADR-001](../adr/001-video-storage-encoding-delivery.md)
and [the media API](../api/media.md#videos-categoryvideos).

- **Trim and cover (AGORA-439).** `trim_start_ms`, `trim_end_ms` and `cover_ms`
  are parsed and checked by `parseVideoEdit` (`videoedit.go`). The trim is applied
  during the one transcode, and the thumbnail is taken at the cover time.
- **Size and length (AGORA-437).** `ProbeVideo` (`videometa.go`) reads a
  video's display size and duration with `ffprobe`. They are stored on the job and
  copied to the post. `BackfillVideoMeta` and `BackfillVideoBytes` fill them in for
  older videos at startup.
- **Limits (AGORA-444, `videolimits.go`).** The longest video is the
  `video_max_seconds` setting, 5 to 120 seconds, read when an upload arrives. The
  per-account limit is `video_quota_mb`, counted from `posts.video_bytes` for the
  account's posts that still exist and checked before the upload is read; the video
  that takes an account over the line is allowed, since its size isn't known until
  it has been transcoded.
- **Takedown.** `RemoveVideoFiles` deletes a video and its thumbnail from disk. It
  only touches files directly under `uploads/videos/` and ignores anything else, such
  as a remote URL or a path that tries to leave the directory.

