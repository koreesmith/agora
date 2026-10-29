// Canonical reaction set, shared by post/comment reactions and DM message
// reactions (AGORA-305). Ordered positive → neutral → negative so the picker
// reads as a gradient instead of an arbitrary pile.
//
// `like` is the federation primitive: an inbound ActivityPub favourite and a
// Bluesky like both land as reaction_type='like', so it carries the neutral
// thumbs-up rather than the heart. The heart belongs to `love`, which is where
// people already expect to find it.
export const REACTIONS = [
  { type: 'like',     emoji: '👍',  label: 'Like'     },
  { type: 'love',     emoji: '❤️',  label: 'Love'     },
  { type: 'laugh',    emoji: '😂',  label: 'Laugh'    },
  { type: 'wow',      emoji: '😮',  label: 'Wow'      },
  { type: 'care',     emoji: '🫂',  label: 'Care'     },
  { type: 'thankful', emoji: '🙏',  label: 'Thankful' },
  { type: 'pride',    emoji: '🏳️‍🌈', label: 'Pride'    },
  { type: 'sad',      emoji: '😢',  label: 'Sad'      },
  { type: 'angry',    emoji: '😡',  label: 'Angry'    },
  { type: 'dislike',  emoji: '👎',  label: 'Dislike'  },
]

export const REACTION_MAP: Record<string, { emoji: string; label: string }> = Object.fromEntries(
  REACTIONS.map(r => [r.type, { emoji: r.emoji, label: r.label }])
)

// Values that are no longer canonical but can still reach a render. DM
// reactions stored the raw glyph until AGORA-305 folded them onto the shared
// type set, and 'vomit' was a real type until the same change retired it. Both
// are migrated server-side, but a cached query result or a stale WebSocket
// payload outlives a deploy, so a client can still hand us either well after
// the backend has moved on.
const LEGACY_VALUES: Record<string, string> = {
  '❤️': 'love', '😂': 'laugh', '😮': 'wow', '😢': 'sad', '👍': 'like', '👎': 'dislike',
  vomit: 'dislike',
}

// Resolves a stored reaction value (type name, or a retired value per above)
// for display. Unknown values render as-is rather than disappearing.
export function reactionDisplay(value: string): { emoji: string; label: string } {
  return REACTION_MAP[value] ?? REACTION_MAP[LEGACY_VALUES[value]] ?? { emoji: value, label: value }
}
