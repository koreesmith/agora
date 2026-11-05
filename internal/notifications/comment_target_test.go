package notifications

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/ctxkeys"
)

// AGORA-460: a notification about a comment records the comment, so a client
// can open the post at it.

type listedNotif struct {
	Type      string  `json:"type"`
	PostID    *string `json:"post_id"`
	CommentID *string `json:"comment_id"`
	Grouped   bool    `json:"grouped"`
	Count     int     `json:"count"`
}

func listFor(t *testing.T, s *Service, userID string) []listedNotif {
	t.Helper()
	ctx := context.WithValue(context.Background(), ctxkeys.UserID, userID)
	w := httptest.NewRecorder()
	s.List(w, httptest.NewRequest("GET", "/notifications", nil).WithContext(ctx))
	var body struct {
		Notifications []listedNotif `json:"notifications"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("list: %v: %s", err, w.Body.String())
	}
	return body.Notifications
}

func TestCommentNotificationsCarryTheComment(t *testing.T) {
	db := pushTestDB(t)
	s := &Service{db: db, email: &EmailService{db: db}}
	owner, replier, liker := pushTestUser(t, db, "own460"), pushTestUser(t, db, "rep460"), pushTestUser(t, db, "lik460")

	mkPost := func(author string, parent *string) string {
		var id string
		if err := db.QueryRow(`INSERT INTO posts (author_id, content, visibility, parent_id) VALUES ($1,'x','public',$2) RETURNING id`, author, parent).Scan(&id); err != nil {
			t.Fatalf("post: %v", err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, id) })
		return id
	}
	post := mkPost(owner, nil)
	c1 := mkPost(owner, &post)  // my comment
	c2 := mkPost(owner, &post)  // another comment of mine
	r1 := mkPost(replier, &c1)  // a reply to c1 (depth 2)
	r2 := mkPost(replier, &c1)  // another reply

	// A like on a comment is stored against the comment (post_id = the comment).
	s.CreateOnComment(owner, liker, "comment_like", c1, "", "")
	time.Sleep(5 * time.Millisecond)
	s.CreateOnComment(owner, liker, "comment_like", c2, "", "") // a different comment: its own line
	time.Sleep(5 * time.Millisecond)
	// A reaction on a comment keeps the root post as post_id and the comment as comment_id.
	s.CreateOnComment(owner, liker, "comment_reaction", post, c1, "love")
	time.Sleep(5 * time.Millisecond)
	s.CreateOnComment(owner, replier, "comment_reply", post, r1, "")
	time.Sleep(5 * time.Millisecond)
	s.CreateOnComment(owner, replier, "comment_reply", post, r2, "") // same type, same post: merges, opens the newest
	// A notification from before comment_id existed, for a reaction: no comment to open.
	db.Exec(`INSERT INTO notifications (user_id, actor_id, type, post_id, data) VALUES ($1,$2,'comment_reaction',$3,'like')`, owner, replier, post)

	got := listFor(t, s, owner)
	by := map[string][]listedNotif{}
	for _, n := range got {
		by[n.Type] = append(by[n.Type], n)
		if n.Type != "" && n.PostID != nil && *n.PostID != post && n.Type != "comment_like" {
			t.Errorf("%s points at %s, want the post %s", n.Type, *n.PostID, post)
		}
	}

	// likes on two different comments are two lines, each opening its own comment, on the root post
	likes := by["comment_like"]
	if len(likes) != 2 {
		t.Fatalf("comment_like lines = %d, want 2 (one per comment)", len(likes))
	}
	seen := map[string]bool{}
	for _, n := range likes {
		if n.CommentID == nil || n.PostID == nil || *n.PostID != post {
			t.Errorf("comment_like = %+v, want the root post and a comment", n)
			continue
		}
		seen[*n.CommentID] = true
	}
	if !seen[c1] || !seen[c2] {
		t.Errorf("comment_like comments = %v, want %s and %s", seen, c1, c2)
	}

	// replies merge, and open the newest
	replies := by["comment_reply"]
	if len(replies) != 1 || replies[0].Count != 2 || replies[0].CommentID == nil || *replies[0].CommentID != r2 {
		t.Errorf("comment_reply = %+v, want one merged line of 2 opening %s", replies, r2)
	}

	// the reaction with a comment, and the old one without
	var withComment, without int
	for _, n := range by["comment_reaction"] {
		if n.CommentID != nil && *n.CommentID == c1 {
			withComment++
		}
		if n.CommentID == nil {
			without++
		}
	}
	if withComment != 1 || without != 1 {
		t.Errorf("comment_reaction lines: %d opening the comment, %d without, want 1 and 1 (%+v)", withComment, without, by["comment_reaction"])
	}
}

// Deleting the comment leaves the notification, which then opens the post as before.
func TestDeletedCommentLeavesTheNotification(t *testing.T) {
	db := pushTestDB(t)
	s := &Service{db: db, email: &EmailService{db: db}}
	owner, other := pushTestUser(t, db, "own460b"), pushTestUser(t, db, "oth460b")
	var post, comment string
	db.QueryRow(`INSERT INTO posts (author_id, content, visibility) VALUES ($1,'x','public') RETURNING id`, owner).Scan(&post)
	db.QueryRow(`INSERT INTO posts (author_id, content, visibility, parent_id) VALUES ($1,'c','public',$2) RETURNING id`, other, post).Scan(&comment)
	t.Cleanup(func() { db.Exec(`DELETE FROM posts WHERE id = $1`, post) })
	s.CreateOnComment(owner, other, "post_comment", post, comment, "")
	db.Exec(`DELETE FROM posts WHERE id = $1`, comment)

	got := listFor(t, s, owner)
	if len(got) != 1 || got[0].PostID == nil || *got[0].PostID != post || got[0].CommentID != nil {
		t.Fatalf("got %+v, want the post with no comment", got)
	}
}
