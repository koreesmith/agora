import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { X } from 'lucide-react'

// AGORA-435: moved on for v4.1.0 so members who dismissed the v4.0.0 list see
// the new one once.
const SEEN_KEY = 'agora_seen_v4_1_0'

// AGORA-388: a brand-new account has just been through the getting started
// wizard, so the auto-shown What's New would only repeat it. Finishing or
// skipping the wizard marks the current version as seen, for new accounts
// only: a returning member still gets it (AGORA-393).
export function markWhatsNewSeen() {
  try { localStorage.setItem(SEEN_KEY, '1') } catch { /* storage unavailable */ }
}

const FEATURES = [
  {
    emoji: '🔖',
    title: 'Bookmarks',
    desc: 'Save any post to come back to later. Tap the bookmark under a post, then find everything you saved in Bookmarks. Only you can see them.',
    link: '/bookmarks',
    linkText: 'Open Bookmarks →',
  },
  {
    emoji: '🔇',
    title: 'Muted keywords',
    desc: 'Hide posts that contain words or phrases you choose, for a set number of days or for good. It applies to your timelines, including posts from the Fediverse and Bluesky.',
    link: '/settings?tab=blocked',
    linkText: 'Mute a word →',
  },
  {
    emoji: '🧭',
    title: 'Better Find Friends',
    desc: 'Suggestions now say why someone is suggested, you can dismiss the ones you do not want, and they include people from the Fediverse and Bluesky.',
    link: '/discover',
    linkText: 'Find friends →',
  },
  {
    emoji: '🔎',
    title: 'Search shows who you follow',
    desc: 'Search results now show when you already follow someone or are friends with them.',
    link: null,
    linkText: null,
  },
  {
    emoji: '🔢',
    title: 'Accurate follower counts',
    desc: 'Follower and following counts are now correct across Agora, the Fediverse and Bluesky.',
    link: null,
    linkText: null,
  },
  {
    emoji: '♾️',
    title: 'Keep scrolling on profiles',
    desc: 'Posts on a profile page now keep loading as you scroll, instead of stopping after the first batch.',
    link: null,
    linkText: null,
  },
  {
    emoji: '🔁',
    title: 'Reposts reach Bluesky',
    desc: "When you repost a public post that isn't on Bluesky, it now appears there as a post from you with the original text and a link back.",
    link: null,
    linkText: null,
  },
]

interface Props {
  /** When true, show the modal regardless of localStorage (manual trigger). */
  forceShow?: boolean
  onClose?: () => void
}

export default function WhatsNewModal({ forceShow, onClose }: Props = {}) {
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    if (!localStorage.getItem(SEEN_KEY)) {
      setVisible(true)
    }
  }, [])

  // Respond to external force-show without touching localStorage
  useEffect(() => {
    if (forceShow) setVisible(true)
  }, [forceShow])

  const dismiss = () => {
    // Only set the key on the first-time auto-show, not on manual triggers
    if (!localStorage.getItem(SEEN_KEY)) {
      localStorage.setItem(SEEN_KEY, '1')
    }
    setVisible(false)
    onClose?.()
  }

  if (!visible) return null

  return (
    <div className="fixed inset-0 bg-black/60 z-50 flex items-center justify-center p-4"
      onClick={dismiss}>
      <div
        className="bg-white dark:bg-agora-800 rounded-2xl shadow-2xl w-full max-w-lg max-h-[85vh] flex flex-col"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 pt-6 pb-3">
          <div>
            <div className="text-xs font-semibold text-agora-500 uppercase tracking-wide mb-1">v4.1.0</div>
            <h2 className="text-xl font-bold text-agora-900 dark:text-agora-100">What's New in Agora</h2>
          </div>
          <button onClick={dismiss} className="text-agora-400 hover:text-agora-600 transition-colors ml-4 flex-shrink-0">
            <X size={20} />
          </button>
        </div>

        {/* Feature cards */}
        <div className="overflow-y-auto flex-1 px-6 pb-2 space-y-3">
          {FEATURES.map((f, i) => (
            <div key={i} className="flex gap-3 bg-agora-50 dark:bg-agora-700/50 rounded-xl p-3">
              <span className="text-2xl flex-shrink-0 w-8 text-center">{f.emoji}</span>
              <div className="min-w-0">
                <p className="font-semibold text-sm text-agora-800 dark:text-agora-100">{f.title}</p>
                <p className="text-xs text-agora-500 dark:text-agora-400 mt-0.5">{f.desc}</p>
                {f.link && (
                  <Link to={f.link} onClick={dismiss}
                    className="text-xs text-agora-600 dark:text-agora-400 hover:underline mt-1 inline-block">
                    {f.linkText}
                  </Link>
                )}
              </div>
            </div>
          ))}
        </div>

        {/* Footer */}
        <div className="px-6 py-4 border-t border-agora-100 dark:border-agora-700 flex items-center justify-between">
          <a href="/docs#user/index" target="_blank" rel="noopener noreferrer"
            className="text-xs text-agora-400 hover:text-agora-600 hover:underline">
            Read the full user guide →
          </a>
          <button onClick={dismiss} className="btn-primary text-sm">
            Got it!
          </button>
        </div>
      </div>
    </div>
  )
}
