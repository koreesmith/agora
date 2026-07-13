package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/store"
)

const (
	maxPostWidth   = 2400
	maxPostHeight  = 2400
	maxAlbumWidth  = 2400
	maxAlbumHeight = 2400
	maxAvatarSize  = 400
	maxCoverWidth  = 1400
	maxCoverHeight = 500
	jpegQuality    = 88
	// 50MB — covers 48MP RAW JPEGs from modern iPhones/DSLRs
	maxUploadBytes = 50 << 20
	// 2GB raw video upload limit; actual constraint is 2-minute duration via ffprobe
	maxVideoUploadBytes = 2 << 30
	maxVideoDurationSec = 120
	// Hard cap on how long a single ffmpeg transcode may run before it is killed,
	// so a pathological input can't hold a transcode slot forever.
	transcodeTimeout = 15 * time.Minute
)

// transcodeSem limits how many ffmpeg transcodes run concurrently. Each ffmpeg
// invocation already uses every CPU core (-threads 0), so running many at once
// only thrashes the box — phone uploads mostly arrive one at a time per user.
var transcodeSem = make(chan struct{}, 2)

// heicMagic identifies HEIC/HEIF files by their ftyp box.
// HEIC files start with a 4-byte length, then "ftyp", then a brand like "heic", "heix", "mif1", etc.
func isHEIC(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	if string(data[4:8]) != "ftyp" {
		return false
	}
	brand := strings.ToLower(string(data[8:12]))
	return brand == "heic" || brand == "heix" || brand == "hevc" ||
		brand == "mif1" || brand == "msf1" || brand == "avif"
}

type Service struct {
	db        *store.DB
	uploadDir string
}

func NewService(db *store.DB, uploadDir string) *Service {
	for _, d := range []string{"avatar", "cover", "posts", "instance", "albums", "videos"} {
		os.MkdirAll(filepath.Join(uploadDir, d), 0755)
	}
	// Transcode jobs run in-process; any left "processing" belong to a previous
	// run whose goroutine died with the process. Fail them so clients polling
	// those jobs stop waiting instead of spinning forever. (AGORA-137)
	if db != nil {
		db.Exec(`UPDATE video_transcode_jobs SET status='failed', error='video processing was interrupted — please try again', updated_at=NOW() WHERE status='processing'`)
	}
	return &Service{db: db, uploadDir: uploadDir}
}

func (s *Service) UploadDir() string { return s.uploadDir }

func RegisterRoutes(r chi.Router, s *Service) {
	r.Post("/media/upload", s.Upload)
	r.Get("/media/jobs/{id}", s.VideoJobStatus)
}

func (s *Service) FileServer() http.Handler {
	return http.StripPrefix("/uploads", http.FileServer(http.Dir(s.uploadDir)))
}

func (s *Service) Upload(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	category := r.URL.Query().Get("category")
	if category == "" {
		category = "posts"
	}

	// Videos go through a separate, asynchronous pipeline (AGORA-137): the raw
	// file is saved and validated inline, then transcoding runs in a background
	// goroutine so the request returns immediately — well within Cloudflare's
	// 100s origin timeout — instead of blocking on ffmpeg.
	if category == "videos" {
		jobID, err := s.StartVideoJob(r, userID)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"job_id": jobID, "status": "processing"})
		return
	}

	url, err := s.SaveUpload(r, category, "")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"url": url})
}

// StartVideoJob saves and validates the raw upload inline, records a
// video_transcode_jobs row in the "processing" state, and kicks off transcoding
// in a background goroutine. It returns the job ID immediately; the caller polls
// VideoJobStatus for completion. (AGORA-137)
func (s *Service) StartVideoJob(r *http.Request, userID string) (jobID string, err error) {
	// 32MB in memory; larger uploads are spooled to disk by the multipart parser
	r.ParseMultipartForm(32 << 20)
	file, _, ferr := r.FormFile("file")
	if ferr != nil {
		return "", fmt.Errorf("no file attached")
	}
	defer file.Close()

	// Stream directly to a temp file — avoids loading the raw upload into RAM.
	// This file outlives the request (the background goroutine reads it), so it
	// is removed by runTranscode rather than deferred here.
	in, terr := os.CreateTemp("", "video-raw-*")
	if terr != nil {
		return "", fmt.Errorf("internal error")
	}
	rawPath := in.Name()

	n, copyErr := io.Copy(in, io.LimitReader(file, maxVideoUploadBytes+1))
	in.Close()
	if copyErr != nil {
		os.Remove(rawPath)
		return "", fmt.Errorf("could not read file")
	}
	if n > maxVideoUploadBytes {
		os.Remove(rawPath)
		return "", fmt.Errorf("video is too large (max 2 GB)")
	}

	// Pre-flight: check duration with ffprobe. This is cheap (metadata only) and
	// lets us reject invalid or over-length videos with a clear error before we
	// create a job or spend any transcode time.
	dur, derr := videoDuration(rawPath)
	if derr != nil {
		os.Remove(rawPath)
		return "", fmt.Errorf("could not read video — ensure it is a valid MP4, MOV, AVI, MKV, or WebM file")
	}
	if dur > float64(maxVideoDurationSec) {
		os.Remove(rawPath)
		return "", fmt.Errorf("video is too long (max 2 minutes — this video is %.0f seconds)", dur)
	}

	id := uuid.New().String()
	if err := s.db.QueryRow(
		`INSERT INTO video_transcode_jobs (id, user_id, status) VALUES ($1, $2, 'processing') RETURNING id`,
		id, userID,
	).Scan(&jobID); err != nil {
		os.Remove(rawPath)
		return "", fmt.Errorf("internal error")
	}

	go s.runTranscode(jobID, rawPath, id)
	return jobID, nil
}

// runTranscode performs the actual ffmpeg work off the request path and records
// the outcome on the job row. The output is written to a temp file and atomically
// renamed into place only on success, so the served URL never points at a
// partially-written (unplayable) file.
func (s *Service) runTranscode(jobID, rawPath, id string) {
	defer os.Remove(rawPath)

	// Bound concurrency so we don't run more CPU-saturating ffmpeg jobs than the
	// box can handle. The job stays "processing" while it waits for a slot.
	transcodeSem <- struct{}{}
	defer func() { <-transcodeSem }()

	ctx, cancel := context.WithTimeout(context.Background(), transcodeTimeout)
	defer cancel()

	videosDir := filepath.Join(s.uploadDir, "videos")
	tmpOut := filepath.Join(videosDir, id+".tmp.mp4")
	finalOut := filepath.Join(videosDir, id+".mp4")
	thumbPath := filepath.Join(videosDir, id+"_thumb.jpg")

	// Transcode to H.264 MP4 (720p max, CRF 26, faststart for web streaming).
	// -pix_fmt yuv420p forces 4:2:0 chroma: libx264 otherwise preserves the
	// source pixel format, and browsers cannot decode 4:4:4/4:2:2 H.264, so a
	// non-4:2:0 source would transcode "successfully" yet refuse to play.
	ffmpegArgs := []string{
		"-i", rawPath,
		"-vf", "scale=-2:min(720\\,ih)",
		"-c:v", "libx264", "-crf", "26", "-preset", "ultrafast",
		"-pix_fmt", "yuv420p",
		"-threads", "0",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		"-y", tmpOut,
	}
	if out, cerr := exec.CommandContext(ctx, "ffmpeg", ffmpegArgs...).CombinedOutput(); cerr != nil {
		os.Remove(tmpOut)
		log.Printf("media: transcode job %s failed: %v — %s", jobID, cerr, strings.TrimSpace(string(out)))
		s.failJob(jobID, "video processing failed — the file may be corrupt or in an unsupported format")
		return
	}

	// Atomically move the completed file into the served location.
	if err := os.Rename(tmpOut, finalOut); err != nil {
		os.Remove(tmpOut)
		log.Printf("media: transcode job %s could not finalize output: %v", jobID, err)
		s.failJob(jobID, "video processing failed — could not save output")
		return
	}

	// Generate poster thumbnail at 1 second — non-fatal; thumbnail is optional.
	thumbArgs := []string{
		"-i", finalOut,
		"-ss", "1", "-vframes", "1",
		"-vf", "scale=-2:320",
		"-y", thumbPath,
	}
	thumbURL := ""
	if err := exec.CommandContext(ctx, "ffmpeg", thumbArgs...).Run(); err == nil {
		thumbURL = fmt.Sprintf("/uploads/videos/%s_thumb.jpg", id)
	}

	videoURL := fmt.Sprintf("/uploads/videos/%s.mp4", id)
	s.db.Exec(
		`UPDATE video_transcode_jobs SET status='done', video_url=$2, video_thumb_url=$3, updated_at=NOW() WHERE id=$1`,
		jobID, videoURL, thumbURL,
	)
}

// failJob marks a transcode job as failed with a user-facing error message.
func (s *Service) failJob(jobID, msg string) {
	s.db.Exec(
		`UPDATE video_transcode_jobs SET status='failed', error=$2, updated_at=NOW() WHERE id=$1`,
		jobID, msg,
	)
}

// VideoJobStatus returns the current state of a transcode job. It is scoped to
// the requesting user so callers can only poll their own jobs.
func (s *Service) VideoJobStatus(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	jobID := chi.URLParam(r, "id")

	var status, videoURL, thumbURL, errMsg string
	err := s.db.QueryRow(
		`SELECT status, video_url, video_thumb_url, error FROM video_transcode_jobs WHERE id=$1 AND user_id=$2`,
		jobID, userID,
	).Scan(&status, &videoURL, &thumbURL, &errMsg)
	if err != nil {
		writeError(w, 404, "job not found")
		return
	}

	writeJSON(w, 200, map[string]string{
		"status":    status,
		"url":       videoURL,
		"thumb_url": thumbURL,
		"error":     errMsg,
	})
}

// videoDuration uses ffprobe to return the video duration in seconds.
func videoDuration(path string) (float64, error) {
	out, err := exec.Command("ffprobe",
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return 0, err
	}
	var dur float64
	_, err = fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &dur)
	return dur, err
}

func (s *Service) SaveUpload(r *http.Request, category, _ string) (string, error) {
	// Allow up to 50MB in memory/temp
	r.ParseMultipartForm(maxUploadBytes)

	file, header, err := r.FormFile("file")
	if err != nil {
		return "", fmt.Errorf("no file attached — make sure you selected an image")
	}
	defer file.Close()

	// Limit read to 50MB to avoid memory exhaustion
	limited := io.LimitReader(file, maxUploadBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("could not read file")
	}
	if int64(len(data)) > maxUploadBytes {
		return "", fmt.Errorf("file is too large (max 50 MB) — try reducing the image size before uploading")
	}

	// HEIC/HEIF — convert to JPEG transparently using heif-convert
	var contentType string
	if isHEIC(data) {
		converted, err := convertHEIC(data)
		if err != nil {
			return "", fmt.Errorf("could not convert HEIC photo — try exporting as JPEG from your Photos app")
		}
		data = converted
		contentType = "image/jpeg"
	} else {
		contentType = http.DetectContentType(data)
	}

	if !strings.HasPrefix(contentType, "image/") {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		return "", fmt.Errorf(
			"unsupported file type (%s). Please upload a JPEG, PNG, or GIF image.", ext)
	}

	var maxW, maxH int
	switch category {
	case "avatar":
		maxW, maxH = maxAvatarSize, maxAvatarSize
	case "cover":
		maxW, maxH = maxCoverWidth, maxCoverHeight
	case "albums":
		maxW, maxH = maxAlbumWidth, maxAlbumHeight
	default:
		maxW, maxH = maxPostWidth, maxPostHeight
	}

	// GIFs saved as-is
	if contentType == "image/gif" {
		filename := uuid.New().String() + ".gif"
		return s.saveBytes(data, category, filename)
	}

	// Read EXIF orientation before decoding — Go's image decoder discards it.
	orientation := exifOrientation(data)

	// Decode, resize, re-encode as JPEG
	img, _, decErr := image.Decode(bytes.NewReader(data))
	if decErr != nil {
		// WebP or other format Go can't decode — save raw with detected extension
		ext := extensionFor(contentType)
		filename := uuid.New().String() + ext
		return s.saveBytes(data, category, filename)
	}

	img = applyOrientation(img, orientation)
	resized := resizeToFit(img, maxW, maxH)
	var buf bytes.Buffer
	if encErr := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: jpegQuality}); encErr != nil {
		return "", fmt.Errorf("could not process image")
	}

	filename := uuid.New().String() + ".jpg"
	return s.saveBytes(buf.Bytes(), category, filename)
}

func (s *Service) saveBytes(data []byte, category, filename string) (string, error) {
	dir := filepath.Join(s.uploadDir, category)
	os.MkdirAll(dir, 0755)
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0644); err != nil {
		return "", fmt.Errorf("could not save file — disk may be full")
	}
	return fmt.Sprintf("/uploads/%s/%s", category, filename), nil
}

func extensionFor(contentType string) string {
	switch contentType {
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	default:
		return ".bin"
	}
}

// convertHEIC converts HEIC/HEIF image data to JPEG using heif-convert.
// Writes to a temp file, runs the conversion, reads the result back.
func convertHEIC(data []byte) ([]byte, error) {
	// Write input to temp file
	in, err := os.CreateTemp("", "heic-in-*.heic")
	if err != nil {
		return nil, err
	}
	defer os.Remove(in.Name())
	if _, err := in.Write(data); err != nil {
		in.Close()
		return nil, err
	}
	in.Close()

	// Output path
	outPath := in.Name() + ".jpg"
	defer os.Remove(outPath)

	// heif-convert <input> <output>
	cmd := exec.Command("heif-convert", "-q", "88", in.Name(), outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("heif-convert: %w — %s", err, string(out))
	}

	return os.ReadFile(outPath)
}

// resizeToFit scales img down to fit within maxW×maxH preserving aspect ratio.
// Uses a fast box-filter (average of source pixels) for large downscales,
// and bilinear for small adjustments.
func resizeToFit(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= maxW && srcH <= maxH {
		return img
	}
	scaleW := float64(maxW) / float64(srcW)
	scaleH := float64(maxH) / float64(srcH)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	dstW := max1(int(float64(srcW)*scale), 1)
	dstH := max1(int(float64(srcH)*scale), 1)

	// For large downscales (scale < 0.5), use box filter — much faster than bilinear
	// on e.g. a 12MP → 800px resize.
	if scale < 0.5 {
		return boxResize(img, dstW, dstH)
	}

	// Bilinear for gentle resizes
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			sx := float64(x) / scale
			sy := float64(y) / scale
			x0, y0 := int(sx), int(sy)
			x1, y1 := x0+1, y0+1
			if x1 >= srcW { x1 = srcW - 1 }
			if y1 >= srcH { y1 = srcH - 1 }
			fx, fy := sx-float64(x0), sy-float64(y0)
			c00 := rgbaF(img.At(b.Min.X+x0, b.Min.Y+y0))
			c10 := rgbaF(img.At(b.Min.X+x1, b.Min.Y+y0))
			c01 := rgbaF(img.At(b.Min.X+x0, b.Min.Y+y1))
			c11 := rgbaF(img.At(b.Min.X+x1, b.Min.Y+y1))
			dst.SetRGBA(x, y, color.RGBA{
				R: clamp(lerp2(c00[0], c10[0], c01[0], c11[0], fx, fy)),
				G: clamp(lerp2(c00[1], c10[1], c01[1], c11[1], fx, fy)),
				B: clamp(lerp2(c00[2], c10[2], c01[2], c11[2], fx, fy)),
				A: 255,
			})
		}
	}
	return dst
}

// boxResize averages blocks of source pixels — fast and good quality for large downscales.
func boxResize(src image.Image, dstW, dstH int) image.Image {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	scaleX := float64(srcW) / float64(dstW)
	scaleY := float64(srcH) / float64(dstH)
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := 0; dy < dstH; dy++ {
		sy0 := int(float64(dy) * scaleY)
		sy1 := int(float64(dy+1) * scaleY)
		if sy1 > srcH { sy1 = srcH }
		if sy1 <= sy0 { sy1 = sy0 + 1 }
		for dx := 0; dx < dstW; dx++ {
			sx0 := int(float64(dx) * scaleX)
			sx1 := int(float64(dx+1) * scaleX)
			if sx1 > srcW { sx1 = srcW }
			if sx1 <= sx0 { sx1 = sx0 + 1 }
			var rSum, gSum, bSum float64
			count := float64((sx1 - sx0) * (sy1 - sy0))
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					c := rgbaF(src.At(b.Min.X+sx, b.Min.Y+sy))
					rSum += c[0]; gSum += c[1]; bSum += c[2]
				}
			}
			dst.SetRGBA(dx, dy, color.RGBA{
				R: clamp(rSum / count),
				G: clamp(gSum / count),
				B: clamp(bSum / count),
				A: 255,
			})
		}
	}
	return dst
}

func rgbaF(c color.Color) [4]float64 {
	r, g, bb, a := c.RGBA()
	if a == 0 {
		return [4]float64{255, 255, 255, 255}
	}
	return [4]float64{
		float64(r) * 255 / float64(a),
		float64(g) * 255 / float64(a),
		float64(bb) * 255 / float64(a),
		255,
	}
}
func lerp(a, b, t float64) float64            { return a + (b-a)*t }
func lerp2(a, b, c, d, fx, fy float64) float64 { return lerp(lerp(a, b, fx), lerp(c, d, fx), fy) }
func clamp(v float64) uint8 {
	if v < 0   { return 0 }
	if v > 255 { return 255 }
	return uint8(v)
}
func max1(a, b int) int {
	if a > b { return a }
	return b
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
