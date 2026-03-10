package search

import (
	"net/http"

	"github.com/agora-social/agora/pkg/utils"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

type Service struct {
	db *sqlx.DB
}

func NewService(db *sqlx.DB) *Service {
	return &Service{db: db}
}

func RegisterRoutes(r chi.Router, svc *Service) {
	r.Get("/search/users", svc.SearchUsers)
}

func (s *Service) SearchUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		utils.Error(w, http.StatusBadRequest, "query required")
		return
	}

	var users []struct {
		ID          string `db:"id" json:"id"`
		Username    string `db:"username" json:"username"`
		DisplayName string `db:"display_name" json:"display_name"`
		AvatarURL   string `db:"avatar_url" json:"avatar_url"`
		IsRemote    bool   `db:"is_remote" json:"is_remote"`
		HomeInstance string `db:"home_instance" json:"home_instance,omitempty"`
	}

	s.db.Select(&users, `
		SELECT id, username, display_name, avatar_url, is_remote, home_instance
		FROM users
		WHERE is_suspended = false
		AND (
			username ILIKE $1
			OR display_name ILIKE $1
			OR username % $2
			OR display_name % $2
		)
		ORDER BY
			CASE WHEN username ILIKE $1 THEN 0 ELSE 1 END,
			similarity(username, $2) DESC
		LIMIT 20
	`, "%"+q+"%", q)

	if users == nil {
		users = []struct {
			ID          string `db:"id" json:"id"`
			Username    string `db:"username" json:"username"`
			DisplayName string `db:"display_name" json:"display_name"`
			AvatarURL   string `db:"avatar_url" json:"avatar_url"`
			IsRemote    bool   `db:"is_remote" json:"is_remote"`
			HomeInstance string `db:"home_instance" json:"home_instance,omitempty"`
		}{}
	}

	utils.JSON(w, http.StatusOK, users)
}
