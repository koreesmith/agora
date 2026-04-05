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
