import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { X } from 'lucide-react'

// AGORA-448: moved on for v5.0.0 so members who dismissed the v4.1.0 list see
// the new one once.
const SEEN_KEY = 'agora_seen_v5_0_0'

// AGORA-388: a brand-new account has just been through the getting started
// wizard, so the auto-shown What's New would only repeat it. Finishing or
// skipping the wizard marks the current version as seen, for new accounts
// only: a returning member still gets it (AGORA-393).
export function markWhatsNewSeen() {
  try { localStorage.setItem(SEEN_KEY, '1') } catch { /* storage unavailable */ }
}

const FEATURES = [
  {
    emoji: '🎬',
    title: 'Reels',
    desc: 'Watch short videos one after another. Following shows reels from the people you follow, and Discover shows reels from around the network. Videos you have already watched are skipped.',
    link: '/reels',
    linkText: 'Watch Reels →',
  },
  {
    emoji: '🎥',
    title: 'Make a reel',
    desc: 'Record a video or choose one, trim it, pick the cover frame and post it. Reels also reach the Fediverse, where other apps show them as videos.',
    link: '/reels',
    linkText: 'Make a reel →',
  },
  {
    emoji: '📌',
    title: 'Pinned posts',
    desc: 'Pin up to three of your own posts to the top of your profile. Use the menu on a post to pin or unpin it.',
    link: null,
    linkText: null,
  },
  {
    emoji: '📝',
    title: 'Drafts',
    desc: 'Save a post to finish later. Use Save draft in the composer, then pick it from Drafts when you are ready. Drafts are kept for 90 days.',
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
            <div className="text-xs font-semibold text-agora-500 uppercase tracking-wide mb-1">v5.0.0</div>
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
