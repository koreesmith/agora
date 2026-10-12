package store

// Canonical reaction types (AGORA-305), shared by post/comment reactions and DM
// message reactions. Lives beside the schema because the CHECK constraints on
// both `reactions.reaction_type` and `message_reactions.reaction` enforce this
// exact list. Editing one without the other turns a 400 into a constraint
// violation at write time, so they're kept within sight of each other.
//
// Order is irrelevant here (the picker's ordering lives in the frontend's
// reactions.ts); this is purely the accept-list.
var ValidReactions = map[string]bool{
	"like": true, "love": true, "laugh": true, "wow": true, "care": true,
	"thankful": true, "pride": true, "sad": true, "angry": true, "dislike": true,
}

// IsValidReaction reports whether a reaction type is already canonical. Callers
// handling client input want NormalizeReaction instead, which also accepts the
// older shapes described below.
func IsValidReaction(t string) bool { return ValidReactions[t] }

// FederatesAsLike is the valence split (AGORA-359/360): neither ActivityPub
// nor Bluesky has any concept of a named/emoji reaction, only a plain Like.
// A positive-or-neutral reaction is close enough to that plain Like that
// showing one there beats showing nothing at all, but a negative reaction
// (sad/angry/dislike) has no equivalent on either protocol, and mapping it
// to a Like would misrepresent what actually happened, so it stays
// local-only for a target that can't represent it honestly. Shared by
// internal/feed (deciding what to send at all) and internal/federation
// (falling back to this exact split for any target that isn't a confirmed
// Agora peer, which gets the real reaction type instead — see AGORA-360).
var FederatesAsLike = map[string]bool{
	"like": true, "love": true, "laugh": true, "wow": true, "care": true,
	"thankful": true, "pride": true,
}

// Shapes older clients still send, mapped onto their canonical replacements.
//
// The mobile app ships through the App Store with no minimum-version check or
// forced-update prompt, so a build predating AGORA-305 stays in use for as long
// as its owner declines to update, and there is no mechanism to hurry them. Two
// of those older shapes would otherwise fail outright:
//
//   - DM reactions were sent as the raw emoji glyph, because the endpoint
//     stored whatever string it was handed until AGORA-305 gave it an
//     accept-list. Rejecting them breaks DM reactions entirely on older builds.
//   - `vomit` was a real reaction type until AGORA-305 replaced it with
//     `dislike`, and an old picker still offers it.
//
// Accepting and rewriting them costs one map lookup and keeps those clients
// working; rejecting them buys nothing a user can act on. The heart is listed
// both with and without its variation selector, since only the composed form
// was ever sent but the bare codepoint is indistinguishable to a reader.
//
// The web app's DM picker offered thumbs-down where the mobile app's offered
// the pouting face, so both appear here.
//
// Safe to drop once old builds have aged out; nothing depends on it beyond
// backwards compatibility.
var legacyReactions = map[string]string{
	"❤️": "love", "❤": "love", "😂": "laugh", "😮": "wow", "😢": "sad",
	"👍": "like", "👎": "dislike", "😡": "angry",
	"vomit": "dislike",
}

// NormalizeReaction maps a client-supplied reaction onto a canonical type,
// accepting both the current type names and the older shapes above. The second
// return reports whether the value was recognised at all; callers should reject
// anything it says no to, since the CHECK constraints will refuse it anyway.
func NormalizeReaction(v string) (string, bool) {
	if ValidReactions[v] {
		return v, true
	}
	if canonical, ok := legacyReactions[v]; ok {
		return canonical, true
	}
	return "", false
}
