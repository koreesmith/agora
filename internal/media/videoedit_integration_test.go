package media

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-439, end to end: upload a video with trim and cover fields through
// StartVideoJob, let the real ffmpeg transcode run, and check what comes out.
//
// Needs ffmpeg with libx264 (the transcode uses it) and the local
// agora-postgres-test instance (localhost:15433); skips without either.
func TestVideoTrimAndCoverEndToEnd(t *testing.T) {
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output(); err != nil || !strings.Contains(string(out), "libx264") {
		t.Skip("skipping: ffmpeg with libx264 not available")
	}
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	uploadDir := t.TempDir()
	svc := &Service{db: db, uploadDir: uploadDir}
	os.MkdirAll(filepath.Join(uploadDir, "videos"), 0755)

	name := fmt.Sprintf("tc439_%d", time.Now().UnixNano())
	var userID string
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		name, name+"@example.invalid").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	makeVideo := func(secs int) string {
		p := filepath.Join(t.TempDir(), fmt.Sprintf("src%d.mp4", secs))
		out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi",
			"-i", "testsrc=size=160x120:rate=5", "-t", fmt.Sprint(secs),
			"-pix_fmt", "yuv420p", "-c:v", "libx264", "-preset", "ultrafast", "-y", p).CombinedOutput()
		if err != nil {
			t.Fatalf("make %ds video: %v %s", secs, err, out)
		}
		return p
	}
	short, long := makeVideo(12), makeVideo(150)

	upload := func(path string, fields map[string]string) (string, error) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", filepath.Base(path))
		data, _ := os.ReadFile(path)
		fw.Write(data)
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		mw.Close()
		req := httptest.NewRequest("POST", "/media/upload?category=videos", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return svc.StartVideoJob(req, userID)
	}

	type result struct {
		status, url, thumb, errMsg string
		durMs                      int
	}
	wait := func(jobID string) result {
		deadline := time.Now().Add(120 * time.Second)
		for time.Now().Before(deadline) {
			var r result
			db.QueryRow(`SELECT status, video_url, video_thumb_url, error, video_duration_ms FROM video_transcode_jobs WHERE id = $1`, jobID).
				Scan(&r.status, &r.url, &r.thumb, &r.errMsg, &r.durMs)
			if r.status != "processing" {
				return r
			}
			time.Sleep(300 * time.Millisecond)
		}
		t.Fatalf("job %s never finished", jobID)
		return result{}
	}
	run := func(path string, fields map[string]string) result {
		t.Helper()
		id, err := upload(path, fields)
		if err != nil {
			t.Fatalf("upload %v: %v", fields, err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM video_transcode_jobs WHERE id = $1`, id) })
		r := wait(id)
		if r.status != "done" {
			t.Fatalf("job for %v ended %s: %s", fields, r.status, r.errMsg)
		}
		return r
	}
	thumbHash := func(r result) [32]byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(uploadDir, "videos", filepath.Base(r.thumb)))
		if err != nil {
			t.Fatalf("no thumbnail for %v: %v", r, err)
		}
		return sha256.Sum256(b)
	}
	near := func(got, want int) bool { return got >= want-400 && got <= want+400 }

	// No fields: the whole video, as before.
	whole := run(short, nil)
	if !near(whole.durMs, 12000) {
		t.Errorf("untrimmed: duration %dms, want about 12000", whole.durMs)
	}

	// Trim 3s..8s of a 12s video.
	trimmed := run(short, map[string]string{"trim_start_ms": "3000", "trim_end_ms": "8000"})
	if !near(trimmed.durMs, 5000) {
		t.Errorf("trim 3s-8s: duration %dms, want about 5000", trimmed.durMs)
	}

	// Cover: a frame from late in the video differs from the default frame,
	// and a time outside the clip falls back to the default frame.
	covered := run(short, map[string]string{"cover_ms": "9000"})
	outside := run(short, map[string]string{"cover_ms": "999999"})
	if thumbHash(covered) == thumbHash(whole) {
		t.Error("cover at 9s produced the same thumbnail as the default frame")
	}
	if thumbHash(outside) != thumbHash(whole) {
		t.Error("a cover time outside the clip should fall back to the default frame")
	}

	// The cover is a time within the trimmed clip: 1s into a clip that starts
	// at 6s is the frame at 7s, which is not the default frame at 1s.
	inTrim := run(short, map[string]string{"trim_start_ms": "6000", "trim_end_ms": "11000", "cover_ms": "1000"})
	if !near(inTrim.durMs, 5000) {
		t.Errorf("trim 6s-11s: duration %dms, want about 5000", inTrim.durMs)
	}
	if thumbHash(inTrim) == thumbHash(whole) {
		t.Error("cover 1s into a clip starting at 6s matched the first-second frame of the original")
	}

	// A video over the limit: refused whole, accepted when trimmed to fit.
	if _, err := upload(long, nil); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("150s video untrimmed: err = %v, want a too-long error", err)
	}
	if _, err := upload(long, map[string]string{"trim_start_ms": "10000", "trim_end_ms": "200000"}); err == nil {
		t.Error("a trim past the end of the video was accepted")
	}
	if _, err := upload(long, map[string]string{"trim_start_ms": "0", "trim_end_ms": "140000"}); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("a 140s trim: err = %v, want a too-long error", err)
	}
	cut := run(long, map[string]string{"trim_start_ms": "10000", "trim_end_ms": "40000"})
	if !near(cut.durMs, 30000) {
		t.Errorf("150s video trimmed to 30s: duration %dms, want about 30000", cut.durMs)
	}
}
