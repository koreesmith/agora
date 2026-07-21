import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { X } from 'lucide-react'

const SEEN_KEY = 'agora_seen_v3'

const FEATURES = [
  {
    emoji: '🦋',
    title: 'Native Bluesky account',
    desc: "Every Agora account is now also a real Bluesky account — no bridge, no separate signup required. Your public posts sync both ways, and you can follow any Bluesky account natively, right alongside your fediverse and Agora connections.",
    link: '/connections?tab=bluesky',
    linkText: 'Explore Bluesky →',
  },
  {
    emoji: '🗂️',
    title: 'Unified Connections & Friend Lists',
    desc: 'Friends, Fediverse follows, and Bluesky follows now live together on one Connections page — and Friend Lists can include accounts from either network, not just Agora friends.',
    link: '/connections?tab=lists',
    linkText: 'See your lists →',
  },
  {
    emoji: '💬',
    title: 'Quote posts',
    desc: 'Mastodon and other fediverse apps can now quote your Agora posts, not just boost them.',
    link: null,
    linkText: null,
  },
  {
    emoji: '😀',
    title: 'Custom emoji',
    desc: "A Mastodon custom emoji like :your_team_logo: now renders as a real inline image in names, bios, and posts, instead of showing the literal shortcode text.",
    link: null,
    linkText: null,
  },
  {
    emoji: '🔍',
    title: 'Unified search',
    desc: 'Search now covers Agora, the Fediverse, and Bluesky in one place — accounts, posts, and hashtags.',
    link: '/search',
    linkText: 'Try it →',
  },
  {
    emoji: '⚠️',
    title: 'Trigger warnings on shares',
    desc: "Add your own content warning when sharing someone else's post, independent of the original post's own visibility.",
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
            <div className="text-xs font-semibold text-agora-500 uppercase tracking-wide mb-1">v3.0.0</div>
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
