package users

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/agora-social/agora/internal/auth"
	"github.com/agora-social/agora/internal/store"
)

// ── Push tokens (AGORA-420) ───────────────────────────────────────────────────
//
// A user used to have exactly one push token, users.expo_push_token, so the
// most recent device to register was the only one that ever got a push: a
// second phone, an iPad, or a reinstall silently took over, and the mobile
// app clearing the token when switching away from an account cleared it for
// every device that account was signed in on. push_tokens holds one row per
// device. The column is still written so a rollback has something to read.

// savePushToken records the token PATCH /users/me was given. A token names a
// device, so registering it moves it to this account from any other (the app
// only ever registers the active account). An empty token is the old "stop
// pushing to me" and clears every device, since it can't say which device it
// means; current apps remove one device with DELETE /users/me/push-token.
func savePushToken(db *store.DB, userID, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		db.Exec(`DELETE FROM push_tokens WHERE user_id = $1`, userID)
		return
	}
	db.Exec(`
		INSERT INTO push_tokens (token, user_id) VALUES ($1, $2)
		ON CONFLICT (token) DO UPDATE SET user_id = $2, last_seen_at = NOW()
	`, token, userID)
	db.Exec(`UPDATE users SET expo_push_token = '' WHERE expo_push_token = $1 AND id != $2`, token, userID)
}

// RemovePushToken stops pushes to one device: DELETE /users/me/push-token with
// {"token": ...}. Scoped to the caller, so it can't unregister someone else.
func (s *Service) RemovePushToken(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		writeError(w, 400, "token required")
		return
	}
	token := strings.TrimSpace(req.Token)
	s.db.Exec(`DELETE FROM push_tokens WHERE token = $1 AND user_id = $2`, token, userID)
	s.db.Exec(`UPDATE users SET expo_push_token = '' WHERE id = $1 AND expo_push_token = $2`, userID, token)
	writeJSON(w, 200, map[string]string{"message": "ok"})
}
