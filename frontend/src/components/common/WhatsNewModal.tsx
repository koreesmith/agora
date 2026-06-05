import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { X } from 'lucide-react'

const SEEN_KEY = 'agora_seen_v2'

const FEATURES = [
  {
    emoji: '📖',
    title: 'Pages',
    desc: 'Follow public Pages from bands, businesses, creators, and organisations. Their posts appear in your feed.',
    link: '/pages',
    linkText: 'Explore Pages →',
  },
  {
    emoji: '🎬',
    title: 'Video posts',
    desc: 'Post videos up to 2 minutes. Attach a video the same way you attach photos — just click the 🎬 button.',
    link: '/feed',
    linkText: 'Go to feed →',
  },
  {
    emoji: '✨',
    title: 'Smart Ranking for Custom Feeds',
    desc: 'Enable Smart Ranking in any custom feed to surface posts from people you interact with most.',
    link: '/my-feeds',
    linkText: 'My Feeds →',
  },
  {
    emoji: '📷',
    title: 'Up to 10 photos per post',
    desc: 'Attach up to 10 photos in a single post. Paste images directly from your clipboard too.',
    link: null,
    linkText: null,
  },
  {
    emoji: '+',
    title: 'Tag groups in posts',
    desc: 'Type +group-name in any post to tag a group. Group admins are notified.',
    link: null,
    linkText: null,
  },
  {
    emoji: '💬',
    title: 'Quick comments',
    desc: 'A comment box now appears directly below each post — no need to click the comment icon first.',
    link: null,
    linkText: null,
  },
]

export default function WhatsNewModal() {
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    if (!localStorage.getItem(SEEN_KEY)) {
      setVisible(true)
    }
  }, [])

  const dismiss = () => {
    localStorage.setItem(SEEN_KEY, '1')
    setVisible(false)
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
            <div className="text-xs font-semibold text-agora-500 uppercase tracking-wide mb-1">v2.0.0</div>
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
