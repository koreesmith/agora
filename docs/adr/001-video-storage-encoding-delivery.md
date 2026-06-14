# ADR-001: Video Storage, Encoding & Delivery Strategy

**Status:** Accepted  
**Ticket:** AGORA-129  
**Date:** 2026-06-05  
**Superseded by:** —

---

## Context

AGORA-119 will add the ability to post videos. Before implementation begins, we need a clear decision on how videos are stored, transcoded, and delivered. This ADR records the options considered and the chosen approach.

### Existing infrastructure constraints

Agora is **fully self-hosted** via Docker Compose on a single server. Key facts:
- All uploads currently stored on **local disk** at `./data/uploads/` (bind-mounted Docker volume)
- `nginx` serves static files from that volume — already supports HTTP byte-range requests
- `libheif-tools` is installed in the runtime image (for HEIC → JPEG conversion)
- **No S3, no CDN, no cloud provider** is in use today
- Redis is available but unused for media

---

## Decision

**Self-hosted ffmpeg transcoding → MP4/H.264 → served directly from local disk via nginx.**

No external services. No HLS for MVP. Consistent with the existing architecture.

---

## Detailed design

### Encoding pipeline

1. Client POSTs a video file to `POST /api/media/upload?category=videos` (multipart)
2. Backend writes the raw upload to a temp file
3. ffmpeg transcodes **synchronously on upload** to:
   - Container: **MP4**
   - Video codec: **H.264** (libx264), CRF 23, preset `fast`
   - Resolution: max **720p (1280×720)**, preserving aspect ratio (`scale=-2:720`)
   - Audio codec: **AAC**, 128 kbps, stereo
4. Output saved to `/data/uploads/videos/{uuid}.mp4`
5. Temp file deleted; URL returned to client
6. Frontend renders with `<video controls>` — works in all modern browsers and WKWebView (iOS)

### Limits

| Limit | Value | Rationale |
|---|---|---|
| Max upload size | **200 MB** | Generous for raw phone video; ffmpeg will compress it down |
| Max output duration | **2 minutes** | Keeps stored files manageable; approx 22 MB transcoded |
| Max output file size | **~50 MB** | Safety valve via ffmpeg `-fs` flag |
| Accepted input formats | MP4, MOV, AVI, MKV, WebM | Everything ffmpeg can read |

> If a file exceeds 2 minutes, the backend rejects the upload before transcoding with a clear error message.

### Storage structure

```
/data/uploads/
  avatars/
  posts/           ← existing images
  videos/          ← new: {uuid}.mp4 output files
  albums/
  instance/
```

### Dockerfile changes

Add `ffmpeg` to the runtime image:

```dockerfile
RUN apk --no-cache add ca-certificates tzdata netcat-openbsd libheif-tools ffmpeg
```

`ffmpeg` in Alpine is ~60 MB installed; includes libx264 and AAC support.

### Delivery

nginx already serves `/uploads/videos/*.mp4` as static files with **byte-range support** enabled by default. This allows:
- HTML5 `<video>` seeking without any extra config
- Progressive download (browser starts playing before download completes)

No separate CDN is needed at current scale. If the instance grows to where egress is a bottleneck, a Cloudflare proxy or a separate media origin can be dropped in front of the `/uploads` path without code changes.

### Mobile (iOS)

- **Playback:** `AVPlayer` / HTML5 `<video>` in WKWebView — native support for MP4/H.264. No changes needed.
- **Upload:** Standard multipart form upload same as images. The existing `uploadMedia` client function works unchanged; just pass `category=videos`.
- No app update required to support video playback or upload.

---

## Cost model

| Metric | Estimate |
|---|---|
| Transcoded size (2-min video) | ~22 MB |
| 1,000 videos | ~22 GB |
| 10,000 videos | ~220 GB |
| VPS disk cost at 220 GB | ~$5–15/month (depending on provider) |
| CPU for 2-min transcode | ~30–60 seconds on modern VPS |

The synchronous transcode approach means uploads block until transcoding completes (~30–60 s). This is acceptable for MVP. If it becomes a UX problem, we can move to async processing with a progress indicator (already have AGORA-117 for the upload modal).

---

## Rejected alternatives

### Option A: HLS/DASH adaptive streaming
- **Pros:** Adaptive bitrate (switches quality based on connection), better large-scale streaming
- **Cons:** Requires storing 15–20 segment files per video, complex nginx `add_header` config, browser support needs JS player (hls.js), significant extra complexity
- **Verdict:** Overkill for MVP. Revisit at 10k+ active users.

### Option B: Cloudflare Stream or Mux
- **Pros:** Automatic transcoding, global CDN, adaptive bitrate, analytics
- **Cons:** External dependency contradicts the self-hosted ethos; Cloudflare Stream costs $5/1,000 minutes stored + $1/1,000 minutes delivered; requires a Cloudflare account; data leaves the instance
- **Verdict:** Not appropriate for a self-hosted-first product. Could be offered as an opt-in integration for instances that want it.

### Option C: MinIO (S3-compatible self-hosted storage)
- **Pros:** S3-compatible API, separates media storage from the app container, easier horizontal scaling
- **Cons:** Adds another service to the Docker Compose stack; no benefit over local disk at current scale; migrations required for existing image uploads
- **Verdict:** Good future option if the server outgrows single-disk storage. Defer until needed.

### Option D: Async transcode queue
- **Pros:** Upload returns immediately, better UX for large files
- **Cons:** Requires a background worker, job queue (Redis or DB-backed), progress polling API, more complex error handling
- **Verdict:** Defer to a future sprint if synchronous transcode latency becomes a complaint. AGORA-117 (upload progress modal) partially mitigates the UX concern.

---

## Implementation notes for AGORA-119

1. Add `ffmpeg` to `Dockerfile` runtime image
2. Update `internal/media/media.go`:
   - New `processVideo(src string) (outputPath string, err error)` function wrapping `exec.Command("ffmpeg", ...)`
   - Pre-flight check: probe duration with `ffprobe` before transcoding; reject if > 2 minutes
   - New `videos` category in `categoryDimensions` map (or equivalent)
3. Update `upload_dir` mkdir to include `videos/` subdirectory
4. Frontend: `CreatePost.tsx` — add video file input alongside the image input; render with `<video>` tag in preview and feed
5. `post_photos` table already supports multiple media URLs; add a `media_type` column or handle video URL detection client-side by extension

---

## Open questions (deferred)

- Should we store the original raw upload for re-transcoding later, or delete it immediately after transcoding? (**Recommendation:** delete immediately to save disk)
- Should thumbnails/poster frames be generated via ffmpeg? (**Recommendation:** yes, generate a single frame at 1s as poster image; store as `/uploads/videos/{uuid}_thumb.jpg`)
- Audio-only posts? (**Recommendation:** out of scope for AGORA-119)
