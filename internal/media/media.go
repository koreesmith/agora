package media

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/agora-social/agora/internal/auth"
)

const (
	maxPostWidth  = 1200
	maxPostHeight = 1200
	maxAvatarSize = 400
	maxCoverWidth = 1200
	maxCoverHeight = 400
	jpegQuality   = 85
)

type Service struct {
	uploadDir string
}

func NewService(uploadDir string) *Service {
	for _, d := range []string{"avatar", "cover", "posts", "instance"} {
		os.MkdirAll(filepath.Join(uploadDir, d), 0755)
	}
	return &Service{uploadDir: uploadDir}
}

func (s *Service) UploadDir() string { return s.uploadDir }

func RegisterRoutes(r chi.Router, s *Service) {
	r.Post("/media/upload", s.Upload)
}

func (s *Service) FileServer() http.Handler {
	return http.StripPrefix("/uploads", http.FileServer(http.Dir(s.uploadDir)))
}

func (s *Service) Upload(w http.ResponseWriter, r *http.Request) {
	_ = auth.UserIDFromCtx(r.Context())
	category := r.URL.Query().Get("category")
	if category == "" {
		category = "posts"
	}
	url, err := s.SaveUpload(r, category, "")
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"url": url})
}

func (s *Service) SaveUpload(r *http.Request, category, _ string) (string, error) {
	r.ParseMultipartForm(20 << 20)

	file, _, err := r.FormFile("file")
	if err != nil {
		return "", fmt.Errorf("no file in request")
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("could not read file")
	}

	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("only image files are allowed")
	}

	var maxW, maxH int
	switch category {
	case "avatar":
		maxW, maxH = maxAvatarSize, maxAvatarSize
	case "cover":
		maxW, maxH = maxCoverWidth, maxCoverHeight
	default:
		maxW, maxH = maxPostWidth, maxPostHeight
	}

	// GIFs saved as-is; everything else decoded, resized, re-encoded as JPEG
	var outData []byte
	ext := ".jpg"

	if contentType == "image/gif" {
		ext = ".gif"
		outData = data
	} else {
		img, _, decErr := image.Decode(bytes.NewReader(data))
		if decErr != nil {
			// Fallback: save raw (e.g. webp on old Go)
			ext = ".bin"
			outData = data
		} else {
			resized := resizeToFit(img, maxW, maxH)
			var buf bytes.Buffer
			if encErr := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: jpegQuality}); encErr != nil {
				return "", fmt.Errorf("could not encode image")
			}
			outData = buf.Bytes()
		}
	}

	filename := uuid.New().String() + ext
	dir := filepath.Join(s.uploadDir, category)
	os.MkdirAll(dir, 0755)

	if err := os.WriteFile(filepath.Join(dir, filename), outData, 0644); err != nil {
		return "", fmt.Errorf("could not save file")
	}
	return fmt.Sprintf("/uploads/%s/%s", category, filename), nil
}

// resizeToFit scales img down to fit within maxW×maxH preserving aspect ratio.
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
func lerp(a, b, t float64) float64       { return a + (b-a)*t }
func lerp2(a, b, c, d, fx, fy float64) float64 { return lerp(lerp(a, b, fx), lerp(c, d, fx), fy) }
func clamp(v float64) uint8 {
	if v < 0 { return 0 }
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
