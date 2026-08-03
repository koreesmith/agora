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

// DM reactions stored the raw glyph until AGORA-305 folded them onto the shared
// type set. Existing rows are migrated in place, but a client holding a stale
// WebSocket payload can still hand us a glyph, so resolve those instead of
// rendering an empty chip.
const LEGACY_DM_GLYPHS: Record<string, string> = {
  '❤️': 'love', '😂': 'laugh', '😮': 'wow', '😢': 'sad', '👍': 'like', '👎': 'dislike',
}

// Resolves a stored reaction value (type name, or a pre-AGORA-305 DM glyph) for
// display. Unknown values render as-is rather than disappearing.
export function reactionDisplay(value: string): { emoji: string; label: string } {
  return REACTION_MAP[value] ?? REACTION_MAP[LEGACY_DM_GLYPHS[value]] ?? { emoji: value, label: value }
}
