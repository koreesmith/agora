import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, Rss, ChevronRight } from 'lucide-react'
import { customFeedsApi } from '../api'
import FeedBuilderModal from '../components/feeds/FeedBuilderModal'

export default function CustomFeedsPage() {
  const qc = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [editFeed, setEditFeed] = useState<any | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['custom-feeds'],
    queryFn: () => customFeedsApi.list().then(r => r.data),
  })
  const feeds: any[] = data ?? []

  const deleteFeed = useMutation({
    mutationFn: (id: string) => customFeedsApi.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['custom-feeds'] }),
  })

  function openEdit(feed: any) {
    customFeedsApi.get(feed.id).then(r => setEditFeed(r.data))
  }

  function confirmDelete(feed: any) {
    if (window.confirm(`Delete "${feed.name}"? This cannot be undone.`)) {
      deleteFeed.mutate(feed.id)
    }
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

      <div className="space-y-2">
        {feeds.map(feed => (
          <div key={feed.id} className="card flex items-center gap-3 px-4 py-3">
            <Rss size={18} className="text-agora-400 flex-shrink-0" />
            <div className="flex-1 min-w-0">
              <p className="font-medium text-agora-900 dark:text-agora-100 truncate">{feed.name}</p>
            </div>
            <div className="flex items-center gap-1 flex-shrink-0">
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
        ))}
      </div>

      {showCreate && <FeedBuilderModal onClose={() => setShowCreate(false)} />}
      {editFeed && <FeedBuilderModal feed={editFeed} onClose={() => setEditFeed(null)} />}
    </div>
  )
}
