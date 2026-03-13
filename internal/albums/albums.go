package albums

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/media"
	"github.com/agora-social/agora/internal/store"
)

type Service struct {
	db    *store.DB
	media *media.Service
}

func NewService(db *store.DB, media *media.Service) *Service {
	return &Service{db: db, media: media}
}

func RegisterRoutes(r chi.Router, s *Service) {
	r.Get("/albums",                              s.ListAlbums)
	r.Post("/albums",                             s.CreateAlbum)
	r.Get("/albums/{id}",                         s.GetAlbum)
	r.Patch("/albums/{id}",                       s.UpdateAlbum)
	r.Delete("/albums/{id}",                      s.DeleteAlbum)
	r.Post("/albums/{id}/photos",                 s.AddPhoto)
	r.Patch("/albums/{id}/photos/{photoID}",      s.UpdatePhoto)
	r.Delete("/albums/{id}/photos/{photoID}",     s.DeletePhoto)
	r.Get("/users/{username}/albums",             s.ListUserAlbums)
}

// ── Types ─────────────────────────────────────────────────────────────────────

type Album struct {
	ID          string  `json:"id"`
	OwnerID     string  `json:"owner_id"`
	OwnerName   string  `json:"owner_username"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	CoverURL    string  `json:"cover_url"`
	Visibility  string  `json:"visibility"`
	PhotoCount  int     `json:"photo_count"`
	CreatedAt   string  `json:"created_at"`
	Photos      []Photo `json:"photos,omitempty"`
}

type Photo struct {
	ID         string `json:"id"`
	AlbumID    string `json:"album_id"`
	URL        string `json:"url"`
	Caption    string `json:"caption"`
	Position   int    `json:"position"`
	CreatedAt  string `json:"created_at"`
}

// ── Visibility helper ─────────────────────────────────────────────────────────

// canView returns true if viewerID may see this album.
func (s *Service) canView(albumID, viewerID string) (bool, Album) {
	var a Album
	err := s.db.QueryRow(`
		SELECT a.id, a.owner_id, u.username, a.title, a.description,
		       a.cover_url, a.visibility, a.photo_count, a.created_at
		FROM albums a
		JOIN users u ON u.id = a.owner_id
		WHERE a.id = $1
	`, albumID).Scan(&a.ID, &a.OwnerID, &a.OwnerName, &a.Title, &a.Description,
		&a.CoverURL, &a.Visibility, &a.PhotoCount, &a.CreatedAt)
	if err != nil {
		return false, a
	}
	if a.OwnerID == viewerID {
		return true, a
	}
	switch a.Visibility {
	case "public":
		return true, a
	case "friends":
		var isFriend bool
		s.db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM friendships
				WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
				AND status = 'accepted'
			)
		`, viewerID, a.OwnerID).Scan(&isFriend)
		return isFriend, a
	default:
		return false, a
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *Service) ListAlbums(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	limit := 20
	offset := 0
	if p, _ := strconv.Atoi(r.URL.Query().Get("page")); p > 0 {
		offset = p * limit
	}

	rows, err := s.db.Query(`
		SELECT a.id, a.owner_id, u.username, a.title, a.description,
		       a.cover_url, a.visibility, a.photo_count, a.created_at
		FROM albums a
		JOIN users u ON u.id = a.owner_id
		WHERE a.owner_id = $1
		   OR (a.visibility = 'public')
		   OR (a.visibility = 'friends' AND EXISTS(
		         SELECT 1 FROM friendships f
		         WHERE ((f.requester_id = $1 AND f.addressee_id = a.owner_id)
		             OR (f.addressee_id = $1 AND f.requester_id = a.owner_id))
		         AND f.status = 'accepted'
		       ))
		ORDER BY a.created_at DESC
		LIMIT $2 OFFSET $3
	`, viewerID, limit, offset)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	var albums []Album
	for rows.Next() {
		var a Album
		rows.Scan(&a.ID, &a.OwnerID, &a.OwnerName, &a.Title, &a.Description,
			&a.CoverURL, &a.Visibility, &a.PhotoCount, &a.CreatedAt)
		albums = append(albums, a)
	}
	if albums == nil { albums = []Album{} }
	writeJSON(w, 200, map[string]any{"albums": albums})
}

func (s *Service) ListUserAlbums(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	username := chi.URLParam(r, "username")

	var ownerID string
	s.db.QueryRow(`SELECT id FROM users WHERE username = $1 AND deletion_scheduled_at IS NULL`, username).Scan(&ownerID)
	if ownerID == "" {
		writeError(w, 404, "user not found"); return
	}

	isSelf := ownerID == viewerID
	var isFriend bool
	if !isSelf {
		s.db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM friendships
				WHERE ((requester_id = $1 AND addressee_id = $2) OR (requester_id = $2 AND addressee_id = $1))
				AND status = 'accepted'
			)
		`, viewerID, ownerID).Scan(&isFriend)
	}

	var visFilter string
	if isSelf {
		visFilter = ""
	} else if isFriend {
		visFilter = "AND a.visibility IN ('public','friends')"
	} else {
		visFilter = "AND a.visibility = 'public'"
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT a.id, a.owner_id, u.username, a.title, a.description,
		       a.cover_url, a.visibility, a.photo_count, a.created_at
		FROM albums a
		JOIN users u ON u.id = a.owner_id
		WHERE a.owner_id = $1 %s
		ORDER BY a.created_at DESC
		LIMIT 50
	`, visFilter), ownerID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	var albums []Album
	for rows.Next() {
		var a Album
		rows.Scan(&a.ID, &a.OwnerID, &a.OwnerName, &a.Title, &a.Description,
			&a.CoverURL, &a.Visibility, &a.PhotoCount, &a.CreatedAt)
		albums = append(albums, a)
	}
	if albums == nil { albums = []Album{} }
	writeJSON(w, 200, map[string]any{"albums": albums})
}

func (s *Service) GetAlbum(w http.ResponseWriter, r *http.Request) {
	viewerID := auth.UserIDFromCtx(r.Context())
	albumID := chi.URLParam(r, "id")

	ok, album := s.canView(albumID, viewerID)
	if album.ID == "" {
		writeError(w, 404, "album not found"); return
	}
	if !ok {
		writeError(w, 403, "forbidden"); return
	}

	rows, err := s.db.Query(`
		SELECT id, album_id, url, caption, position, created_at
		FROM album_photos
		WHERE album_id = $1
		ORDER BY position ASC, created_at ASC
	`, albumID)
	if err != nil {
		writeError(w, 500, "db error"); return
	}
	defer rows.Close()

	for rows.Next() {
		var p Photo
		rows.Scan(&p.ID, &p.AlbumID, &p.URL, &p.Caption, &p.Position, &p.CreatedAt)
		album.Photos = append(album.Photos, p)
	}
	if album.Photos == nil { album.Photos = []Photo{} }
	writeJSON(w, 200, map[string]any{"album": album})
}

func (s *Service) CreateAlbum(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeError(w, 400, "title required"); return
	}
	if req.Visibility != "public" && req.Visibility != "private" {
		req.Visibility = "friends"
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO albums (owner_id, title, description, visibility)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, ownerID, strings.TrimSpace(req.Title), req.Description, req.Visibility).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not create album"); return
	}
	writeJSON(w, 201, map[string]string{"id": id})
}

func (s *Service) UpdateAlbum(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	albumID := chi.URLParam(r, "id")

	var currentOwner string
	s.db.QueryRow(`SELECT owner_id FROM albums WHERE id = $1`, albumID).Scan(&currentOwner)
	if currentOwner == "" { writeError(w, 404, "album not found"); return }
	if currentOwner != ownerID { writeError(w, 403, "forbidden"); return }

	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Visibility  *string `json:"visibility"`
		CoverURL    *string `json:"cover_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}

	var sets []string
	var args []any
	i := 1
	add := func(col string, val any) { sets = append(sets, fmt.Sprintf("%s = $%d", col, i)); args = append(args, val); i++ }
	if req.Title != nil       { add("title", strings.TrimSpace(*req.Title)) }
	if req.Description != nil { add("description", *req.Description) }
	if req.CoverURL != nil    { add("cover_url", *req.CoverURL) }
	if req.Visibility != nil {
		v := *req.Visibility
		if v != "public" && v != "private" { v = "friends" }
		add("visibility", v)
	}
	if len(sets) == 0 { writeJSON(w, 200, map[string]string{"message": "nothing to update"}); return }
	sets = append(sets, "updated_at = NOW()")
	args = append(args, albumID)
	s.db.Exec(fmt.Sprintf("UPDATE albums SET %s WHERE id = $%d", strings.Join(sets, ", "), i), args...)
	writeJSON(w, 200, map[string]string{"message": "updated"})
}

func (s *Service) DeleteAlbum(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	albumID := chi.URLParam(r, "id")

	var currentOwner string
	s.db.QueryRow(`SELECT owner_id FROM albums WHERE id = $1`, albumID).Scan(&currentOwner)
	if currentOwner == "" { writeError(w, 404, "album not found"); return }
	if currentOwner != ownerID { writeError(w, 403, "forbidden"); return }

	s.db.Exec(`DELETE FROM albums WHERE id = $1`, albumID)
	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

func (s *Service) AddPhoto(w http.ResponseWriter, r *http.Request) {
	uploaderID := auth.UserIDFromCtx(r.Context())
	albumID := chi.URLParam(r, "id")

	var ownerID string
	var photoCount int
	s.db.QueryRow(`SELECT owner_id, photo_count FROM albums WHERE id = $1`, albumID).Scan(&ownerID, &photoCount)
	if ownerID == "" { writeError(w, 404, "album not found"); return }
	if ownerID != uploaderID { writeError(w, 403, "only the album owner can add photos"); return }

	// Handle both multipart upload and JSON url
	caption := r.FormValue("caption")
	var photoURL string

	if r.Header.Get("Content-Type") != "" && strings.Contains(r.Header.Get("Content-Type"), "multipart") {
		url, err := s.media.SaveUpload(r, "albums", "")
		if err != nil {
			writeError(w, 400, err.Error()); return
		}
		photoURL = url
	} else {
		var req struct {
			URL     string `json:"url"`
			Caption string `json:"caption"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
			writeError(w, 400, "url required"); return
		}
		photoURL = req.URL
		caption = req.Caption
	}

	var id string
	err := s.db.QueryRow(`
		INSERT INTO album_photos (album_id, uploader_id, url, caption, position)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, albumID, uploaderID, photoURL, caption, photoCount+1).Scan(&id)
	if err != nil {
		writeError(w, 500, "could not add photo"); return
	}

	// Update cover if this is the first photo
	if photoCount == 0 {
		s.db.Exec(`UPDATE albums SET cover_url = $1, photo_count = photo_count + 1, updated_at = NOW() WHERE id = $2`, photoURL, albumID)
	} else {
		s.db.Exec(`UPDATE albums SET photo_count = photo_count + 1, updated_at = NOW() WHERE id = $1`, albumID)
	}

	writeJSON(w, 201, map[string]string{"id": id, "url": photoURL})
}

func (s *Service) UpdatePhoto(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	albumID := chi.URLParam(r, "id")
	photoID := chi.URLParam(r, "photoID")

	var albumOwner string
	s.db.QueryRow(`SELECT owner_id FROM albums WHERE id = $1`, albumID).Scan(&albumOwner)
	if albumOwner != ownerID { writeError(w, 403, "forbidden"); return }

	var req struct {
		Caption  *string `json:"caption"`
		Position *int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json"); return
	}
	if req.Caption != nil {
		s.db.Exec(`UPDATE album_photos SET caption = $1 WHERE id = $2 AND album_id = $3`, *req.Caption, photoID, albumID)
	}
	if req.Position != nil {
		s.db.Exec(`UPDATE album_photos SET position = $1 WHERE id = $2 AND album_id = $3`, *req.Position, photoID, albumID)
	}
	writeJSON(w, 200, map[string]string{"message": "updated"})
}

func (s *Service) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	ownerID := auth.UserIDFromCtx(r.Context())
	albumID := chi.URLParam(r, "id")
	photoID := chi.URLParam(r, "photoID")

	var albumOwner string
	s.db.QueryRow(`SELECT owner_id FROM albums WHERE id = $1`, albumID).Scan(&albumOwner)
	if albumOwner != ownerID { writeError(w, 403, "forbidden"); return }

	var photoURL string
	s.db.QueryRow(`SELECT url FROM album_photos WHERE id = $1 AND album_id = $2`, photoID, albumID).Scan(&photoURL)
	if photoURL == "" { writeError(w, 404, "photo not found"); return }

	s.db.Exec(`DELETE FROM album_photos WHERE id = $1`, photoID)
	s.db.Exec(`UPDATE albums SET photo_count = GREATEST(photo_count - 1, 0), updated_at = NOW() WHERE id = $1`, albumID)

	// If deleted photo was the cover, pick a new one
	var newCover string
	s.db.QueryRow(`SELECT url FROM album_photos WHERE album_id = $1 ORDER BY position ASC, created_at ASC LIMIT 1`, albumID).Scan(&newCover)
	s.db.Exec(`UPDATE albums SET cover_url = $1 WHERE id = $2`, newCover, albumID)

	writeJSON(w, 200, map[string]string{"message": "deleted"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── Timeline Photos (default album) ──────────────────────────────────────────

// AddToTimelineAlbum adds a photo URL to the user's "Timeline Photos" album,
// creating that album first if it doesn't exist. Called by the feed service
// whenever a post with an image is created.
func (s *Service) AddToTimelineAlbum(ownerID, photoURL string) {
	// Find or create the Timeline Photos album
	var albumID string
	s.db.QueryRow(`
		SELECT id FROM albums WHERE owner_id = $1 AND title = 'Timeline Photos'
	`, ownerID).Scan(&albumID)

	if albumID == "" {
		s.db.QueryRow(`
			INSERT INTO albums (owner_id, title, description, visibility, cover_url)
			VALUES ($1, 'Timeline Photos', 'Photos posted to your timeline.', 'friends', $2)
			RETURNING id
		`, ownerID, photoURL).Scan(&albumID)
		if albumID == "" {
			return
		}
		// First photo — photo_count already 0, cover set in insert
		s.db.Exec(`
			INSERT INTO album_photos (album_id, uploader_id, url, position)
			VALUES ($1, $2, $3, 1)
		`, albumID, ownerID, photoURL)
		s.db.Exec(`UPDATE albums SET photo_count = 1 WHERE id = $1`, albumID)
		return
	}

	// Add to existing album
	var photoCount int
	s.db.QueryRow(`SELECT photo_count FROM albums WHERE id = $1`, albumID).Scan(&photoCount)
	s.db.Exec(`
		INSERT INTO album_photos (album_id, uploader_id, url, position)
		VALUES ($1, $2, $3, $4)
	`, albumID, ownerID, photoURL, photoCount+1)
	s.db.Exec(`UPDATE albums SET photo_count = photo_count + 1, updated_at = NOW() WHERE id = $1`, albumID)
}
