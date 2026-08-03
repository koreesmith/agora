package store

import "testing"

// The legacy half of this mapping exists purely so that mobile builds predating
// AGORA-305 keep working, and those builds cannot be forced to update. That
// makes it exactly the kind of code that gets "tidied" away later by someone who
// sees an emoji in a Go map and assumes it is dead. The DM cases below are the
// ones that matter: an older client sends the glyph, and rejecting it would
// break message reactions outright rather than degrading them.
func TestNormalizeReaction(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  string
		valid bool
	}{
		{"canonical passes through", "like", "like", true},
		{"canonical newcomer", "sad", "sad", true},
		{"canonical newcomer", "dislike", "dislike", true},

		// Retired by AGORA-305; an old picker still offers it.
		{"retired vomit becomes dislike", "vomit", "dislike", true},

		// Pre-AGORA-305 DM clients send the raw glyph.
		{"dm glyph heart", "❤️", "love", true},
		{"dm glyph bare heart", "❤", "love", true},
		{"dm glyph joy", "😂", "laugh", true},
		{"dm glyph open mouth", "😮", "wow", true},
		{"dm glyph crying", "😢", "sad", true},
		{"dm glyph thumbs up", "👍", "like", true},
		{"dm glyph thumbs down", "👎", "dislike", true},
		{"dm glyph pouting (mobile only)", "😡", "angry", true},

		{"unknown rejected", "nonsense", "", false},
		{"empty rejected", "", "", false},
		{"unmapped glyph rejected", "🦄", "", false},
	}

	for _, tc := range cases {
		got, ok := NormalizeReaction(tc.in)
		if ok != tc.valid {
			t.Errorf("%s: NormalizeReaction(%q) valid = %v, want %v", tc.name, tc.in, ok, tc.valid)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: NormalizeReaction(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// Every value NormalizeReaction can return has to survive the CHECK constraints
// on reactions.reaction_type and message_reactions.reaction, which are written
// from ValidReactions. A legacy entry pointing at a type that no longer exists
// would turn a 400 into a write-time constraint violation, which is the failure
// mode the accept-list was added to prevent.
func TestLegacyReactionsResolveToValidTypes(t *testing.T) {
	for legacy, canonical := range legacyReactions {
		if !ValidReactions[canonical] {
			t.Errorf("legacyReactions[%q] = %q, which is not in ValidReactions", legacy, canonical)
		}
	}
}
