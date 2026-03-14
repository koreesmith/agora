import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Heart, MessageCircle, Repeat2, Trash2, Flag, Globe, Users, Lock, MoreHorizontal, X, Pencil } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { feedApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import { formatDistanceToNow } from 'date-fns'
import CommentsSection, { renderContent } from './CommentsSection'
import ReportModal from './ReportModal'
import { handle } from '../../utils/handle'

interface Post {
  id: string
  author_id: string
  author_username: string
  author_display_name: string
  author_avatar_url: string
  is_remote?: boolean
  remote_instance?: string
  content: string
  image_url: string
  visibility: string
  group_id?: string
  group_name?: string
  group_slug?: string
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
  edited_at?: string
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
  const [showReport, setShowReport] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editContent, setEditContent] = useState(post.content)

  const invalidate = () => qc.invalidateQueries({ queryKey: [invalidateKey] })

  const like = useMutation({
    mutationFn: () => post.liked ? feedApi.unlikePost(post.id) : feedApi.likePost(post.id),
    onSuccess: invalidate,
  })

  const del = useMutation({
    mutationFn: () => feedApi.deletePost(post.id),
    onSuccess: invalidate,
  })

  const edit = useMutation({
    mutationFn: () => feedApi.editPost(post.id, { content: editContent }),
    onSuccess: () => { setEditing(false); invalidate() },
  })

  const repost = useMutation({
    mutationFn: () => feedApi.repost(post.id),
    onSuccess: invalidate,
  })

  const isOwn = user?.id === post.author_id
  const canDelete = isOwn || user?.role === 'admin' || user?.role === 'moderator'

  return (
    <div className={`card p-4 ${post.group_slug ? 'border-l-4 border-agora-400 dark:border-agora-500' : ''}`}>
      {showReport && (
        <ReportModal postId={post.id} onClose={() => setShowReport(false)} />
      )}
      {/* Group banner */}
      {post.group_slug && (
        <Link
          to={`/groups/${post.group_slug}`}
          className="flex items-center gap-1.5 text-xs font-medium text-agora-600 dark:text-agora-400 bg-agora-50 dark:bg-agora-700/50 rounded-lg px-2.5 py-1.5 mb-3 hover:bg-agora-100 dark:hover:bg-agora-700 transition-colors w-fit"
        >
          <Users size={12} />
          <span>{post.group_name}</span>
          <span className="text-agora-400 dark:text-agora-500">· View in group →</span>
        </Link>
      )}

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
                {handle(post.repost_of_id ? post.repost_author_username! : post.author_username,
                  !post.repost_of_id && post.is_remote, !post.repost_of_id ? post.remote_instance : undefined)}
              </span>
              <span className="text-agora-300 dark:text-agora-600 text-xs">·</span>
              <Link to={`/post/${post.repost_of_id ? post.repost_of_id : post.id}`}
                className="text-agora-400 text-xs hover:underline">
                {formatDistanceToNow(new Date(post.created_at), { addSuffix: true })}
              </Link>
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
                  {isOwn && !post.repost_of_id && (
                    <button onClick={() => { setEditing(true); setEditContent(post.content); setShowMenu(false) }}
                      className="flex items-center gap-2 w-full px-3 py-2 text-sm text-agora-600 dark:text-agora-300 hover:bg-agora-50 dark:hover:bg-agora-700">
                      <Pencil size={14} /> Edit
                    </button>
                  )}
                  {canDelete && (
                    <button onClick={() => { if (confirm('Delete post?')) del.mutate(); setShowMenu(false) }}
                      className="flex items-center gap-2 w-full px-3 py-2 text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20">
                      <Trash2 size={14} /> Delete
                    </button>
                  )}
                  {!isOwn && (
                    <button onClick={() => { setShowReport(true); setShowMenu(false) }}
                      className="flex items-center gap-2 w-full px-3 py-2 text-sm text-agora-600 dark:text-agora-400 hover:bg-agora-50 dark:hover:bg-agora-700">
                      <Flag size={14} /> Report
                    </button>
                  )}
                </div>
              )}
            </div>
          </div>

          {/* Inline editor */}
          {editing ? (
            <div className="mt-2 space-y-2">
              <textarea
                className="input w-full resize-none text-sm"
                rows={3}
                value={editContent}
                onChange={e => setEditContent(e.target.value)}
                autoFocus
              />
              <div className="flex gap-2 justify-end">
                <button onClick={() => setEditing(false)} className="btn-secondary text-xs py-1 px-3">Cancel</button>
                <button
                  onClick={() => edit.mutate()}
                  disabled={edit.isPending || !editContent.trim()}
                  className="btn-primary text-xs py-1 px-3"
                >
                  {edit.isPending ? 'Saving…' : 'Save'}
                </button>
              </div>
            </div>
          ) : (
            <>
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

              {/* Edited label */}
              {post.edited_at && !post.repost_of_id && (
                <p className="text-xs text-agora-400 mt-0.5 italic">
                  edited {formatDistanceToNow(new Date(post.edited_at), { addSuffix: true })}
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
                        className="w-full max-h-[32rem] object-contain rounded-lg hover:opacity-95 transition-opacity cursor-zoom-in bg-agora-50 dark:bg-agora-900"
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
                          className="max-w-[90vw] max-h-[90vh] w-auto h-auto rounded-lg shadow-2xl object-contain"
                          onClick={e => e.stopPropagation()}
                        />
                      </div>
                    )}
                  </>
                )
              })()}
            </>
          )}

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
