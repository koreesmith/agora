package feed

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/ctxkeys"
	"github.com/agora-social/agora/internal/media"
)

// AGORA-444: when a moderator or admin removes someone else's video, the file
// is gone from disk and not just hidden. The author deleting their own post is
// unchanged, and anything that isn't this instance's own upload is left alone.
func TestModeratorTakedownRemovesVideoFiles(t *testing.T) {
	db := openVideoMetaDB(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "videos"), 0755)
	s := &Service{db: db, media: media.NewService(db, dir)}

	author := mkVideoMetaUser(t, db, "td_author")
	mod := mkVideoMetaUser(t, db, "td_mod")

	mkFiles := func(name string) (video, thumb string) {
		video, thumb = "/uploads/videos/"+name+".mp4", "/uploads/videos/"+name+"_thumb.jpg"
		os.WriteFile(filepath.Join(dir, "videos", name+".mp4"), []byte("video"), 0644)
		os.WriteFile(filepath.Join(dir, "videos", name+"_thumb.jpg"), []byte("thumb"), 0644)
		return
	}
	exists := func(rel string) bool { _, err := os.Stat(filepath.Join(dir, rel)); return err == nil }
	post := func(video, thumb string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility, video_url, video_thumb_url, video_bytes)
			VALUES ($1, 'clip', 'public', $2, $3, 100) RETURNING id`, author, video, thumb).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}
	del := func(userID, role, postID string) int {
		req := httptest.NewRequest("DELETE", "/posts/"+postID, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", postID)
		ctx := context.WithValue(req.Context(), ctxkeys.UserID, userID)
		ctx = context.WithValue(ctx, ctxkeys.UserRole, role)
		req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		s.DeletePost(w, req)
		return w.Code
	}
	auditCount := func(postID string) int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'takedown_video' AND details LIKE $1`, "post "+postID+"%").Scan(&n)
		return n
	}

	// moderator takes down someone else's video
	v1, t1 := mkFiles(fmt.Sprintf("td1_%d", time.Now().UnixNano()))
	p1 := post(v1, t1)
	t.Cleanup(func() { db.Exec(`DELETE FROM audit_log WHERE details LIKE $1`, "post "+p1+"%") })
	if code := del(mod, "moderator", p1); code != 200 {
		t.Fatalf("moderator delete = %d", code)
	}
	if exists(v1[len("/uploads/"):]) || exists(t1[len("/uploads/"):]) {
		t.Error("the video or its thumbnail is still on disk after a takedown")
	}
	var deletedAt *time.Time
	var bytes int64
	db.QueryRow(`SELECT deleted_at, video_bytes FROM posts WHERE id = $1`, p1).Scan(&deletedAt, &bytes)
	if deletedAt == nil || bytes != 0 {
		t.Errorf("the post should be deleted with no storage counted: deleted_at=%v bytes=%d", deletedAt, bytes)
	}
	if auditCount(p1) != 1 {
		t.Errorf("the takedown should be in the audit log once, found %d", auditCount(p1))
	}

	// an admin does the same
	v2, t2 := mkFiles(fmt.Sprintf("td2_%d", time.Now().UnixNano()))
	p2 := post(v2, t2)
	t.Cleanup(func() { db.Exec(`DELETE FROM audit_log WHERE details LIKE $1`, "post "+p2+"%") })
	del(mod, "admin", p2)
	if exists(v2[len("/uploads/"):]) {
		t.Error("an admin's takedown left the video on disk")
	}

	// the author deleting their own post is unchanged: the files stay
	v3, t3 := mkFiles(fmt.Sprintf("td3_%d", time.Now().UnixNano()))
	p3 := post(v3, t3)
	if code := del(author, "user", p3); code != 200 {
		t.Fatalf("author delete = %d", code)
	}
	if !exists(v3[len("/uploads/"):]) || !exists(t3[len("/uploads/"):]) {
		t.Error("the author deleting their own video removed its files")
	}
	if auditCount(p3) != 0 {
		t.Error("an author's own delete is not a takedown")
	}

	// a moderator removing a remote video touches no file and doesn't fail
	p4 := post("https://elsewhere.example/v.mp4", "")
	if code := del(mod, "moderator", p4); code != 200 {
		t.Errorf("deleting a remote video = %d", code)
	}
	if auditCount(p4) != 0 {
		t.Error("a remote video has no local file, so there is nothing to record")
	}

	// a stranger can't delete it at all
	p5 := post("/uploads/videos/never.mp4", "")
	if code := del(mod, "user", p5); code != 403 {
		t.Errorf("a regular user deleting someone else's post = %d, want 403", code)
	}
}
