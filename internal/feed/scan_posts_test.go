package feed

import (
	"errors"
	"testing"
)

// fakeRows is a minimal stand-in for the *sql.Rows-shaped interface
// scanPosts accepts, letting a Scan error be injected without a real DB.
type fakeRows struct {
	n         int
	errOnCall map[int]error
	calls     int
}

func (f *fakeRows) Next() bool { return f.calls < f.n }

func (f *fakeRows) Scan(dest ...any) error {
	i := f.calls
	f.calls++
	return f.errOnCall[i]
}

// AGORA-355: scanPosts used to discard rows.Scan's error entirely, which
// meant a column-list drift between a query and this function's fixed
// Scan call list would silently append a mostly-zero-valued Post instead of
// failing anywhere visible. A row whose Scan fails must now be skipped, not
// appended half-populated, and it must not prevent later rows in the same
// result set from scanning correctly.
func TestScanPostsSkipsRowOnScanError(t *testing.T) {
	rows := &fakeRows{
		n: 3,
		errOnCall: map[int]error{
			1: errors.New("simulated column mismatch"),
		},
	}

	posts := scanPosts(rows)

	if len(posts) != 2 {
		t.Errorf("scanPosts returned %d posts, want 2 (3 rows scanned, 1 failed and should be skipped)", len(posts))
	}
}

func TestScanPostsAllSucceed(t *testing.T) {
	rows := &fakeRows{n: 5}

	posts := scanPosts(rows)

	if len(posts) != 5 {
		t.Errorf("scanPosts returned %d posts, want 5", len(posts))
	}
}

func TestScanPostsEmptyReturnsEmptySliceNotNil(t *testing.T) {
	rows := &fakeRows{n: 0}

	posts := scanPosts(rows)

	if posts == nil {
		t.Error("scanPosts returned nil for zero rows, want an empty (non-nil) slice")
	}
	if len(posts) != 0 {
		t.Errorf("scanPosts returned %d posts for zero rows, want 0", len(posts))
	}
}
