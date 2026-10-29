package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/mediacache"
)

// AGORA-380: admin controls for the remote media cache. Its settings live in
// instance_settings but are deliberately kept out of adminEditableSettings,
// so they're only ever written through UpdateRemoteMediaCache's validation.

func (s *Service) remoteMediaCache() *mediacache.Cache {
	// No HTTP client: the admin side only reads stats, prunes and clears.
	return mediacache.New(s.db, s.media.UploadDir(), nil)
}

func (s *Service) GetRemoteMediaCache(w http.ResponseWriter, r *http.Request) {
	c := s.remoteMediaCache()
	writeJSON(w, 200, map[string]any{
		"settings": c.Settings(),
		"stats":    c.Stats(),
	})
}

func (s *Service) UpdateRemoteMediaCache(w http.ResponseWriter, r *http.Request) {
	actorID := auth.UserIDFromCtx(r.Context())
	var req mediacache.Settings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid json")
		return
	}
	c := s.remoteMediaCache()
	if err := c.SaveSettings(req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.db.Exec(
		`INSERT INTO audit_log (actor_id, action, target_type, details) VALUES ($1, 'update_remote_media_cache', 'media', $2)`,
		actorID, fmt.Sprintf("enabled=%t max_mb=%d max_file_mb=%d", req.Enabled, req.MaxMB, req.MaxFileMB),
	)
	writeJSON(w, 200, map[string]any{
		"settings": c.Settings(),
		"stats":    c.Stats(),
	})
}

func (s *Service) PruneRemoteMediaCache(w http.ResponseWriter, r *http.Request) {
	s.runRemoteMediaCacheJob(w, r, "prune_remote_media_cache", s.remoteMediaCache().Prune)
}

func (s *Service) ClearRemoteMediaCache(w http.ResponseWriter, r *http.Request) {
	s.runRemoteMediaCacheJob(w, r, "clear_remote_media_cache", s.remoteMediaCache().Clear)
}

func (s *Service) runRemoteMediaCacheJob(w http.ResponseWriter, r *http.Request, action string, job func() (mediacache.PruneResult, error)) {
	actorID := auth.UserIDFromCtx(r.Context())
	res, err := job()
	if err != nil {
		writeError(w, 500, "could not update the cache")
		return
	}
	s.db.Exec(
		`INSERT INTO audit_log (actor_id, action, target_type, details) VALUES ($1, $2, 'media', $3)`,
		actorID, action, fmt.Sprintf("removed %d unused and %d other file(s), freed %d bytes", res.Orphans, res.Evicted, res.BytesFreed),
	)
	writeJSON(w, 200, map[string]any{
		"result": res,
		"stats":  s.remoteMediaCache().Stats(),
	})
}

// RefreshRemoteProfiles (AGORA-381) starts a background refresh of every
// remote profile people here see, instead of waiting for the hourly batches.
// Refreshing is also what re-caches avatars and covers, so this is the way to
// repopulate the cache straight after clearing it.
func (s *Service) RefreshRemoteProfiles(w http.ResponseWriter, r *http.Request) {
	if s.fed == nil {
		writeError(w, 503, "federation is not available")
		return
	}
	actorID := auth.UserIDFromCtx(r.Context())
	started := s.fed.RefreshAllRemoteProfiles()
	if started {
		s.db.Exec(
			`INSERT INTO audit_log (actor_id, action, target_type, details) VALUES ($1, 'refresh_remote_profiles', 'federation', 'started')`,
			actorID,
		)
	}
	writeJSON(w, 202, map[string]bool{"started": started})
}
