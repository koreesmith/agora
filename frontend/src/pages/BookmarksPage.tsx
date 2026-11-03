import { useEffect, useRef } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { Bookmark } from 'lucide-react'
import { bookmarksApi } from '../api'
import PostCard from '../components/feed/PostCard'

// AGORA-429: the viewer's saved posts, most recently saved first. Pages in as
// the sentinel below the list scrolls into view, like the profile timeline.
const PAGE_SIZE = 20

export default function BookmarksPage() {
  const bottomRef = useRef<HTMLDivElement>(null)
  const {
    data, isLoading, error, fetchNextPage, hasNextPage, isFetchingNextPage, isFetchNextPageError,
  } = useInfiniteQuery({
    queryKey: ['bookmarks'],
    queryFn: ({ pageParam = 0 }) =>
      bookmarksApi.list({ offset: pageParam, limit: PAGE_SIZE }).then(r => r.data),
    getNextPageParam: (last, pages) => last.posts?.length === PAGE_SIZE ? pages.length * PAGE_SIZE : undefined,
    initialPageParam: 0,
  })

  // A failed page stops auto-loading (the sentinel, still in view, would
  // otherwise retry in a tight loop); the Retry button drives it instead.
  useEffect(() => {
    const el = bottomRef.current
    if (!el || !hasNextPage || isFetchingNextPage || isFetchNextPageError) return
    const obs = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting) fetchNextPage()
    }, { rootMargin: '400px' })
    obs.observe(el)
    return () => obs.disconnect()
  }, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage, data])

  // Paging by offset can repeat a post when the list shifts underneath it
  // (something was unsaved), so de-duplicate by id.
  const seen = new Set<string>()
  const posts = (data?.pages ?? []).flatMap((p: any) => p.posts ?? []).filter((p: any) => {
    if (seen.has(p.id)) return false
    seen.add(p.id)
    return true
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Bookmark size={22} className="text-agora-600" />
        <h1 className="text-xl font-bold">Bookmarks</h1>
      </div>
      <p className="text-sm text-agora-500">Posts you saved to come back to. Only you can see this list.</p>

      {isLoading && <div className="text-center py-12 text-agora-400">Loading your bookmarks…</div>}

      {error && !isLoading && (
        <div className="card p-6 text-center text-sm text-red-500">Could not load your bookmarks.</div>
      )}

      {!isLoading && !error && posts.length === 0 && (
        <div className="card p-12 text-center text-agora-400 space-y-2">
          <Bookmark size={32} className="mx-auto" />
          <p className="font-medium">Nothing saved yet.</p>
          <p className="text-sm">Use the bookmark button under a post to save it here.</p>
        </div>
      )}

      {posts.map((p: any) => <PostCard key={p.id} post={p} invalidateKey="bookmarks" />)}

      <div ref={bottomRef} className="h-4" />
      {isFetchingNextPage && <div className="text-center text-agora-400 text-sm py-2">Loading more…</div>}
      {isFetchNextPageError && !isFetchingNextPage && (
        <div className="text-center text-sm py-2">
          <p className="text-red-500">Could not load more bookmarks.</p>
          <button onClick={() => fetchNextPage()} className="btn-secondary text-xs mt-2">Retry</button>
        </div>
      )}
    </div>
  )
}
