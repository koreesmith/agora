import { useInfiniteQuery } from '@tanstack/react-query'
import { feedApi } from '../api'
import CreatePost from '../components/feed/CreatePost'
import PostCard from '../components/feed/PostCard'
import { useEffect, useRef } from 'react'

export default function FeedPage() {
  const bottomRef = useRef<HTMLDivElement>(null)

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } = useInfiniteQuery({
    queryKey: ['feed'],
    queryFn: ({ pageParam = 0 }) => feedApi.getFeed({ offset: pageParam, limit: 20 }).then(r => r.data),
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
      <CreatePost />
      {isLoading && <div className="text-center text-agora-400 py-8">Loading…</div>}
      {posts.map(p => <PostCard key={p.id} post={p} invalidateKey="feed" />)}
      {posts.length === 0 && !isLoading && (
        <div className="card p-8 text-center text-agora-400">
          <p className="font-medium">Your feed is empty</p>
          <p className="text-sm mt-1">Add friends to see their posts here.</p>
        </div>
      )}
      <div ref={bottomRef} className="h-4" />
      {isFetchingNextPage && <div className="text-center text-agora-400 text-sm py-2">Loading more…</div>}
    </div>
  )
}
