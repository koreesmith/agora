import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Heart, MessageCircle, Repeat2, Trash2, Flag, Globe, Users, Lock, MoreHorizontal, X } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { feedApi, moderationApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import { formatDistanceToNow } from 'date-fns'
import CommentsSection, { renderContent } from './CommentsSection'

interface Post {
  id: string
  author_id: string
  author_username: string
  author_display_name: string
  author_avatar_url: string
  content: string
  image_url: string
  visibility: string
  repost_of_id?: string
  repost_author_username?: string
  repost_author_display_name?: string
  repost_content?: string
  repost_image_url?: string
  like_count: number
  comment_count: number
  repost_count: number
  liked: boolean
  reposted: boolean
  created_at: string
}

const visIcons: Record<string, React.ReactNode> = {
  public:  <Globe size={12} />,
  friends: <Users size={12} />,
  group:   <Lock size={12} />,
  private: <Lock size={12} />,
}

export default function PostCard({ post, invalidateKey = 'feed' }: { post: Post, invalidateKey?: string }) {
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null)
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [showComments, setShowComments] = useState(false)
  const [showMenu, setShowMenu] = useState(false)

  const invalidate = () => qc.invalidateQueries({ queryKey: [invalidateKey] })

  const like = useMutation({
    mutationFn: () => post.liked ? feedApi.unlikePost(post.id) : feedApi.likePost(post.id),
    onSuccess: invalidate,
  })

  const del = useMutation({
    mutationFn: () => feedApi.deletePost(post.id),
    onSuccess: invalidate,
  })

  const repost = useMutation({
    mutationFn: () => feedApi.repost(post.id),
    onSuccess: invalidate,
  })

  const report = useMutation({
    mutationFn: (reason: string) => moderationApi.createReport({
      reported_post_id: post.id, reason,
    }),
  })

  const isOwn = user?.id === post.author_id
  const canDelete = isOwn || user?.role === 'admin' || user?.role === 'moderator'

  return (
    <div className="card p-4">
      {/* Repost header */}
      {post.repost_of_id && (
        <div className="flex items-center gap-1.5 text-xs text-agora-500 dark:text-agora-400 mb-3">
          <Repeat2 size={13} />
          <Link to={`/profile/${post.author_username}`} className="font-medium hover:underline">
            {post.author_display_name || post.author_username}
          </Link>
          <span>reposted</span>
        </div>
      )}

      {/* Author row */}
      <div className="flex items-start gap-3">
        <Link to={`/profile/${post.repost_of_id ? post.repost_author_username : post.author_username}`} className="flex-shrink-0">
          <div className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden">
            {post.author_avatar_url && !post.repost_of_id
              ? <img src={post.author_avatar_url} alt="" className="w-full h-full object-cover" />
              : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 dark:text-agora-300">
                  {(post.repost_of_id ? post.repost_author_display_name : post.author_display_name)?.[0]?.toUpperCase() || '?'}
                </span>
            }
          </div>
        </Link>

        <div className="flex-1 min-w-0">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5 flex-wrap">
              <Link to={`/profile/${post.repost_of_id ? post.repost_author_username : post.author_username}`}
                className="font-semibold text-agora-900 dark:text-agora-100 hover:underline text-sm">
                {post.repost_of_id ? post.repost_author_display_name : post.author_display_name}
              </Link>
              <span className="text-agora-400 text-xs">
                @{post.repost_of_id ? post.repost_author_username : post.author_username}
              </span>
              <span className="text-agora-300 dark:text-agora-600 text-xs">·</span>
              <span className="text-agora-400 text-xs">
                {formatDistanceToNow(new Date(post.created_at), { addSuffix: true })}
              </span>
              <span className="text-agora-300 dark:text-agora-600 flex items-center gap-0.5 text-xs">
                {visIcons[post.visibility]}
              </span>
            </div>

            {/* Menu */}
            <div className="relative flex-shrink-0">
              <button onClick={() => setShowMenu(m => !m)} className="btn-ghost p-1 text-agora-400">
                <MoreHorizontal size={16} />
              </button>
              {showMenu && (
                <div className="absolute right-0 top-6 z-10 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-700 rounded-lg shadow-lg py-1 min-w-[140px]"
                  onBlur={() => setShowMenu(false)}>
                  {canDelete && (
                    <button onClick={() => { if (confirm('Delete post?')) del.mutate(); setShowMenu(false) }}
                      className="flex items-center gap-2 w-full px-3 py-2 text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20">
                      <Trash2 size={14} /> Delete
                    </button>
                  )}
                  {!isOwn && (
                    <button onClick={() => {
                      const reason = prompt('Report reason (spam, harassment, misinformation, other):') || 'other'
                      report.mutate(reason)
                      setShowMenu(false)
                    }}
                      className="flex items-center gap-2 w-full px-3 py-2 text-sm text-agora-600 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-700">
                      <Flag size={14} /> Report
                    </button>
                  )}
                </div>
              )}
            </div>
          </div>

          {/* Content */}
          {(post.repost_of_id ? post.repost_content : post.content) && (
            <p className="text-sm text-agora-800 dark:text-agora-200 mt-1 whitespace-pre-wrap break-words">
              {renderContent(post.repost_of_id ? post.repost_content! : post.content)}
            </p>
          )}

          {/* Reposter's comment */}
          {post.repost_of_id && post.content && (
            <p className="text-sm text-agora-500 dark:text-agora-400 mt-2 italic">
              "{renderContent(post.content)}"
            </p>
          )}

          {/* Image */}
          {(post.repost_of_id ? post.repost_image_url : post.image_url) && (() => {
            const url = post.repost_of_id ? post.repost_image_url : post.image_url
            return (
              <>
                <button
                  onClick={() => setLightboxUrl(url!)}
                  className="mt-2 block w-full rounded-lg overflow-hidden focus:outline-none"
                >
                  <img
                    src={url}
                    alt=""
                    className="w-full max-h-80 object-cover rounded-lg hover:opacity-95 transition-opacity cursor-zoom-in"
                  />
                </button>
                {lightboxUrl === url && (
                  <div
                    className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center p-4"
                    onClick={() => setLightboxUrl(null)}
                  >
                    <button
                      className="absolute top-4 right-4 text-white bg-black/40 rounded-full p-1.5 hover:bg-black/70"
                      onClick={() => setLightboxUrl(null)}
                    >
                      <X size={20} />
                    </button>
                    <img
                      src={url}
                      alt=""
                      className="max-w-3xl max-h-[85vh] w-auto h-auto rounded-lg shadow-2xl object-contain"
                      onClick={e => e.stopPropagation()}
                    />
                  </div>
                )}
              </>
            )
          })()}

          {/* Actions */}
          <div className="flex items-center gap-4 mt-3 text-agora-400 dark:text-agora-500">
            <button
              onClick={() => like.mutate()}
              className={`flex items-center gap-1.5 text-sm transition-colors hover:text-red-500 ${post.liked ? 'text-red-500' : ''}`}>
              <Heart size={16} fill={post.liked ? 'currentColor' : 'none'} />
              <span>{post.like_count}</span>
            </button>

            <button
              onClick={() => setShowComments(c => !c)}
              className="flex items-center gap-1.5 text-sm hover:text-agora-700 dark:hover:text-agora-200 transition-colors">
              <MessageCircle size={16} />
              <span>{post.comment_count}</span>
            </button>

            <button
              onClick={() => repost.mutate()}
              className={`flex items-center gap-1.5 text-sm transition-colors hover:text-green-500 ${post.reposted ? 'text-green-500' : ''}`}>
              <Repeat2 size={16} />
              <span>{post.repost_count}</span>
            </button>
          </div>
        </div>
      </div>

      {/* Comments */}
      {showComments && <CommentsSection postId={post.id} postAuthorId={post.author_id} />}
    </div>
  )
}
