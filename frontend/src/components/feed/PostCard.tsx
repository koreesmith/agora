import { useState, useRef, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { MessageCircle, Repeat2, Trash2, Flag, Globe, Users, Lock, MoreHorizontal, X, Pencil, AlertTriangle, ExternalLink, ArrowRight } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { feedApi, friendsApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import { formatDistanceToNow } from 'date-fns'
import CommentsSection, { renderContent } from './CommentsSection'
import ReportModal from './ReportModal'
import { handle } from '../../utils/handle'
import { isGifUrl, isDirectGifUrl } from '../../utils/gif'

// ── Reaction config ───────────────────────────────────────────────────────────

import { REACTIONS, REACTION_MAP } from '../../utils/reactions'

interface Post {
  id: string
  author_id: string
  author_username: string
  author_display_name: string
  author_pronouns: string
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
  group_id?: string
  friend_list_id?: string
  group_name?: string
  group_slug?: string
  repost_of_id?: string
  repost_author_username?: string
  repost_author_display_name?: string
  repost_author_pronouns?: string
  repost_content?: string
  repost_image_url?: string
  like_count: number
  comment_count: number
  repost_count: number
  liked: boolean
  reposted: boolean
  // Reactions (AGORA-25)
  reaction_count: number
  my_reaction: string
  reaction_counts: Record<string, number>
  // Polls (AGORA-5)
  poll_options?: { id: string; text: string; votes: number; position: number }[]
  my_poll_vote?: string
  // Wall (AGORA-19)
  wall_user_id?: string
  wall_username?: string
  wall_display_name?: string
  wall_status?: string
  created_at: string
  edited_at?: string
}

const visIcons: Record<string, React.ReactNode> = {
  public:  <Globe size={12} />,
  friends: <Users size={12} />,
  group:   <Lock size={12} />,
  private: <Lock size={12} />,
}

// ── Reaction Picker ───────────────────────────────────────────────────────────

function ReactionPicker({ onPick, activeReaction }: { onPick: (type: string) => void; activeReaction?: string }) {
  return (
    <div className="absolute bottom-8 left-0 z-30 flex items-center gap-1 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-full px-2 py-1.5 shadow-xl"
      onMouseLeave={e => e.stopPropagation()}
    >
      {REACTIONS.map(r => (
        <button
          key={r.type}
          title={r.type === activeReaction ? `Remove ${r.label}` : r.label}
          onClick={e => { e.stopPropagation(); onPick(r.type) }}
          className={`text-xl leading-none hover:scale-125 transition-transform duration-150 px-0.5 rounded-full ${r.type === activeReaction ? 'bg-agora-100 dark:bg-agora-700 ring-2 ring-agora-400 scale-110' : ''}`}
          style={{ lineHeight: 1 }}
        >
          {r.emoji}
        </button>
      ))}
    </div>
  )
}

// ── Reactions Summary Bar ─────────────────────────────────────────────────────

function ReactionBar({ counts, total, myReaction, onOpenModal }: {
  counts: Record<string, number>
  total: number
  myReaction: string
  onOpenModal: () => void
}) {
  if (total === 0 && !myReaction) return null
  const sorted = Object.entries(counts).filter(([,c]) => c > 0).sort((a,b) => b[1]-a[1])
  return (
    <button
      onClick={onOpenModal}
      className="flex items-center gap-1 mt-1 px-1 hover:bg-agora-50 dark:hover:bg-agora-700/50 rounded-full transition-colors"
    >
      <span className="flex -space-x-0.5">
        {sorted.slice(0,3).map(([type]) => (
          <span key={type} className="text-sm leading-none">{REACTION_MAP[type]?.emoji}</span>
        ))}
      </span>
      {total > 0 && (
        <span className="text-xs text-agora-500 dark:text-agora-400 ml-0.5">{total}</span>
      )}
    </button>
  )
}

// ── Reactions Synopsis Modal ──────────────────────────────────────────────────

function ReactionsModal({ postId, onClose }: { postId: string; onClose: () => void }) {
  const [activeTab, setActiveTab] = useState<string>('all')
  const { data } = useQuery({
    queryKey: ['reactions', postId],
    queryFn: () => feedApi.getReactions(postId).then(r => r.data),
  })
  const reactions: any[] = data?.reactions || []
  const counts: Record<string, number> = data?.counts || {}
  const total: number = data?.total || 0

  const tabs = [
    { key: 'all', label: `All ${total}` },
    ...REACTIONS.filter(r => counts[r.type]).map(r => ({
      key: r.type,
      label: `${r.emoji} ${counts[r.type]}`,
    })),
  ]

  const filtered = activeTab === 'all' ? reactions : reactions.filter(r => r.type === activeTab)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60"
      onClick={onClose}>
      <div className="bg-white dark:bg-agora-800 rounded-2xl shadow-2xl w-full max-w-sm"
        onClick={e => e.stopPropagation()}>
        {/* Header */}
        <div className="flex items-center justify-between px-4 pt-4 pb-2">
          <h3 className="font-semibold text-agora-900 dark:text-agora-100">Reactions</h3>
          <button onClick={onClose} className="text-agora-400 hover:text-agora-600 transition-colors">
            <X size={18} />
          </button>
        </div>
        {/* Tab strip */}
        <div className="flex gap-1 px-3 pb-2 overflow-x-auto border-b border-agora-100 dark:border-agora-700">
          {tabs.map(t => (
            <button
              key={t.key}
              onClick={() => setActiveTab(t.key)}
              className={`flex-shrink-0 px-3 py-1 rounded-full text-sm transition-colors ${
                activeTab === t.key
                  ? 'bg-agora-600 text-white'
                  : 'text-agora-500 hover:bg-agora-100 dark:hover:bg-agora-700'
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
        {/* List */}
        <div className="max-h-72 overflow-y-auto py-2">
          {filtered.length === 0
            ? <p className="text-center text-sm text-agora-400 py-6">No reactions yet</p>
            : filtered.map((r: any) => (
                <div key={r.user_id} className="flex items-center gap-3 px-4 py-2 hover:bg-agora-50 dark:hover:bg-agora-700/50">
                  <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                    {r.avatar_url
                      ? <img src={r.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">
                          {(r.display_name || r.username)[0].toUpperCase()}
                        </span>
                    }
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-agora-900 dark:text-agora-100 truncate">
                      {r.display_name || r.username}
                    </p>
                    <p className="text-xs text-agora-400">@{r.username}</p>
                  </div>
                  <span className="text-lg leading-none">{REACTION_MAP[r.type]?.emoji}</span>
                </div>
              ))
          }
        </div>
      </div>
    </div>
  )
}

// ── PostCard ──────────────────────────────────────────────────────────────────

export default function PostCard({ post, invalidateKey = 'feed' }: { post: Post, invalidateKey?: string }) {
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null)
  const [twExpanded, setTwExpanded] = useState(false)
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [showComments, setShowComments] = useState(false)
  const [showMenu, setShowMenu] = useState(false)
  const [showReport, setShowReport] = useState(false)
  const [showReactionPicker, setShowReactionPicker] = useState(false)
  const [showReactionsModal, setShowReactionsModal] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editContent, setEditContent] = useState(post.content)
  const [editVisibility, setEditVisibility] = useState(post.visibility)
  const [editFriendListId, setEditFriendListId] = useState(post.friend_list_id || '')
  const [editTwEnabled, setEditTwEnabled] = useState(!!post.content_warning)
  const [editTwLabel, setEditTwLabel] = useState(post.content_warning || '')

  const { data: groupsData } = useQuery({
    queryKey: ['friend-groups'],
    queryFn: () => friendsApi.listGroups().then(r => r.data),
    enabled: editing && !post.group_id,
  })
  const friendLists: any[] = groupsData?.groups || []

  const invalidate = () => qc.invalidateQueries({ queryKey: [invalidateKey] })

  // Optimistic reaction state — holds until the refetched prop catches up
  // undefined = use post.my_reaction; null = optimistically removed; string = optimistically set
  const [optimisticReaction, setOptimisticReaction] = useState<string | null | undefined>(undefined)
  const myReaction = optimisticReaction !== undefined ? (optimisticReaction ?? '') : (post.my_reaction ?? '')

  // Once the server-returned prop matches the optimistic value, clear the override
  useEffect(() => {
    if (optimisticReaction === null && !post.my_reaction) setOptimisticReaction(undefined)
    if (typeof optimisticReaction === 'string' && post.my_reaction === optimisticReaction) setOptimisticReaction(undefined)
  }, [post.my_reaction])

  // Reaction mutation — picks or removes
  const react = useMutation({
    mutationFn: (type: string | null) =>
      type ? feedApi.reactPost(post.id, type) : feedApi.unreactPost(post.id),
    onSettled: () => invalidate(),
  })

  const handleReactionPick = (type: string) => {
    setShowReactionPicker(false)
    if (myReaction === type) {
      setOptimisticReaction(null)
      react.mutate(null)
    } else {
      setOptimisticReaction(type)
      react.mutate(type)
    }
  }

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

  const pollVote = useMutation({
    mutationFn: (optionId: string | null) =>
      optionId ? feedApi.pollVote(post.id, optionId) : feedApi.pollUnvote(post.id),
    onSettled: invalidate,
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

      {/* Wall post banner */}
      {post.wall_user_id && (
        <div className="flex items-center gap-1.5 text-xs text-agora-500 dark:text-agora-400 mb-3">
          <ArrowRight size={13} />
          <Link to={`/profile/${post.author_username}`} className="font-medium hover:underline">
            {post.author_display_name || post.author_username}
          </Link>
          <span>→</span>
          <Link to={`/profile/${post.wall_username}`} className="font-medium hover:underline">
            {post.wall_display_name || post.wall_username}'s wall
          </Link>
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
              {(() => {
                const pronouns = post.repost_of_id ? post.repost_author_pronouns : post.author_pronouns
                return pronouns ? (
                  <span className="text-agora-400 dark:text-agora-500 text-xs">({pronouns})</span>
                ) : null
              })()}
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
                autoComplete="off"
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
                    autoComplete="off"
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
                const isDirectGif = isDirectGifUrl(url!)
                const isGif = isDirectGif || isGifUrl(url!)
                // If it's a GIF share page URL (not direct media), show as a link instead
                if (isGif && !isDirectGif) {
                  return (
                    <a href={url} target="_blank" rel="noopener noreferrer"
                      className="mt-2 flex items-center gap-2 border border-agora-200 dark:border-agora-700 rounded-xl px-3 py-2 hover:bg-agora-50 dark:hover:bg-agora-900/40 transition-colors">
                      <span className="text-lg">🎞️</span>
                      <div>
                        <p className="text-xs text-agora-500">GIF</p>
                        <p className="text-sm text-agora-700 dark:text-agora-300 truncate">{url}</p>
                      </div>
                    </a>
                  )
                }
                return isDirectGif ? (
                  // GIFs: display inline, no lightbox (animation would pause in lightbox)
                  <div className="mt-2 rounded-lg overflow-hidden">
                    <img
                      src={url}
                      alt=""
                      className="w-full max-h-[32rem] object-contain rounded-lg"
                    />
                  </div>
                ) : (
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

          {/* Poll */}
          {post.poll_options && post.poll_options.length >= 2 && (() => {
            const opts = post.poll_options!
            const totalVotes = opts.reduce((s, o) => s + o.votes, 0)
            const hasVoted = !!post.my_poll_vote
            return (
              <div className="mt-3 space-y-2">
                {opts.map(opt => {
                  const isMyVote = post.my_poll_vote === opt.id
                  const pct = totalVotes > 0 ? Math.round((opt.votes / totalVotes) * 100) : 0
                  return hasVoted ? (
                    // Results view
                    <button
                      key={opt.id}
                      onClick={() => pollVote.mutate(isMyVote ? null : opt.id)}
                      className={`w-full text-left rounded-lg overflow-hidden border transition-colors ${
                        isMyVote
                          ? 'border-agora-500 dark:border-agora-400'
                          : 'border-agora-200 dark:border-agora-600'
                      }`}
                    >
                      <div className="relative px-3 py-2">
                        <div
                          className={`absolute inset-0 ${isMyVote ? 'bg-agora-100 dark:bg-agora-700' : 'bg-agora-50 dark:bg-agora-800/50'}`}
                          style={{ width: `${pct}%` }}
                        />
                        <div className="relative flex items-center justify-between gap-2">
                          <span className={`text-sm ${isMyVote ? 'font-semibold text-agora-800 dark:text-agora-100' : 'text-agora-700 dark:text-agora-300'}`}>
                            {isMyVote && <span className="mr-1">✓</span>}{opt.text}
                          </span>
                          <span className="text-xs text-agora-500 flex-shrink-0">{pct}%</span>
                        </div>
                      </div>
                    </button>
                  ) : (
                    // Voting view
                    <button
                      key={opt.id}
                      onClick={() => pollVote.mutate(opt.id)}
                      disabled={pollVote.isPending}
                      className="w-full text-left px-3 py-2 rounded-lg border border-agora-200 dark:border-agora-600 hover:border-agora-500 dark:hover:border-agora-400 hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors text-sm text-agora-700 dark:text-agora-300 disabled:opacity-50"
                    >
                      {opt.text}
                    </button>
                  )
                })}
                <p className="text-xs text-agora-400">
                  {totalVotes} {totalVotes === 1 ? 'vote' : 'votes'}
                  {hasVoted && <button onClick={() => pollVote.mutate(null)} className="ml-2 underline hover:text-agora-600">Remove vote</button>}
                </p>
              </div>
            )
          })()}

          {/* Actions */}
          <div className="mt-3">
            <div className="flex items-center gap-4 text-agora-400 dark:text-agora-500">
              {/* Reaction button + picker */}
              <div className="relative">
                <button
                  onClick={() => myReaction ? handleReactionPick(myReaction) : setShowReactionPicker(p => !p)}
                  onContextMenu={e => { e.preventDefault(); setShowReactionPicker(p => !p) }}
                  className={`flex items-center gap-1.5 text-sm transition-colors hover:text-red-500 ${myReaction ? 'text-red-500' : ''}`}
                  title={myReaction ? 'Click to remove · Right-click to change' : 'React'}
                >
                  <span className="text-base leading-none" style={{lineHeight:1}}>
                    {myReaction ? REACTION_MAP[myReaction]?.emoji : '🤍'}
                  </span>
                  <span className="text-sm">{post.reaction_count || ''}</span>
                </button>
                {showReactionPicker && (
                  <ReactionPicker onPick={handleReactionPick} activeReaction={myReaction || undefined} />
                )}
              </div>

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

            {/* Reaction summary bar */}
            <ReactionBar
              counts={post.reaction_counts || {}}
              total={post.reaction_count || 0}
              myReaction={myReaction}
              onOpenModal={() => setShowReactionsModal(true)}
            />
          </div>
        </div>
      </div>

      {/* Reactions synopsis modal */}
      {showReactionsModal && (
        <ReactionsModal postId={post.id} onClose={() => setShowReactionsModal(false)} />
      )}

      {/* Comments */}
      {showComments && <CommentsSection postId={post.id} postAuthorId={post.author_id} />}
    </div>
  )
}
