import { useState, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { feedApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import { formatDistanceToNow } from 'date-fns'
import { Trash2, Send, Pencil, Reply, Image, X as XIcon } from 'lucide-react'
import { useMentions } from './useMentions'
import MentionDropdown from './MentionDropdown'

// ── Reaction config ───────────────────────────────────────────────────────────

const REACTIONS = [
  { type: 'like',     emoji: '❤️',  label: 'Like'     },
  { type: 'love',     emoji: '😍',  label: 'Love'     },
  { type: 'laugh',    emoji: '😂',  label: 'Laugh'    },
  { type: 'angry',    emoji: '😡',  label: 'Angry'    },
  { type: 'care',     emoji: '🤗',  label: 'Care'     },
  { type: 'pride',    emoji: '🏳️‍🌈', label: 'Pride'    },
  { type: 'thankful', emoji: '🙏',  label: 'Thankful' },
  { type: 'vomit',    emoji: '🤮',  label: 'Vomit'    },
]
const REACTION_MAP: Record<string, { emoji: string; label: string }> = Object.fromEntries(
  REACTIONS.map(r => [r.type, r])
)

function CommentReactionPicker({ onPick, activeReaction }: { onPick: (type: string) => void; activeReaction?: string }) {
  return (
    <div className="absolute bottom-6 left-0 z-30 flex items-center gap-1 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-full px-2 py-1 shadow-xl">
      {REACTIONS.map(r => (
        <button
          key={r.type}
          title={r.type === activeReaction ? `Remove ${r.label}` : r.label}
          onClick={e => { e.stopPropagation(); onPick(r.type) }}
          className={`text-base leading-none hover:scale-125 transition-transform duration-150 px-0.5 rounded-full ${r.type === activeReaction ? 'bg-agora-100 dark:bg-agora-700 ring-2 ring-agora-400 scale-110' : ''}`}
          style={{ lineHeight: 1 }}
        >
          {r.emoji}
        </button>
      ))}
    </div>
  )
}

// ── Comment Reactions Modal ───────────────────────────────────────────────────

function CommentReactionsModal({ commentId, onClose }: { commentId: string; onClose: () => void }) {
  const [activeTab, setActiveTab] = useState<string>('all')
  const { data } = useQuery({
    queryKey: ['reactions', commentId],
    queryFn: () => feedApi.getReactions(commentId).then(r => r.data),
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
  const filtered = activeTab === 'all' ? reactions : reactions.filter((r: any) => r.type === activeTab)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60"
      onClick={onClose}>
      <div className="bg-white dark:bg-agora-800 rounded-2xl shadow-2xl w-full max-w-sm"
        onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-4 pt-4 pb-2">
          <h3 className="font-semibold text-agora-900 dark:text-agora-100">Reactions</h3>
          <button onClick={onClose} className="text-agora-400 hover:text-agora-600 transition-colors">
            <XIcon size={18} />
          </button>
        </div>
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

// Render text with @mentions as profile links and URLs as clickable links
export function renderContent(text: string) {
  const parts = text.split(/(https?:\/\/[^\s<>"{}|\\^`[\]]+|@[a-zA-Z0-9_-]+)/g)
  return parts.map((part, i) => {
    if (/^@[a-zA-Z0-9_-]+$/.test(part)) {
      return <Link key={i} to={`/profile/${part.slice(1)}`} className="text-agora-600 dark:text-agora-400 hover:underline font-medium">{part}</Link>
    }
    if (/^https?:\/\//i.test(part)) {
      const url = part.replace(/[.,!?)]+$/, '')
      const trailing = part.slice(url.length)
      return (
        <span key={i}>
          <a href={url} target="_blank" rel="noreferrer noopener" className="text-agora-600 dark:text-agora-400 hover:underline break-all">{url}</a>
          {trailing}
        </span>
      )
    }
    return <span key={i}>{part}</span>
  })
}

export default function CommentsSection({ postId, postAuthorId }: { postId: string, postAuthorId: string }) {
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [text, setText] = useState('')
  const [imageUrl, setImageUrl] = useState('')
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const { mentionUsers, showMentions, handleChange, insertMention, dismiss, inputRef } = useMentions()

  const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setUploading(true)
    try { const res = await feedApi.uploadMedia(file, 'posts'); setImageUrl(res.data.url) }
    catch (err: any) { alert(err?.response?.data?.error || 'Upload failed') }
    finally { setUploading(false); if (fileRef.current) fileRef.current.value = '' }
  }

  const { data } = useQuery({
    queryKey: ['comments', postId],
    queryFn: () => feedApi.getComments(postId).then(r => r.data),
  })

  const create = useMutation({
    mutationFn: () => feedApi.createComment(postId, { content: text, image_url: imageUrl }),
    onSuccess: () => {
      setText('')
      setImageUrl('')
      qc.invalidateQueries({ queryKey: ['comments', postId] })
      qc.invalidateQueries({ queryKey: ['feed'] })
    },
  })

  const del = useMutation({
    mutationFn: (commentId: string) => feedApi.deleteComment(postId, commentId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['comments', postId] }),
  })

  const reactComment = useMutation({
    mutationFn: ({ id, type }: { id: string; type: string | null }) =>
      type ? feedApi.reactPost(id, type) : feedApi.unreactPost(id),
    onSettled: () => qc.invalidateQueries({ queryKey: ['comments', postId] }),
  })

  const invalidate = () => qc.invalidateQueries({ queryKey: ['comments', postId] })
  const comments = data?.comments || []

  const handleReact = (id: string, myReaction: string, type: string) => {
    reactComment.mutate({ id, type: myReaction === type ? null : type })
  }

  return (
    <div className="mt-4 pt-4 border-t border-agora-100 dark:border-agora-700 space-y-3">
      {comments.map((c: any) => (
        <div key={c.id}>
          <CommentRow
            comment={c}
            postId={postId}
            postAuthorId={postAuthorId}
            currentUserId={user?.id}
            currentUserRole={user?.role}
            onDelete={() => del.mutate(c.id)}
            onReact={(type) => handleReact(c.id, c.my_reaction, type)}
            onEdited={invalidate}
            onReplyCreated={invalidate}
            depth={0}
          />

          {c.replies?.length > 0 && (
            <div className="ml-10 mt-2 space-y-2 border-l-2 border-agora-100 dark:border-agora-700 pl-3">
              {c.replies.map((reply: any) => (
                <div key={reply.id}>
                  <CommentRow
                    comment={reply}
                    postId={postId}
                    postAuthorId={postAuthorId}
                    currentUserId={user?.id}
                    currentUserRole={user?.role}
                    onDelete={() => del.mutate(reply.id)}
                    onReact={(type) => handleReact(reply.id, reply.my_reaction, type)}
                    onEdited={invalidate}
                    onReplyCreated={invalidate}
                    depth={1}
                  />
                  {reply.replies?.length > 0 && (
                    <div className="ml-8 mt-2 space-y-2 border-l-2 border-agora-100 dark:border-agora-700 pl-3">
                      {reply.replies.map((r2: any) => (
                        <CommentRow
                          key={r2.id}
                          comment={r2}
                          postId={postId}
                          postAuthorId={postAuthorId}
                          currentUserId={user?.id}
                          currentUserRole={user?.role}
                          onDelete={() => del.mutate(r2.id)}
                          onReact={(type) => handleReact(r2.id, r2.my_reaction, type)}
                          onEdited={invalidate}
                          onReplyCreated={invalidate}
                          depth={2}
                        />
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      ))}

      <div className="flex gap-2 pt-1">
        <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {user?.avatar_url
            ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{user?.username?.[0]?.toUpperCase()}</span>}
        </div>
        <div className="flex-1 space-y-1.5">
          {imageUrl && (
            <div className="relative">
              <img src={imageUrl} alt="" className="rounded-lg max-h-40 object-contain bg-agora-50 dark:bg-agora-900" />
              <button onClick={() => setImageUrl('')} className="absolute top-1 right-1 bg-black/60 text-white rounded-full w-5 h-5 flex items-center justify-center hover:bg-black/80">
                <XIcon size={10} />
              </button>
            </div>
          )}
          <div className="flex gap-1.5 relative">
            <label className={`flex-shrink-0 p-1.5 rounded-lg text-agora-400 hover:text-agora-600 hover:bg-agora-100 dark:hover:bg-agora-700 cursor-pointer transition-colors ${uploading ? 'opacity-50 pointer-events-none' : ''}`} title="Add image">
              <Image size={16} />
              <input ref={fileRef} type="file" accept="image/*" className="hidden" onChange={handleImageUpload} disabled={uploading || !!imageUrl} />
            </label>
            <input
              ref={inputRef as React.RefObject<HTMLInputElement>}
              className="input flex-1 text-sm py-1.5"
              placeholder="Write a comment… use @username to tag"
              autoComplete="off"
              value={text}
              onChange={e => { setText(e.target.value); handleChange(e.target.value, e.target.selectionStart ?? e.target.value.length) }}
              onKeyDown={e => {
                if (e.key === 'Escape') { dismiss(); return }
                if (e.key === 'Enter' && !e.shiftKey && (text.trim() || imageUrl) && !showMentions) create.mutate()
              }}
            />
            {showMentions && <MentionDropdown users={mentionUsers} onSelect={u => insertMention(text, setText, u)} />}
            <button onClick={() => create.mutate()} disabled={(!text.trim() && !imageUrl) || create.isPending || uploading} className="btn-primary px-3 py-1.5">
              {uploading ? <span className="text-xs">…</span> : <Send size={14} />}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

// ── Comment Row ────────────────────────────────────────────────────────────────

function CommentRow({ comment: c, postId, postAuthorId, currentUserId, currentUserRole, onDelete, onReact, onEdited, onReplyCreated, depth }: {
  comment: any
  postId: string
  postAuthorId: string
  currentUserId?: string
  currentUserRole?: string
  onDelete: () => void
  onReact: (type: string) => void
  onEdited: () => void
  onReplyCreated: () => void
  depth: number
}) {
  const { user } = useAuthStore()
  const [editing, setEditing] = useState(false)
  const [editContent, setEditContent] = useState(c.content)
  const [showReplyBox, setShowReplyBox] = useState(false)
  const [replyText, setReplyText] = useState('')
  const [replyImageUrl, setReplyImageUrl] = useState('')
  const [replyUploading, setReplyUploading] = useState(false)
  const [showReactionPicker, setShowReactionPicker] = useState(false)
  const replyFileRef = useRef<HTMLInputElement>(null)
  const { mentionUsers, showMentions, handleChange, insertMention, dismiss, inputRef } = useMentions()

  const handleReplyImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setReplyUploading(true)
    try { const res = await feedApi.uploadMedia(file, 'posts'); setReplyImageUrl(res.data.url) }
    catch (err: any) { alert(err?.response?.data?.error || 'Upload failed') }
    finally { setReplyUploading(false); if (replyFileRef.current) replyFileRef.current.value = '' }
  }

  const editMutation = useMutation({
    mutationFn: () => feedApi.editComment(postId, c.id, editContent),
    onSuccess: () => { setEditing(false); onEdited() },
  })

  const replyMutation = useMutation({
    mutationFn: () => feedApi.createComment(postId, {
      content: replyText,
      image_url: replyImageUrl,
      reply_to_id: c.id,
    }),
    onSuccess: () => {
      setReplyText('')
      setReplyImageUrl('')
      setShowReplyBox(false)
      onReplyCreated()
    },
  })

  const openReply = () => {
    setReplyText(`@${c.username} `)
    setReplyImageUrl('')
    setShowReplyBox(true)
  }

  const handlePickReaction = (type: string) => {
    setShowReactionPicker(false)
    onReact(type)
  }

  const isOwn = c.author_id === currentUserId
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null)
  const [showReactionsModal, setShowReactionsModal] = useState(false)
  const canDelete = isOwn || currentUserId === postAuthorId || currentUserRole === 'admin'
  const avatarSize = depth === 0 ? 'w-8 h-8' : depth === 1 ? 'w-6 h-6' : 'w-5 h-5'
  const myReaction: string = c.my_reaction || ''
  const reactionCount: number = c.reaction_count || 0

  return (
    <div className="flex gap-2">
      <Link to={`/profile/${c.username}`} className="flex-shrink-0">
        <div className={`${avatarSize} rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden`}>
          {c.avatar_url
            ? <img src={c.avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">
                {(c.display_name || c.username)[0].toUpperCase()}
              </span>}
        </div>
      </Link>
      <div className="flex-1 min-w-0">
        <div className="bg-agora-50 dark:bg-agora-700/50 rounded-xl px-3 py-2">
          <Link to={`/profile/${c.username}`} className="text-xs font-semibold text-agora-800 dark:text-agora-200 hover:underline">
            {c.display_name || c.username}
          </Link>
          {c.pronouns && (
            <span className="text-agora-400 dark:text-agora-500 text-xs ml-1">({c.pronouns})</span>
          )}
          {editing ? (
            <div className="mt-1 space-y-1.5">
              <textarea
                className="w-full bg-white dark:bg-agora-800 rounded-lg border border-agora-200 dark:border-agora-600 px-2 py-1 text-sm resize-none focus:outline-none focus:ring-1 focus:ring-agora-400"
                rows={2}
                autoComplete="off"
                value={editContent}
                onChange={e => setEditContent(e.target.value)}
                autoFocus
              />
              <div className="flex gap-1.5 justify-end">
                <button onClick={() => setEditing(false)} className="text-xs text-agora-400 hover:text-agora-600 px-2 py-0.5">Cancel</button>
                <button
                  onClick={() => editMutation.mutate()}
                  disabled={editMutation.isPending || !editContent.trim()}
                  className="text-xs bg-agora-600 text-white rounded-md px-2 py-0.5 hover:bg-agora-700 disabled:opacity-50"
                >
                  {editMutation.isPending ? 'Saving…' : 'Save'}
                </button>
              </div>
            </div>
          ) : (
            <>
              <p className="text-sm text-agora-700 dark:text-agora-300 mt-0.5 break-words">{renderContent(c.content)}</p>
              {c.image_url && (
                <>
                  <img
                    src={c.image_url}
                    alt=""
                    className="mt-1 rounded-lg max-h-64 object-contain bg-agora-50 dark:bg-agora-900 cursor-zoom-in"
                    onClick={() => setLightboxUrl(c.image_url)}
                  />
                  {lightboxUrl === c.image_url && (
                    <div
                      className="fixed inset-0 z-50 bg-black/90 flex items-center justify-center"
                      onClick={() => setLightboxUrl(null)}
                    >
                      <button
                        onClick={() => setLightboxUrl(null)}
                        className="absolute top-4 right-4 bg-black/40 text-white rounded-full p-1.5 hover:bg-black/70"
                      >
                        <XIcon size={20} />
                      </button>
                      <img
                        src={c.image_url}
                        alt=""
                        className="max-w-[90vw] max-h-[90vh] object-contain rounded-lg shadow-2xl"
                        onClick={e => e.stopPropagation()}
                      />
                    </div>
                  )}
                </>
              )}
            </>
          )}
        </div>

        {/* Action row */}
        <div className="flex items-center gap-3 mt-1 px-1">
          <span className="text-xs text-agora-400">
            {formatDistanceToNow(new Date(c.created_at), { addSuffix: true })}
          </span>
          {c.edited_at && <span className="text-xs text-agora-400 italic">edited</span>}

          {/* Reaction button + picker */}
          <div className="relative">
            <button
              onClick={() => myReaction ? (onReact(myReaction)) : setShowReactionPicker(p => !p)}
              onContextMenu={e => { e.preventDefault(); setShowReactionPicker(p => !p) }}
              className={`flex items-center gap-1 text-xs transition-colors ${myReaction ? 'text-red-500' : 'text-agora-400 hover:text-red-400'}`}
              title={myReaction ? 'Click to remove · Right-click to change' : 'React'}
            >
              <span style={{ lineHeight: 1 }}>
                {myReaction ? REACTION_MAP[myReaction]?.emoji : '🤍'}
              </span>
            </button>
            {showReactionPicker && (
              <CommentReactionPicker onPick={handlePickReaction} activeReaction={myReaction || undefined} />
            )}
          </div>
          {reactionCount > 0 && (
            <button
              onClick={() => setShowReactionsModal(true)}
              className="text-xs text-agora-400 hover:text-agora-600 transition-colors"
            >
              {reactionCount}
            </button>
          )}
          {showReactionsModal && (
            <CommentReactionsModal commentId={c.id} onClose={() => setShowReactionsModal(false)} />
          )}

          {depth < 2 && (
            <button
              onClick={openReply}
              className="flex items-center gap-1 text-xs text-agora-400 hover:text-agora-600 transition-colors"
            >
              <Reply size={12} /> Reply
            </button>
          )}

          {isOwn && !editing && (
            <button
              onClick={() => { setEditing(true); setEditContent(c.content) }}
              className="text-xs text-agora-300 hover:text-agora-500 transition-colors"
              title="Edit"
            >
              <Pencil size={12} />
            </button>
          )}

          {canDelete && (
            <button onClick={onDelete} className="text-xs text-agora-300 hover:text-red-500 transition-colors">
              <Trash2 size={12} />
            </button>
          )}
        </div>

        {showReplyBox && (
          <div className="flex gap-2 mt-2">
            <div className="w-6 h-6 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
              {user?.avatar_url
                ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{user?.username?.[0]?.toUpperCase()}</span>}
            </div>
            <div className="flex-1 space-y-1.5">
              {replyImageUrl && (
                <div className="relative">
                  <img src={replyImageUrl} alt="" className="rounded-lg max-h-32 object-contain bg-agora-50 dark:bg-agora-900" />
                  <button onClick={() => setReplyImageUrl('')} className="absolute top-1 right-1 bg-black/60 text-white rounded-full w-5 h-5 flex items-center justify-center hover:bg-black/80">
                    <XIcon size={10} />
                  </button>
                </div>
              )}
              <div className="flex gap-1.5 relative">
                <label className={`flex-shrink-0 p-1 rounded text-agora-400 hover:text-agora-600 hover:bg-agora-100 dark:hover:bg-agora-700 cursor-pointer transition-colors ${replyUploading ? 'opacity-50 pointer-events-none' : ''}`} title="Add image">
                  <Image size={14} />
                  <input ref={replyFileRef} type="file" accept="image/*" className="hidden" onChange={handleReplyImageUpload} disabled={replyUploading || !!replyImageUrl} />
                </label>
                <input
                  ref={inputRef as React.RefObject<HTMLInputElement>}
                  className="input flex-1 text-sm py-1"
                  placeholder={`Reply to ${c.username}…`}
                  autoComplete="off"
                  value={replyText}
                  autoFocus
                  onChange={e => {
                    setReplyText(e.target.value)
                    handleChange(e.target.value, e.target.selectionStart ?? e.target.value.length)
                  }}
                  onKeyDown={e => {
                    if (e.key === 'Escape') { dismiss(); setShowReplyBox(false); return }
                    if (e.key === 'Enter' && !e.shiftKey && (replyText.trim() || replyImageUrl) && !showMentions) replyMutation.mutate()
                  }}
                />
                {showMentions && <MentionDropdown users={mentionUsers} onSelect={u => insertMention(replyText, setReplyText, u)} />}
                <button
                  onClick={() => { setShowReplyBox(false); setReplyText(''); setReplyImageUrl('') }}
                  className="btn-secondary text-xs px-2 py-1"
                >
                  Cancel
                </button>
                <button
                  onClick={() => replyMutation.mutate()}
                  disabled={(!replyText.trim() && !replyImageUrl) || replyMutation.isPending || replyUploading}
                  className="btn-primary text-xs px-2 py-1"
                >
                  {replyMutation.isPending ? '…' : <Send size={12} />}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
