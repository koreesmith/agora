import { useState, useRef, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Home, Rss, Settings2, ChevronDown, Search } from 'lucide-react'
import { customFeedsApi } from '../../api'

interface Props {
  activeFeedId: string | null
  onChange: (id: string | null) => void
}

// Past this many feeds the overflow menu stops being scannable and earns a
// search box. Below it, a search box is just a control nobody needs.
const SEARCH_THRESHOLD = 8

export default function FeedSwitcher({ activeFeedId, onChange }: Props) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const menuRef = useRef<HTMLDivElement>(null)

  const { data } = useQuery({
    queryKey: ['custom-feeds'],
    queryFn: () => customFeedsApi.list().then(r => r.data),
  })
  const feeds: any[] = data ?? []

  // The server returns pinned feeds first, already in the user's order.
  const pinned   = feeds.filter(f => f.pinned)
  const unpinned = feeds.filter(f => !f.pinned)

  // An unpinned feed the user picked from the menu joins the pill row for as
  // long as it stays selected. Without this the row would show no active pill
  // at all, leaving no on-screen answer to "which feed am I reading?".
  const promoted = useMemo(
    () => (activeFeedId ? unpinned.find(f => f.id === activeFeedId) ?? null : null),
    [activeFeedId, unpinned],
  )
  const pills    = promoted ? [...pinned, promoted] : pinned
  const overflow = promoted ? unpinned.filter(f => f.id !== promoted.id) : unpinned

  const matches = query.trim()
    ? overflow.filter(f => f.name.toLowerCase().includes(query.trim().toLowerCase()))
    : overflow

  useEffect(() => {
    if (!open) return
    function onDocClick(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDocClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDocClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  useEffect(() => {
    if (!open) setQuery('')
  }, [open])

  function pillClass(active: boolean) {
    return `flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-medium whitespace-nowrap transition-colors flex-shrink-0 ${
      active
        ? 'bg-agora-700 text-white'
        : 'bg-agora-100 dark:bg-agora-800 text-agora-700 dark:text-agora-300 hover:bg-agora-200 dark:hover:bg-agora-700'
    }`
  }

  function selectFeed(id: string | null) {
    onChange(id)
    setOpen(false)
  }

  return (
    <div className="flex items-center gap-2 pb-1">
      {/* The pill row can still scroll on narrow screens, where a few pinned
          feeds may not fit. The fade marks that there is more to swipe to,
          which a bare overflow-x-auto with hidden scrollbars never did. */}
      <div className="flex items-center gap-2 overflow-x-auto scrollbar-none feed-pills-fade flex-1 min-w-0">
        <button onClick={() => selectFeed(null)} className={pillClass(activeFeedId === null)}>
          <Home size={13} />
          Home
        </button>

        {pills.map(feed => (
          <button key={feed.id} onClick={() => selectFeed(feed.id)} className={pillClass(activeFeedId === feed.id)}>
            <Rss size={13} />
            {feed.name}
          </button>
        ))}
      </div>

      <div className="relative flex-shrink-0" ref={menuRef}>
        <button
          onClick={() => setOpen(v => !v)}
          aria-expanded={open}
          aria-haspopup="menu"
          className={`flex items-center gap-1 px-3 py-1.5 rounded-full text-sm font-medium whitespace-nowrap transition-colors ${
            open
              ? 'bg-agora-200 dark:bg-agora-700 text-agora-900 dark:text-agora-100'
              : 'text-agora-500 dark:text-agora-400 hover:bg-agora-100 dark:hover:bg-agora-800'
          }`}
        >
          {overflow.length > 0 ? `${overflow.length} more` : 'Feeds'}
          <ChevronDown size={13} className={open ? 'rotate-180 transition-transform' : 'transition-transform'} />
        </button>

        {open && (
          <div
            role="menu"
            className="absolute right-0 top-full mt-1 w-60 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-xl shadow-lg py-1 z-30"
          >
            {feeds.length > SEARCH_THRESHOLD && (
              <div className="px-2 pb-1">
                <div className="flex items-center gap-2 px-2 py-1.5 rounded-lg border border-agora-200 dark:border-agora-600">
                  <Search size={13} className="text-agora-400 flex-shrink-0" />
                  <input
                    autoFocus
                    value={query}
                    onChange={e => setQuery(e.target.value)}
                    placeholder="Find a feed"
                    className="w-full bg-transparent text-sm outline-none text-agora-900 dark:text-agora-100 placeholder:text-agora-400"
                  />
                </div>
              </div>
            )}

            <div className="max-h-72 overflow-y-auto">
              {matches.map(feed => (
                <button
                  key={feed.id}
                  role="menuitem"
                  onClick={() => selectFeed(feed.id)}
                  className={`w-full text-left px-3 py-2 text-sm flex items-center gap-2 hover:bg-agora-50 dark:hover:bg-agora-700 ${
                    activeFeedId === feed.id ? 'font-semibold' : ''
                  }`}
                >
                  <Rss size={14} className="text-agora-400 flex-shrink-0" />
                  <span className="truncate">{feed.name}</span>
                </button>
              ))}

              {matches.length === 0 && (
                <p className="px-3 py-3 text-sm text-agora-400 text-center">
                  {query.trim()
                    ? 'No feeds match'
                    : feeds.length === 0
                      ? 'No custom feeds yet'
                      : 'Every feed is pinned'}
                </p>
              )}
            </div>

            {/* Lives in the menu rather than the pill row, where it used to
                scroll out of reach exactly when a user had enough feeds to
                need it. */}
            <div className="border-t border-agora-100 dark:border-agora-700 mt-1 pt-1">
              <Link
                to="/my-feeds"
                onClick={() => setOpen(false)}
                className="w-full px-3 py-2 text-sm flex items-center gap-2 text-agora-500 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-700"
              >
                <Settings2 size={14} />
                Manage feeds
              </Link>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
