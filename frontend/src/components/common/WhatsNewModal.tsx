import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { X } from 'lucide-react'

const SEEN_KEY = 'agora_seen_v4'

const FEATURES = [
  {
    emoji: '🌉',
    title: 'Friends across Agora instances',
    desc: "Friend requests, friends-only posts, and their replies and reactions now reach your friends even when they're on a different Agora instance, not just your own.",
    link: '/connections',
    linkText: 'See your friends →',
  },
  {
    emoji: '✉️',
    title: 'Direct messages across instances',
    desc: 'Send and receive DMs with friends on other Agora instances, not just your own.',
    link: '/messages',
    linkText: 'Open Messages →',
  },
  {
    emoji: '🌐',
    title: 'Bring your own domain',
    desc: "Use your own custom domain as your AT Protocol handle. Once it's verified, a badge shows up right next to your name.",
    link: '/settings?tab=bluesky',
    linkText: 'Set it up →',
  },
  {
    emoji: '📌',
    title: 'Pinned feed pills',
    desc: 'Pin your favorite feeds to the top of your feed bar, with an overflow menu for the rest.',
    link: '/feed',
    linkText: 'Try it →',
  },
  {
    emoji: '😀',
    title: 'Refreshed reactions',
    desc: 'A realigned reaction set, shared consistently between posts and Messages.',
    link: null,
    linkText: null,
  },
  {
    emoji: '👀',
    title: 'See your followers',
    desc: 'View your fediverse and Bluesky follower and following lists, now with a dedicated Followers segment in each tab.',
    link: '/connections?tab=fediverse&sub=followers',
    linkText: 'Check your followers →',
  },
  {
    emoji: '🙈',
    title: 'Hide a post',
    desc: 'Remove a post from your own timeline without deleting it.',
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
            <div className="text-xs font-semibold text-agora-500 uppercase tracking-wide mb-1">v4.0.0</div>
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
