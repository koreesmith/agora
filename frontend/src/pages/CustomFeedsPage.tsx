import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, Rss, ChevronRight, ChevronUp, ChevronDown, Pin, PinOff } from 'lucide-react'
import { customFeedsApi } from '../api'
import FeedBuilderModal from '../components/feeds/FeedBuilderModal'

// Mirrors maxPinnedFeeds in internal/customfeeds. The server is the authority
// and returns 422 past it; this only drives the counter and disabled states.
const MAX_PINNED = 3

export default function CustomFeedsPage() {
  const qc = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [editFeed, setEditFeed] = useState<any | null>(null)
  const [pinError, setPinError] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['custom-feeds'],
    queryFn: () => customFeedsApi.list().then(r => r.data),
  })
  const feeds: any[] = data ?? []
  const pinned   = feeds.filter(f => f.pinned)
  const unpinned = feeds.filter(f => !f.pinned)

  const deleteFeed = useMutation({
    mutationFn: (id: string) => customFeedsApi.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['custom-feeds'] }),
  })

  const setPinned = useMutation({
    mutationFn: ({ id, pinned }: { id: string, pinned: boolean }) => customFeedsApi.setPinned(id, pinned),
    onSuccess: () => { setPinError(''); qc.invalidateQueries({ queryKey: ['custom-feeds'] }) },
    onError: (err: any) => setPinError(err?.response?.data?.error ?? 'Could not update pins.'),
  })

  const reorderPins = useMutation({
    mutationFn: (ids: string[]) => customFeedsApi.reorderPins(ids),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['custom-feeds'] }),
    onError: () => setPinError('Could not reorder pins.'),
  })

  // Capped at 3 pins, so up/down beats drag here: it works from the keyboard,
  // needs no pointer precision on touch, and adds no dependency for a list
  // that can never be longer than three rows.
  function movePin(index: number, delta: number) {
    const next = [...pinned]
    const target = index + delta
    if (target < 0 || target >= next.length) return
    ;[next[index], next[target]] = [next[target], next[index]]
    reorderPins.mutate(next.map(f => f.id))
  }

  function openEdit(feed: any) {
    customFeedsApi.get(feed.id).then(r => setEditFeed(r.data))
  }

  function confirmDelete(feed: any) {
    if (window.confirm(`Delete "${feed.name}"? This cannot be undone.`)) {
      deleteFeed.mutate(feed.id)
    }
  }

  function feedRow(feed: any, opts: { index?: number, pinnedRow: boolean }) {
    const atCap = !opts.pinnedRow && pinned.length >= MAX_PINNED
    return (
      <div key={feed.id} className="card flex items-center gap-3 px-4 py-3">
        {opts.pinnedRow && (
          <div className="flex flex-col flex-shrink-0">
            <button
              onClick={() => movePin(opts.index!, -1)}
              disabled={opts.index === 0 || reorderPins.isPending}
              className="p-0.5 text-agora-400 hover:text-agora-700 dark:hover:text-agora-200 disabled:opacity-30 disabled:hover:text-agora-400 transition-colors"
              title="Move up"
            >
              <ChevronUp size={14} />
            </button>
            <button
              onClick={() => movePin(opts.index!, 1)}
              disabled={opts.index === pinned.length - 1 || reorderPins.isPending}
              className="p-0.5 text-agora-400 hover:text-agora-700 dark:hover:text-agora-200 disabled:opacity-30 disabled:hover:text-agora-400 transition-colors"
              title="Move down"
            >
              <ChevronDown size={14} />
            </button>
          </div>
        )}

        <Rss size={18} className="text-agora-400 flex-shrink-0" />
        <div className="flex-1 min-w-0">
          <p className="font-medium text-agora-900 dark:text-agora-100 truncate">{feed.name}</p>
        </div>

        <div className="flex items-center gap-1 flex-shrink-0">
          <button
            onClick={() => setPinned.mutate({ id: feed.id, pinned: !opts.pinnedRow })}
            disabled={setPinned.isPending || atCap}
            className="p-1.5 text-agora-400 hover:text-agora-700 dark:hover:text-agora-200 rounded hover:bg-agora-50 dark:hover:bg-agora-800 disabled:opacity-30 disabled:hover:bg-transparent transition-colors"
            title={opts.pinnedRow ? 'Unpin from feed picker' : atCap ? `Unpin one first, ${MAX_PINNED} pins maximum` : 'Pin to feed picker'}
          >
            {opts.pinnedRow ? <PinOff size={14} /> : <Pin size={14} />}
          </button>
          <Link
            to={`/my-feeds/${feed.id}`}
            className="flex items-center gap-1 text-xs text-agora-500 hover:text-agora-700 dark:hover:text-agora-300 font-medium px-2 py-1 rounded hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors"
          >
            View <ChevronRight size={13} />
          </Link>
          <button
            onClick={() => openEdit(feed)}
            className="p-1.5 text-agora-400 hover:text-agora-700 dark:hover:text-agora-200 rounded hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors"
            title="Edit feed"
          >
            <Pencil size={14} />
          </button>
          <button
            onClick={() => confirmDelete(feed)}
            disabled={deleteFeed.isPending}
            className="p-1.5 text-agora-400 hover:text-red-500 rounded hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors"
            title="Delete feed"
          >
            <Trash2 size={14} />
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold text-agora-900 dark:text-agora-100">My Feeds</h1>
        <button onClick={() => setShowCreate(true)} className="btn-primary flex items-center gap-1.5 text-sm">
          <Plus size={15} /> New Feed
        </button>
      </div>

      {isLoading && <div className="text-center text-agora-400 py-8">Loading…</div>}

      {!isLoading && feeds.length === 0 && (
        <div className="card p-10 text-center space-y-3">
          <Rss size={36} className="mx-auto text-agora-300" />
          <p className="font-semibold text-agora-700 dark:text-agora-300">No custom feeds yet</p>
          <p className="text-sm text-agora-400">
            Create a custom feed to see posts filtered by friend groups, community groups, or specific people.
          </p>
          <button onClick={() => setShowCreate(true)} className="btn-primary text-sm mx-auto">
            Create your first feed
          </button>
        </div>
      )}

      {pinError && (
        <div className="card px-4 py-3 text-sm text-red-600 dark:text-red-400 border border-red-200 dark:border-red-900/40">
          {pinError}
        </div>
      )}

      {feeds.length > 0 && (
        <>
          <div className="space-y-2">
            <div className="flex items-baseline justify-between">
              <h2 className="text-sm font-semibold text-agora-700 dark:text-agora-300">Pinned</h2>
              <p className="text-xs text-agora-400">
                {pinned.length} of {MAX_PINNED} pins used
              </p>
            </div>
            <p className="text-xs text-agora-400">
              Pinned feeds get their own button at the top of your feed, in this order. Everything else lives in the
              feed picker's menu.
            </p>
            {pinned.length === 0 && (
              <div className="card px-4 py-6 text-center text-sm text-agora-400">
                No pinned feeds. Pin up to {MAX_PINNED} to reach them in one tap.
              </div>
            )}
            {pinned.map((feed, i) => feedRow(feed, { index: i, pinnedRow: true }))}
          </div>

          {unpinned.length > 0 && (
            <div className="space-y-2">
              <h2 className="text-sm font-semibold text-agora-700 dark:text-agora-300">More feeds</h2>
              {unpinned.map(feed => feedRow(feed, { pinnedRow: false }))}
            </div>
          )}
        </>
      )}

      {showCreate && <FeedBuilderModal onClose={() => setShowCreate(false)} />}
      {editFeed && <FeedBuilderModal feed={editFeed} onClose={() => setEditFeed(null)} />}
    </div>
  )
}
