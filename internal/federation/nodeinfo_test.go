package federation

import "testing"

// AGORA-409: NodeInfo said every instance was open for signup, so instance
// directories listed invite-only, waitlist and closed instances as open.
func TestNodeInfoOpenRegistrations(t *testing.T) {
	cases := map[string]bool{
		"":         true, // unset: the register handler treats it as open
		"open":     true,
		"invite":   false,
		"waitlist": false,
		"closed":   false,
	}
	for mode, want := range cases {
		if got := nodeInfoOpenRegistrations(mode); got != want {
			t.Errorf("nodeInfoOpenRegistrations(%q) = %v, want %v", mode, got, want)
		}
	}
}
