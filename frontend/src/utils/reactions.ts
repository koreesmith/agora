export const REACTIONS = [
  { type: 'like',     emoji: '❤️',  label: 'Like'     },
  { type: 'love',     emoji: '😍',  label: 'Love'     },
  { type: 'laugh',    emoji: '😂',  label: 'Laugh'    },
  { type: 'wow',      emoji: '😮',  label: 'Wow'      },
  { type: 'angry',    emoji: '😡',  label: 'Angry'    },
  { type: 'care',     emoji: '🤗',  label: 'Care'     },
  { type: 'pride',    emoji: '🏳️‍🌈', label: 'Pride'    },
  { type: 'thankful', emoji: '🙏',  label: 'Thankful' },
  { type: 'vomit',    emoji: '🤮',  label: 'Vomit'    },
]

export const REACTION_MAP: Record<string, { emoji: string; label: string }> = Object.fromEntries(
  REACTIONS.map(r => [r.type, { emoji: r.emoji, label: r.label }])
)
