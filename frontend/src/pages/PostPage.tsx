import { useEffect } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { feedApi } from '../api'
import PostCard from '../components/feed/PostCard'
import CommentsSection from '../components/feed/CommentsSection'
import { Lock, Users, UserX, ArrowLeft } from 'lucide-react'

export default function PostPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const { data, isLoading, error } = useQuery({
    queryKey: ['post', id],
    queryFn: () => feedApi.getPost(id!).then(r => r.data),
    enabled: !!id,
    retry: false, // don't retry 403s
  })

  // If the backend tells us this is a comment, redirect to the parent post
  useEffect(() => {
    if (data?.redirect_to_post) {
      navigate(`/post/${data.redirect_to_post}`, { replace: true })
    }
  }, [data, navigate])

  if (isLoading) return (
    <div className="text-center py-12 text-agora-400">Loading…</div>
  )

  // Handle structured 403 access-denied responses
  if ((error as any)?.response?.status === 403) {
    const errData = (error as any).response.data
    return <AccessDenied reason={errData?.reason} groupName={errData?.group_name} groupSlug={errData?.group_slug} />
  }

  if (error || !data?.post) return (
    <div className="card p-10 text-center space-y-3">
      <p className="font-semibold text-agora-600">Post not found</p>
      <p className="text-sm text-agora-400">This post may have been deleted or doesn't exist.</p>
      <Link to="/" className="btn-secondary text-sm inline-flex items-center gap-1.5">
        <ArrowLeft size={14} /> Back to feed
      </Link>
    </div>
  )

  const post = data.post

  return (
    <div className="space-y-4">
      <Link to="/" className="flex items-center gap-1.5 text-sm text-agora-500 hover:text-agora-700 transition-colors">
        <ArrowLeft size={14} /> Back to feed
      </Link>
      <PostCard post={post} invalidateKey="post" />
      <CommentsSection postId={id!} postAuthorId={post.author_id} />
    </div>
  )
}

// ── Access Denied ─────────────────────────────────────────────────────────────

function AccessDenied({ reason, groupName, groupSlug }: {
  reason?: string
  groupName?: string
  groupSlug?: string
}) {
  let icon = <Lock size={36} className="mx-auto text-agora-400" />
  let title = 'Post unavailable'
  let message = 'You don\'t have permission to view this post.'
  let action: React.ReactNode = null

  switch (reason) {
    case 'not_friends':
      icon = <UserX size={36} className="mx-auto text-agora-400" />
      title = 'Friends only'
      message = 'This post was shared with friends only. You\'ll need to be friends with the author to view it.'
      break
    case 'not_group_member':
      icon = <Users size={36} className="mx-auto text-agora-400" />
      title = 'Members only'
      message = groupName
        ? `This post is in the private group "${groupName}". You need to be a member to view it.`
        : 'This post is in a private group. You need to be a member to view it.'
      if (groupSlug) {
        action = (
          <Link to={`/groups/${groupSlug}`} className="btn-primary text-sm inline-flex items-center gap-1.5">
            <Users size={14} /> View group
          </Link>
        )
      }
      break
    case 'private':
      icon = <Lock size={36} className="mx-auto text-agora-400" />
      title = 'Private post'
      message = 'This post is private and can only be seen by its author.'
      break
  }

  return (
    <div className="card p-10 text-center space-y-4">
      {icon}
      <div className="space-y-1">
        <p className="font-semibold text-lg">{title}</p>
        <p className="text-sm text-agora-500 max-w-xs mx-auto">{message}</p>
      </div>
      <div className="flex items-center justify-center gap-3 pt-1">
        {action}
        <Link to="/" className="btn-secondary text-sm inline-flex items-center gap-1.5">
          <ArrowLeft size={14} /> Back to feed
        </Link>
      </div>
    </div>
  )
}
