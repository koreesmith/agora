package media

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agora-social/agora/pkg/middleware"
	"github.com/agora-social/agora/pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Service struct {
	mediaDir string
}

func NewService(mediaDir string) *Service {
	os.MkdirAll(filepath.Join(mediaDir, "avatars"), 0755)
	os.MkdirAll(filepath.Join(mediaDir, "covers"), 0755)
	os.MkdirAll(filepath.Join(mediaDir, "posts"), 0755)
	os.MkdirAll(filepath.Join(mediaDir, "instance"), 0755)
	return &Service{mediaDir: mediaDir}
}

var allowedMimeTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

const maxUploadSize = 10 << 20 // 10MB

func RegisterRoutes(r chi.Router, svc *Service) {
	r.Post("/media/upload", svc.Upload)
}

func (s *Service) Upload(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	category := r.URL.Query().Get("category")
	if category == "" {
		category = "posts"
	}

	url, err := s.UploadImage(r, category, userID)
	if err != nil {
		utils.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	utils.JSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Service) UploadImage(r *http.Request, category, userID string) (string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		return "", fmt.Errorf("file too large (max 10MB)")
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return "", fmt.Errorf("no file provided")
	}
	defer file.Close()

	// Read first 512 bytes to detect content type
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	mimeType := http.DetectContentType(buf[:n])

	ext, ok := allowedMimeTypes[mimeType]
	if !ok {
		return "", fmt.Errorf("unsupported file type: %s", mimeType)
	}

	// Seek back to start
	if seeker, ok := file.(io.Seeker); ok {
		seeker.Seek(0, io.SeekStart)
	}

	_ = header
	filename := fmt.Sprintf("%s_%s_%d%s", userID, uuid.New().String(), time.Now().UnixNano(), ext)
	subdir := sanitizeCategory(category)
	destPath := filepath.Join(s.mediaDir, subdir, filename)

	dest, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to save file")
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		return "", fmt.Errorf("failed to write file")
	}

	return fmt.Sprintf("/media/%s/%s", subdir, filename), nil
}

func (s *Service) SaveInstanceLogo(r *http.Request) (string, error) {
	return s.UploadImage(r, "instance", "system")
}

func sanitizeCategory(category string) string {
	allowed := map[string]bool{"avatars": true, "covers": true, "posts": true, "instance": true}
	cat := strings.ToLower(category)
	if !allowed[cat] {
		return "posts"
	}
	return cat
}
