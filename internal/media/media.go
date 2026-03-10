package media

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/agora-social/agora/internal/auth"
)

type Service struct {
	uploadDir string
}

func NewService(uploadDir string) *Service {
	os.MkdirAll(filepath.Join(uploadDir, "avatars"), 0755)
	os.MkdirAll(filepath.Join(uploadDir, "covers"), 0755)
	os.MkdirAll(filepath.Join(uploadDir, "posts"), 0755)
	os.MkdirAll(filepath.Join(uploadDir, "instance"), 0755)
	return &Service{uploadDir: uploadDir}
}

func (s *Service) UploadDir() string {
	return s.uploadDir
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Post("/media/upload", s.Upload)
}

func (s *Service) FileServer() http.Handler {
	return http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.uploadDir)))
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
	r.ParseMultipartForm(10 << 20) // 10 MB max

	file, header, err := r.FormFile("file")
	if err != nil {
		return "", fmt.Errorf("no file in request")
	}
	defer file.Close()

	// Validate content type
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	contentType := http.DetectContentType(buf[:n])
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("only image files are allowed")
	}
	file.Seek(0, io.SeekStart)

	// Determine extension
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		switch contentType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		default:
			ext = ".jpg"
		}
	}

	// Save file
	filename := uuid.New().String() + ext
	dir := filepath.Join(s.uploadDir, category)
	os.MkdirAll(dir, 0755)
	dest := filepath.Join(dir, filename)

	out, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("could not save file")
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		return "", fmt.Errorf("could not write file")
	}

	return fmt.Sprintf("/uploads/%s/%s", category, filename), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
