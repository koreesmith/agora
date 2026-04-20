import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { ArrowLeft, Rss, Pencil } from 'lucide-react'
import { feedApi, customFeedsApi } from '../api'
import PostCard from '../components/feed/PostCard'
import FeedBuilderModal from '../components/feeds/FeedBuilderModal'

export default function CustomFeedViewPage() {
  const { id } = useParams<{ id: string }>()
  const [editing, setEditing] = useState(false)
  const [editFeed, setEditFeed] = useState<any | null>(null)

  const { data: feedData, refetch: refetchMeta } = useQuery({
    queryKey: ['custom-feed', id],
    queryFn: () => customFeedsApi.get(id!).then(r => r.data),
    enabled: !!id,
  })

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['feed', 'custom', id],
    queryFn: ({ pageParam = 0 }) =>
      feedApi.getFeed({ offset: pageParam, limit: 20, custom_feed_id: id }).then(r => r.data),
    getNextPageParam: (lastPage, pages) =>
      lastPage.posts?.length === 20 ? pages.length * 20 : undefined,
    initialPageParam: 0,
    enabled: !!id,
  })

  const posts = data?.pages.flatMap(p => p.posts) ?? []

  function openEdit() {
    setEditFeed(feedData)
    setEditing(true)
  }

  function handleEditClose() {
    setEditing(false)
    setEditFeed(null)
    refetchMeta()
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <Link to="/my-feeds" className="p-1.5 text-agora-500 hover:text-agora-700 rounded hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors">
          <ArrowLeft size={18} />
        </Link>
        <Rss size={18} className="text-agora-400" />
        <div className="flex-1 min-w-0">
          <h1 className="text-lg font-bold leading-tight truncate">
            {feedData?.name ?? 'Custom Feed'}
          </h1>
          <p className="text-xs text-agora-400">
            {feedData?.filters?.length ?? 0} filter {feedData?.filters?.length === 1 ? 'rule' : 'rules'}
          </p>
        </div>
        <button
          onClick={openEdit}
          className="p-1.5 text-agora-400 hover:text-agora-700 dark:hover:text-agora-200 rounded hover:bg-agora-50 dark:hover:bg-agora-800 transition-colors"
          title="Edit feed"
        >
          <Pencil size={16} />
        </button>
      </div>

      {isLoading && <div className="text-center text-agora-400 py-8">Loading…</div>}

      {posts.length === 0 && !isLoading && (
        <div className="card p-10 text-center space-y-2">
          <Rss size={32} className="mx-auto text-agora-300" />
          <p className="font-medium text-agora-600 dark:text-agora-400">No posts yet</p>
          <p className="text-sm text-agora-400">
            Posts matching your filter rules will appear here.
          </p>
        </div>
      )}

      {posts.map((post: any) => (
        <PostCard key={post.id} post={post} invalidateKey={`feed-custom-${id}`} />
      ))}

      {hasNextPage && (
        <button
          onClick={() => fetchNextPage()}
          disabled={isFetchingNextPage}
          className="w-full btn-secondary text-sm"
        >
          {isFetchingNextPage ? 'Loading…' : 'Load more'}
        </button>
      )}

      {editing && editFeed && (
        <FeedBuilderModal feed={editFeed} onClose={handleEditClose} />
      )}
    </div>
  )
}
