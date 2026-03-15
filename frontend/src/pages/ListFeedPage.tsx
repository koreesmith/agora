import { useParams, Link } from 'react-router-dom'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { feedApi, friendsApi } from '../api'
import PostCard from '../components/feed/PostCard'
import CreatePost from '../components/feed/CreatePost'
import { ArrowLeft, List } from 'lucide-react'

export default function ListFeedPage() {
  const { id } = useParams<{ id: string }>()

  // Get the list name
  const { data: listsData } = useQuery({
    queryKey: ['friend-groups'],
    queryFn: () => friendsApi.listGroups().then(r => r.data),
  })
  const list = listsData?.groups?.find((g: any) => g.id === id)

  const { data, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ['feed', 'list', id],
    queryFn: ({ pageParam = 0 }) => feedApi.getFeed({ page: pageParam, list_id: id }).then(r => r.data),
    getNextPageParam: (lastPage, pages) => lastPage.posts?.length === 20 ? pages.length : undefined,
    initialPageParam: 0,
  })

  const posts = data?.pages.flatMap(p => p.posts) ?? []

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <Link to="/friends" className="btn-ghost p-1.5 text-agora-500 hover:text-agora-700">
          <ArrowLeft size={18} />
        </Link>
        <List size={18} className="text-agora-400" />
        <div>
          <h1 className="text-lg font-bold leading-tight">{list?.name ?? 'List'}</h1>
          <p className="text-xs text-agora-400">{list?.member_count ?? 0} {list?.member_count === 1 ? 'person' : 'people'}</p>
        </div>
      </div>

      <CreatePost />

      {posts.length === 0 && !isFetchingNextPage && (
        <div className="card p-10 text-center text-agora-400 space-y-2">
          <List size={32} className="mx-auto opacity-40" />
          <p className="font-medium">No posts yet</p>
          <p className="text-sm">Posts from people in this list will appear here. You can also post something above — use "Friend List" visibility to share it only with this list.</p>
        </div>
      )}

      {posts.map((post: any) => <PostCard key={post.id} post={post} invalidateKey="feed" />)}

      {hasNextPage && (
        <button onClick={() => fetchNextPage()} disabled={isFetchingNextPage}
          className="w-full btn-secondary text-sm">
          {isFetchingNextPage ? 'Loading…' : 'Load more'}
        </button>
      )}
    </div>
  )
}
