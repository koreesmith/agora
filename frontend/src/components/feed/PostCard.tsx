import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Heart, MessageCircle, Repeat2, Trash2, Flag, Globe, Users, Lock, MoreHorizontal, X, Pencil, AlertTriangle, ExternalLink } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { feedApi, friendsApi } from '../../api'
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
  content_warning: string
  link_url: string
  link_title: string
  link_description: string
  link_image: string
  link_domain: string
  group_id?: string        // community group id
  friend_list_id?: string  // friend list id (when visibility=group)
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
  const [twExpanded, setTwExpanded] = useState(false)
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [showComments, setShowComments] = useState(false)
  const [showMenu, setShowMenu] = useState(false)
  const [showReport, setShowReport] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editContent, setEditContent] = useState(post.content)
  const [editVisibility, setEditVisibility] = useState(post.visibility)
  const [editFriendListId, setEditFriendListId] = useState(post.friend_list_id || '')
  const [editTwEnabled, setEditTwEnabled] = useState(!!post.content_warning)
  const [editTwLabel, setEditTwLabel] = useState(post.content_warning || '')

  // Only fetch friend lists when the edit UI is open
  const { data: groupsData } = useQuery({
    queryKey: ['friend-groups'],
    queryFn: () => friendsApi.listGroups().then(r => r.data),
    enabled: editing && !post.group_id, // don't fetch for community group posts
  })
  const friendLists: any[] = groupsData?.groups || []

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
    mutationFn: () => feedApi.editPost(post.id, {
      content: editContent,
      visibility: editVisibility,
      friend_list_id: editVisibility === 'group' ? editFriendListId : undefined,
      content_warning: editTwEnabled ? editTwLabel : '',
    }),
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
                    <button onClick={() => { setEditing(true); setEditContent(post.content); setEditVisibility(post.visibility); setEditFriendListId(post.friend_list_id || ''); setEditTwEnabled(!!post.content_warning); setEditTwLabel(post.content_warning || ''); setShowMenu(false) }}
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

          {/* Trigger warning banner — shown when post has a content warning and not yet expanded */}
          {post.content_warning && !editing && !twExpanded && (
            <div className="mt-3 flex items-center gap-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-700 rounded-xl px-4 py-3">
              <AlertTriangle size={18} className="text-amber-500 flex-shrink-0" />
              <div className="flex-1 min-w-0">
                <p className="text-sm font-semibold text-amber-800 dark:text-amber-300">Trigger Warning</p>
                <p className="text-xs text-amber-600 dark:text-amber-400 truncate">{post.content_warning}</p>
              </div>
              <button
                onClick={() => setTwExpanded(true)}
                className="flex-shrink-0 text-xs font-medium text-amber-700 dark:text-amber-400 border border-amber-300 dark:border-amber-600 rounded-lg px-3 py-1.5 hover:bg-amber-100 dark:hover:bg-amber-900/40 transition-colors"
              >
                Show post
              </button>
            </div>
          )}

          {/* Collapse button when TW post is expanded */}
          {post.content_warning && !editing && twExpanded && (
            <div className="mt-2 flex items-center gap-1.5">
              <AlertTriangle size={12} className="text-amber-400" />
              <span className="text-xs text-amber-500">{post.content_warning}</span>
              <button onClick={() => setTwExpanded(false)} className="ml-auto text-xs text-agora-400 hover:text-agora-600">
                Hide
              </button>
            </div>
          )}

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
              {/* Trigger warning toggle in edit mode */}
              <div className="flex items-center gap-2">
                <button
                  onClick={() => { setEditTwEnabled(v => !v); if (editTwEnabled) setEditTwLabel('') }}
                  className={`flex items-center gap-1.5 px-2 py-1 rounded-lg text-xs font-medium border transition-colors ${
                    editTwEnabled
                      ? 'bg-amber-100 dark:bg-amber-900/30 border-amber-400 text-amber-700 dark:text-amber-400'
                      : 'border-agora-200 dark:border-agora-600 text-agora-400 hover:border-amber-400 hover:text-amber-500'
                  }`}
                >
                  <AlertTriangle size={12} /> TW
                </button>
                {editTwEnabled && (
                  <input
                    className="flex-1 input text-xs py-1"
                    placeholder="Describe the trigger…"
                    value={editTwLabel}
                    onChange={e => setEditTwLabel(e.target.value)}
                    maxLength={120}
                  />
                )}
              </div>

              {/* Visibility picker — hidden for community group posts */}
              {!post.group_id && (
                <div className="space-y-1.5">
                  <div className="flex gap-1.5">
                    {([
                      ['public',  'public',  <Globe size={12} />,  'Public'],
                      ['friends', 'friends', <Users size={12} />,  'Friends'],
                      ['group',   'group',   <Lock  size={12} />,  'Friend List'],
                      ['private', 'private', <Lock  size={12} />,  'Private'],
                    ] as [string, string, React.ReactNode, string][]).map(([key, val, icon, label]) => (
                      <button
                        key={key}
                        onClick={() => setEditVisibility(val)}
                        className={`flex items-center gap-1 px-2 py-1 rounded text-xs border transition-colors ${
                          editVisibility === val
                            ? 'border-agora-600 bg-agora-50 dark:bg-agora-700 text-agora-700 dark:text-agora-200'
                            : 'border-agora-200 dark:border-agora-600 text-agora-400 hover:border-agora-400'
                        }`}
                      >
                        {icon} {label}
                      </button>
                    ))}
                  </div>
                  {/* Friend list selector — shown when visibility=group */}
                  {editVisibility === 'group' && (
                    friendLists.length > 0
                      ? <select
                          value={editFriendListId}
                          onChange={e => setEditFriendListId(e.target.value)}
                          className="text-xs bg-transparent text-agora-600 dark:text-agora-300 border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1.5 w-full focus:outline-none"
                        >
                          <option value="">Select a list…</option>
                          {friendLists.map((g: any) => (
                            <option key={g.id} value={g.id}>{g.name}</option>
                          ))}
                        </select>
                      : <p className="text-xs text-agora-400">
                          No friend lists yet — <Link to="/friends" className="underline">create one</Link>
                        </p>
                  )}
                </div>
              )}
              <div className="flex gap-2 justify-end">
                <button onClick={() => setEditing(false)} className="btn-secondary text-xs py-1 px-3">Cancel</button>
                <button
                  onClick={() => edit.mutate()}
                  disabled={edit.isPending || (!editContent.trim() && !post.image_url) || (editVisibility === 'group' && !editFriendListId)}
                  className="btn-primary text-xs py-1 px-3"
                >
                  {edit.isPending ? 'Saving…' : 'Save'}
                </button>
              </div>
            </div>
          ) : (
            <>
              {/* Content — hidden behind TW gate until expanded */}
              {(!post.content_warning || twExpanded) && (
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

              {/* Link preview card */}
              {post.link_url && !post.repost_of_id && (
                <a
                  href={post.link_url}
                  target="_blank"
                  rel="noreferrer"
                  className="mt-2 flex gap-3 border border-agora-200 dark:border-agora-600 rounded-xl overflow-hidden hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors"
                  onClick={e => e.stopPropagation()}
                >
                  {post.link_image && (
                    <img
                      src={post.link_image}
                      alt=""
                      className="w-20 h-20 object-cover flex-shrink-0 bg-agora-100 dark:bg-agora-700"
                      onError={e => { (e.target as HTMLImageElement).style.display = 'none' }}
                    />
                  )}
                  <div className="flex-1 min-w-0 p-3 space-y-0.5">
                    <p className="text-xs text-agora-400 flex items-center gap-1">
                      <ExternalLink size={10} /> {post.link_domain}
                    </p>
                    {post.link_title && (
                      <p className="text-sm font-semibold line-clamp-2 text-agora-800 dark:text-agora-200">
                        {post.link_title}
                      </p>
                    )}
                    {post.link_description && (
                      <p className="text-xs text-agora-500 line-clamp-2">{post.link_description}</p>
                    )}
                  </div>
                </a>
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
