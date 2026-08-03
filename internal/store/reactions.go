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

// IsValidReaction reports whether a client-supplied reaction type is one this
// instance accepts.
func IsValidReaction(t string) bool { return ValidReactions[t] }
