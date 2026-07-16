import { useEffect, useRef } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { feedApi } from '../api'
import PostCard from '../components/feed/PostCard'

export default function ExplorePage() {
  const bottomRef = useRef<HTMLDivElement>(null)

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['public-feed'],
    queryFn: ({ pageParam = 0 }) =>
      feedApi.getPublicFeed({ offset: pageParam, limit: 20 }).then(r => r.data),
    getNextPageParam: (last, pages) => last.posts?.length === 20 ? pages.length * 20 : undefined,
    initialPageParam: 0,
  })

  useEffect(() => {
    const obs = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) fetchNextPage()
    })
    if (bottomRef.current) obs.observe(bottomRef.current)
    return () => obs.disconnect()
  }, [hasNextPage, isFetchingNextPage])

  const posts = data?.pages.flatMap(p => p.posts) || []

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-xl font-bold text-agora-900 dark:text-agora-100">Explore</h1>
        <p className="text-sm text-agora-500 mt-0.5">Public posts from across the instance.</p>
      </div>
      {isLoading && <div className="text-center text-agora-400 py-8">Loading…</div>}
      {posts.map(p => <PostCard key={p.id} post={p} invalidateKey="public-feed" />)}
      {posts.length === 0 && !isLoading && (
        <div className="card p-8 text-center text-agora-400">
          <p className="font-medium">No public posts yet</p>
        </div>
      )}
      <div ref={bottomRef} className="h-4" />
      {isFetchingNextPage && <div className="text-center text-agora-400 text-sm py-2">Loading more…</div>}
    </div>
  )
}
