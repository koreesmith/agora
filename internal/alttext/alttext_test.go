package alttext

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClean(t *testing.T) {
	got := Clean([]string{"  a dog  ", "", "extra"}, 2)
	if len(got) != 2 || got[0] != "a dog" || got[1] != "" {
		t.Errorf("Clean trims and drops extras: got %q", got)
	}

	got = Clean(nil, 3)
	if len(got) != 3 || got[0] != "" || got[2] != "" {
		t.Errorf("Clean pads to n: got %q", got)
	}

	if got := Clean([]string{"x"}, 0); len(got) != 0 {
		t.Errorf("Clean with n=0: got %q", got)
	}

	// Capped by characters, not bytes: a multi-byte description is cut on a
	// rune boundary and stays valid UTF-8.
	long := strings.Repeat("é", MaxLen+50)
	got = Clean([]string{long}, 1)
	if n := utf8.RuneCountInString(got[0]); n != MaxLen {
		t.Errorf("capped to %d characters, want %d", n, MaxLen)
	}
	if !utf8.ValidString(got[0]) {
		t.Error("cut description is not valid UTF-8")
	}
}
