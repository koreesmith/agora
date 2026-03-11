import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { feedApi } from '../api'
import PostCard from '../components/feed/PostCard'
import CommentsSection from '../components/feed/CommentsSection'

export default function PostPage() {
  const { id } = useParams<{ id: string }>()

  const { data, isLoading } = useQuery({
    queryKey: ['post', id],
    queryFn: () => feedApi.getPost(id!).then(r => r.data),
    enabled: !!id,
  })

  if (isLoading) return <div className="text-center py-12 text-agora-400">Loading…</div>
  if (!data?.post) return <div className="text-center py-12 text-agora-400">Post not found.</div>

  return (
    <div className="space-y-4">
      <PostCard post={data.post} invalidateKey={`post-${id}`} />
      <CommentsSection postId={id!} postAuthorId={data.post.author_id} />
    </div>
  )
}
