package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/agora-social/agora/internal/ctxkeys"
)

type draftEnv struct{ *pinEnv }

func newDraftEnv(t *testing.T) *draftEnv { return &draftEnv{newPinEnv(t)} }

// call runs a draft handler as userID with a JSON body.
func (e *draftEnv) call(userID, method, path, body string, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rctx := chi.NewRouteContext()
	if id != "" {
		rctx.URLParams.Add("id", id)
	}
	req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxkeys.UserID, userID), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	switch method {
	case "GET":
		e.s.ListDrafts(w, req)
	case "POST":
		e.s.CreateDraft(w, req)
	case "PUT":
		e.s.UpdateDraft(w, req)
	case "DELETE":
		e.s.DeleteDraft(w, req)
	}
	return w
}

type draftResp struct {
	ID      string         `json:"id"`
	Content string         `json:"content"`
	Data    map[string]any `json:"data"`
}

func (e *draftEnv) create(userID, body string) (int, draftResp) {
	w := e.call(userID, "POST", "/drafts", body, "")
	var d draftResp
	json.Unmarshal(w.Body.Bytes(), &d)
	if d.ID != "" {
		e.t.Cleanup(func() { e.db.Exec(`DELETE FROM post_drafts WHERE id = $1`, d.ID) })
	}
	return w.Code, d
}

func (e *draftEnv) list(userID string) []draftResp {
	w := e.call(userID, "GET", "/drafts", "", "")
	var out struct {
		Drafts []draftResp `json:"drafts"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	return out.Drafts
}

// A draft round trips what the composer sent, newest change first, and only its
// owner can see, change or remove it.
func TestDraftsRoundTripAndOwnership(t *testing.T) {
	e := newDraftEnv(t)
	me, _ := e.user("dr_me")
	other, _ := e.user("dr_other")

	code, a := e.create(me, `{"content":"first","data":{"visibility":"friends","content_warning":"spoilers","poll_options":["a","b"],"federate_atproto":false}}`)
	if code != 201 || a.ID == "" {
		t.Fatalf("create = %d %+v", code, a)
	}
	if a.Data["visibility"] != "friends" || a.Data["content_warning"] != "spoilers" || a.Data["federate_atproto"] != false {
		t.Errorf("the composer's fields didn't come back as sent: %+v", a.Data)
	}
	time.Sleep(15 * time.Millisecond)
	_, b := e.create(me, `{"content":"second","data":{}}`)

	got := e.list(me)
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("list should be most recently changed first: %+v", got)
	}

	// editing a draft moves it to the top and replaces its contents
	time.Sleep(15 * time.Millisecond)
	w := e.call(me, "PUT", "/drafts/"+a.ID, `{"content":"first, edited","data":{"visibility":"public"}}`, a.ID)
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	got = e.list(me)
	if got[0].ID != a.ID || got[0].Content != "first, edited" || got[0].Data["visibility"] != "public" || got[0].Data["content_warning"] != nil {
		t.Errorf("after an edit: %+v", got[0])
	}

	// someone else can't see, change or remove it
	if len(e.list(other)) != 0 {
		t.Error("another account sees my drafts")
	}
	if w := e.call(other, "PUT", "/drafts/"+a.ID, `{"content":"hijack","data":{}}`, a.ID); w.Code != 404 {
		t.Errorf("updating someone else's draft = %d, want 404", w.Code)
	}
	e.call(other, "DELETE", "/drafts/"+a.ID, "", a.ID)
	if len(e.list(me)) != 2 {
		t.Error("another account deleted my draft")
	}

	// the owner removes it; doing it again is fine
	e.call(me, "DELETE", "/drafts/"+a.ID, "", a.ID)
	if w := e.call(me, "DELETE", "/drafts/"+a.ID, "", a.ID); w.Code != 200 {
		t.Errorf("deleting twice = %d, want 200", w.Code)
	}
	if got := e.list(me); len(got) != 1 || got[0].ID != b.ID {
		t.Errorf("after deleting: %+v", got)
	}
}

func TestDraftsRefuseWhatIsNotWorthSaving(t *testing.T) {
	e := newDraftEnv(t)
	me, _ := e.user("dr_bad")
	cases := map[string]struct {
		body string
		want int
	}{
		"nothing at all":                    {`{"content":"","data":{}}`, 400},
		"only spaces":                       {`{"content":"   \n","data":{}}`, 400},
		"options that are all blank":        {`{"content":"","data":{"poll_options":["","  "]}}`, 400},
		"data that is not an object":        {`{"content":"hi","data":[1,2]}`, 400},
		"not json":                          {`nope`, 400},
		"text only is enough":               {`{"content":"hi"}`, 201},
		"a poll alone is enough":            {`{"content":"","data":{"poll_options":["yes","no"]}}`, 201},
		"an uploaded photo alone is enough": {`{"content":"","data":{"image_urls":["/uploads/posts/a.jpg"]}}`, 201},
		"a video alone is enough":           {`{"content":"","data":{"video_url":"/uploads/videos/a.mp4"}}`, 201},
		"too long":                          {fmt.Sprintf(`{"content":%q}`, strings.Repeat("x", maxDraftChars+1)), 413},
	}
	for name, c := range cases {
		w := e.call(me, "POST", "/drafts", c.body, "")
		var d draftResp
		json.Unmarshal(w.Body.Bytes(), &d)
		if d.ID != "" {
			e.t.Cleanup(func() { e.db.Exec(`DELETE FROM post_drafts WHERE id = $1`, d.ID) })
		}
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d (%s)", name, w.Code, c.want, w.Body.String())
		}
	}
}

// The uploaded files a draft points to are recorded, so cleaning up unused media
// leaves them alone, and only this instance's own uploads count.
func TestDraftRecordsItsMedia(t *testing.T) {
	e := newDraftEnv(t)
	me, _ := e.user("dr_media")
	_, d := e.create(me, `{"content":"look","data":{"image_urls":["/uploads/posts/a.jpg","https://elsewhere.example/b.jpg"],"video_url":"/uploads/videos/v.mp4","video_thumb_url":"/uploads/videos/v_thumb.jpg"}}`)
	var urls string
	e.db.QueryRow(`SELECT array_to_string(media_urls, ',') FROM post_drafts WHERE id = $1`, d.ID).Scan(&urls)
	if urls != "/uploads/posts/a.jpg,/uploads/videos/v.mp4,/uploads/videos/v_thumb.jpg" {
		t.Errorf("media recorded as %q", urls)
	}
	// changing the draft changes what it holds
	e.call(me, "PUT", "/drafts/"+d.ID, `{"content":"look","data":{}}`, d.ID)
	e.db.QueryRow(`SELECT array_to_string(media_urls, ',') FROM post_drafts WHERE id = $1`, d.ID).Scan(&urls)
	if urls != "" {
		t.Errorf("after removing the media the draft still holds %q", urls)
	}
}

func TestDraftLimit(t *testing.T) {
	e := newDraftEnv(t)
	me, _ := e.user("dr_limit")
	for i := 0; i < MaxDrafts; i++ {
		if code, _ := e.create(me, fmt.Sprintf(`{"content":"draft %d"}`, i)); code != 201 {
			t.Fatalf("draft %d = %d", i+1, code)
		}
	}
	w := e.call(me, "POST", "/drafts", `{"content":"one too many"}`, "")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "25") {
		t.Errorf("past the limit = %d %s", w.Code, w.Body.String())
	}
	// deleting one makes room
	e.call(me, "DELETE", "/drafts/"+e.list(me)[0].ID, "", e.list(me)[0].ID)
	if code, _ := e.create(me, `{"content":"room again"}`); code != 201 {
		t.Errorf("after deleting one = %d", code)
	}
}

// Untouched drafts are not shown and are removed by the daily cleanup; recent
// ones are left alone.
func TestDraftExpiry(t *testing.T) {
	e := newDraftEnv(t)
	me, _ := e.user("dr_old")
	_, old := e.create(me, `{"content":"abandoned"}`)
	_, fresh := e.create(me, `{"content":"recent"}`)
	e.db.Exec(`UPDATE post_drafts SET updated_at = NOW() - INTERVAL '91 days' WHERE id = $1`, old.ID)

	if got := e.list(me); len(got) != 1 || got[0].ID != fresh.ID {
		t.Errorf("an expired draft is still listed: %+v", got)
	}
	e.s.cleanupDrafts()
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM post_drafts WHERE id = $1`, old.ID).Scan(&n)
	if n != 0 {
		t.Error("the cleanup left an expired draft")
	}
	e.db.QueryRow(`SELECT COUNT(*) FROM post_drafts WHERE id = $1`, fresh.ID).Scan(&n)
	if n != 1 {
		t.Error("the cleanup removed a recent draft")
	}
}

// Posting a draft removes it, in the same request, and only the poster's own.
func TestPostingFromADraftRemovesIt(t *testing.T) {
	e := newDraftEnv(t)
	me, _ := e.user("dr_post")
	other, _ := e.user("dr_post_other")
	_, mine := e.create(me, `{"content":"to post"}`)
	_, theirs := e.create(other, `{"content":"not yours"}`)

	post := func(draftID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/posts", strings.NewReader(fmt.Sprintf(`{"content":"posted","visibility":"friends","draft_id":%q}`, draftID)))
		req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserID, me))
		w := httptest.NewRecorder()
		e.s.CreatePost(w, req)
		return w
	}
	w := post(mine.ID)
	if w.Code != 201 {
		t.Fatalf("post = %d %s", w.Code, w.Body.String())
	}
	var made struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &made)
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM posts WHERE id = $1`, made.ID) })
	if len(e.list(me)) != 0 {
		t.Error("the draft survived being posted")
	}

	// naming someone else's draft posts normally and leaves theirs alone
	w = post(theirs.ID)
	json.Unmarshal(w.Body.Bytes(), &made)
	e.t.Cleanup(func() { e.db.Exec(`DELETE FROM posts WHERE id = $1`, made.ID) })
	if len(e.list(other)) != 1 {
		t.Error("posting with someone else's draft id removed their draft")
	}
	// a malformed id doesn't break posting
	if w := post("not-a-uuid"); w.Code != 201 {
		t.Errorf("a bad draft id broke the post: %d", w.Code)
	} else {
		json.Unmarshal(w.Body.Bytes(), &made)
		e.t.Cleanup(func() { e.db.Exec(`DELETE FROM posts WHERE id = $1`, made.ID) })
	}
}
