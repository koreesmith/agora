package users

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/ctxkeys"
)

// AGORA-405: scheduling a deletion reports the date under both names (mobile
// builds read deletion_scheduled_at), and cancelling clears it.
func TestRequestAndCancelDeletion(t *testing.T) {
	db := openOnboardingTestDB(t)
	s := &Service{db: db}
	id := insertOnboardingUser(t, db, "agora405del", false)
	ctx := context.WithValue(context.Background(), ctxkeys.UserID, id)

	w := httptest.NewRecorder()
	s.RequestDeletion(w, httptest.NewRequest("POST", "/users/me/request-deletion", nil).WithContext(ctx))
	if w.Code != 200 {
		t.Fatalf("request status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	when, ok := body["deletion_scheduled_at"].(string)
	if !ok || when == "" {
		t.Fatalf("deletion_scheduled_at = %v, want a timestamp", body["deletion_scheduled_at"])
	}
	if body["scheduled_at"] != when {
		t.Errorf("scheduled_at = %v, want it to match deletion_scheduled_at %q", body["scheduled_at"], when)
	}
	if _, err := time.Parse(time.RFC3339Nano, when); err != nil {
		t.Errorf("deletion_scheduled_at %q does not parse: %v", when, err)
	}

	var pending bool
	db.QueryRow(`SELECT deletion_scheduled_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&pending)
	if !pending {
		t.Fatalf("deletion_scheduled_at not stored after request")
	}

	w = httptest.NewRecorder()
	s.CancelDeletion(w, httptest.NewRequest("DELETE", "/users/me/request-deletion", nil).WithContext(ctx))
	if w.Code != 200 {
		t.Fatalf("cancel status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	db.QueryRow(`SELECT deletion_scheduled_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&pending)
	if pending {
		t.Errorf("deletion_scheduled_at still set after cancel")
	}
}
