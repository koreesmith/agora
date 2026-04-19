import { useState, useEffect, useRef, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useParams, useNavigate } from 'react-router-dom'
import { dmApi, feedApi, friendsApi } from '../api'
import { useAuthStore } from '../store/auth'
import { useWebSocket } from '../hooks/useWebSocket'
import { formatDistanceToNow } from 'date-fns'
import { Send, Image, X, Edit2, Trash2, Check, Search, MessageCircle, Plus, ArrowLeft } from 'lucide-react'
import { isGifUrl } from '../utils/gif'

// ── Types ─────────────────────────────────────────────────────────────────────

interface Participant {
  user_id: string
  username: string
  display_name: string
  avatar_url: string
  last_read_at?: string
  read_receipts: boolean
  last_active_at?: string
  is_online?: boolean
}

interface Message {
  id: string
  conversation_id: string
  author_id: string
  author_username: string
  author_display_name: string
  author_avatar_url: string
  content: string
  image_url: string
  reactions?: { reaction: string; user_id: string; username: string }[]
  edited_at?: string
  deleted_at?: string
  created_at: string
}

interface Conversation {
  id: string
  participants: Participant[]
  last_message?: Message
  unread_count: number
  is_accepted: boolean
  updated_at: string
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function otherParticipant(conv: Conversation, myId: string): Participant | undefined {
  return conv.participants?.find(p => p.user_id !== myId)
}

function Avatar({ user, size = 10, showPresence = false }: { user?: Participant | null; size?: number; showPresence?: boolean }) {
  if (!user) return null
  const cls = `w-${size} h-${size} rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0`
  return (
    <div className="relative flex-shrink-0" style={{ width: `${size * 4}px`, height: `${size * 4}px` }}>
      <div className={cls} style={{ width: '100%', height: '100%' }}>
        {user.avatar_url
          ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
          : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600 text-sm">
              {(user.display_name || user.username)[0].toUpperCase()}
            </span>}
      </div>
      {showPresence && user.is_online && (
        <span className="absolute bottom-0 right-0 w-3 h-3 bg-green-500 border-2 border-white dark:border-agora-900 rounded-full" />
      )}
    </div>
  )
}

// ── New Conversation Modal ────────────────────────────────────────────────────

function NewConvModal({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => void }) {
  const { user } = useAuthStore()
  const [username, setUsername] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [showSuggestions, setShowSuggestions] = useState(false)

  const { data: friendsData } = useQuery({
    queryKey: ['friends'],
    queryFn: () => friendsApi.listFriends().then(r => r.data),
  })
  const friends: any[] = friendsData?.friends || []

  const suggestions = username.trim()
    ? friends.filter(f =>
        f.username.toLowerCase().includes(username.toLowerCase()) ||
        (f.display_name || '').toLowerCase().includes(username.toLowerCase())
      ).slice(0, 6)
    : friends.slice(0, 6)

  const start = useMutation({
    mutationFn: (uname: string) => dmApi.startConversation(uname.trim(), message.trim() || undefined),
    onSuccess: (res) => onCreated(res.data.id),
    onError: (e: any) => setError(e.response?.data?.error || 'Could not start conversation'),
  })

  const handleSelect = (uname: string) => {
    setUsername(uname)
    setShowSuggestions(false)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60" onClick={onClose}>
      <div className="bg-white dark:bg-agora-800 rounded-2xl shadow-2xl w-full max-w-sm p-5 space-y-4" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between">
          <h3 className="font-semibold text-agora-900 dark:text-agora-100">New message</h3>
          <button onClick={onClose} className="text-agora-400 hover:text-agora-600"><X size={18} /></button>
        </div>

        {/* Username input with autocomplete */}
        <div className="relative">
          <input
            className="input w-full"
            autoComplete="off"
            placeholder="Search friends or type a username…"
            value={username}
            onFocus={() => setShowSuggestions(true)}
            onChange={e => { setUsername(e.target.value); setError(''); setShowSuggestions(true) }}
            onKeyDown={e => { if (e.key === 'Escape') setShowSuggestions(false) }}
          />
          {showSuggestions && suggestions.length > 0 && (
            <div className="absolute top-full left-0 right-0 mt-1 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-xl shadow-lg overflow-hidden z-10">
              {suggestions.map((f: any) => (
                <button
                  key={f.id}
                  onClick={() => handleSelect(f.username)}
                  className="w-full flex items-center gap-2.5 px-3 py-2 hover:bg-agora-50 dark:hover:bg-agora-700 text-left"
                >
                  <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                    {f.avatar_url
                      ? <img src={f.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{(f.display_name || f.username)[0].toUpperCase()}</span>}
                  </div>
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{f.display_name || f.username}</p>
                    <p className="text-xs text-agora-400">@{f.username}</p>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>

        <textarea className="input w-full resize-none text-sm" rows={3} autoComplete="off"
          placeholder="Write a message… (optional)" value={message}
          onChange={e => setMessage(e.target.value)} />
        {error && <p className="text-xs text-red-500">{error}</p>}
        <div className="flex gap-2 justify-end">
          <button onClick={onClose} className="btn-secondary text-sm">Cancel</button>
          <button
            onClick={() => { setShowSuggestions(false); start.mutate(username) }}
            disabled={!username.trim() || start.isPending}
            className="btn-primary text-sm"
          >
            {start.isPending ? 'Starting…' : 'Start conversation'}
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Conversation List ─────────────────────────────────────────────────────────

function ConvList({ activeId, onSelect }: { activeId?: string; onSelect: (id: string) => void }) {
  const { user } = useAuthStore()
  const [showNew, setShowNew] = useState(false)
  const [search, setSearch] = useState('')
  const nav = useNavigate()

  const { data, refetch } = useQuery({
    queryKey: ['conversations'],
    queryFn: () => dmApi.listConversations().then(r => r.data),
    refetchInterval: 30000,
  })
  const convs: Conversation[] = data?.conversations || []
  const filtered = convs.filter(c => {
    const other = otherParticipant(c, user!.id)
    if (!other) return false
    const q = search.toLowerCase()
    return !q || other.username.toLowerCase().includes(q) || other.display_name?.toLowerCase().includes(q)
  })

  const requests  = filtered.filter(c => !c.is_accepted)
  const accepted  = filtered.filter(c => c.is_accepted)

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-4 border-b border-agora-100 dark:border-agora-700 flex items-center justify-between">
        <h2 className="font-semibold text-agora-900 dark:text-agora-100">Messages</h2>
        <button onClick={() => setShowNew(true)} className="btn-primary p-1.5 rounded-lg" title="New message">
          <Plus size={16} />
        </button>
      </div>

      {/* Search */}
      <div className="px-3 py-2 border-b border-agora-100 dark:border-agora-700">
        <div className="relative">
          <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-agora-400" />
          <input className="input w-full pl-8 text-sm py-1.5" placeholder="Search conversations…" autoComplete="off"
            value={search} onChange={e => setSearch(e.target.value)} />
        </div>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto">
        {requests.length > 0 && (
          <>
            <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide px-4 pt-3 pb-1">
              Message requests ({requests.length})
            </p>
            {requests.map(c => <ConvRow key={c.id} conv={c} myId={user!.id} active={c.id === activeId} onClick={() => onSelect(c.id)} />)}
          </>
        )}
        {accepted.map(c => <ConvRow key={c.id} conv={c} myId={user!.id} active={c.id === activeId} onClick={() => onSelect(c.id)} />)}
        {filtered.length === 0 && (
          <div className="text-center py-12 text-agora-400 text-sm space-y-2">
            <MessageCircle size={28} className="mx-auto opacity-40" />
            <p>{search ? 'No conversations found.' : 'No messages yet.'}</p>
          </div>
        )}
      </div>

      {showNew && <NewConvModal onClose={() => setShowNew(false)} onCreated={(id) => { setShowNew(false); refetch(); onSelect(id) }} />}
    </div>
  )
}

function ConvRow({ conv, myId, active, onClick }: { conv: Conversation; myId: string; active: boolean; onClick: () => void }) {
  const other = otherParticipant(conv, myId)
  if (!other) return null
  const preview = conv.last_message
    ? conv.last_message.deleted_at ? '(deleted)' : conv.last_message.image_url && !conv.last_message.content ? '📷 Image' : conv.last_message.content
    : 'No messages yet'

  return (
    <button onClick={onClick}
      className={`w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors ${active ? 'bg-agora-50 dark:bg-agora-700/50 border-r-2 border-agora-600' : ''}`}>
      <div className="relative flex-shrink-0">
        <Avatar user={other} size={10} showPresence />
        {conv.unread_count > 0 && (
          <span className="absolute -top-1 -right-1 w-4 h-4 bg-agora-600 text-white text-[10px] font-bold rounded-full flex items-center justify-center z-10">
            {conv.unread_count > 9 ? '9+' : conv.unread_count}
          </span>
        )}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between gap-1">
          <span className={`text-sm truncate ${conv.unread_count > 0 ? 'font-semibold text-agora-900 dark:text-agora-100' : 'font-medium text-agora-700 dark:text-agora-300'}`}>
            {other.display_name || other.username}
          </span>
          {conv.last_message && (
            <span className="text-xs text-agora-400 flex-shrink-0">
              {formatDistanceToNow(new Date(conv.last_message.created_at), { addSuffix: false })}
            </span>
          )}
        </div>
        <p className={`text-xs truncate ${conv.unread_count > 0 ? 'text-agora-700 dark:text-agora-300' : 'text-agora-400'}`}>
          {!conv.is_accepted ? '⚠ Message request' : preview}
        </p>
      </div>
    </button>
  )
}

// ── Message Bubble ────────────────────────────────────────────────────────────

function MessageBubble({ msg, isOwn, onEdit, onDelete, onReact, participants }: {
  msg: Message
  isOwn: boolean
  onEdit: (msg: Message) => void
  onDelete: (id: string) => void
  onReact: (id: string, reaction: string) => void
  participants: Participant[]
}) {
  const [showMenu, setShowMenu] = useState(false)
  const [showReactions, setShowReactions] = useState(false)
  const REACTIONS = ['❤️','😂','😮','😢','👍','👎']

  if (msg.deleted_at) {
    return (
      <div className={`flex ${isOwn ? 'justify-end' : 'justify-start'} mb-1`}>
        <span className="text-xs text-agora-400 italic px-3 py-1.5">Message deleted</span>
      </div>
    )
  }

  // Read receipts: find participants who've read past this message
  const readers = participants.filter(p => p.user_id !== msg.author_id && p.last_read_at && p.last_read_at >= msg.created_at)

  return (
    <div className={`flex items-end gap-2 mb-1 group ${isOwn ? 'flex-row-reverse' : 'flex-row'}`}>
      {/* Reactions picker trigger */}
      <div className={`flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity ${isOwn ? 'flex-row-reverse' : 'flex-row'}`}>
        <div className="relative">
          <button onClick={() => setShowReactions(v => !v)}
            className="text-agora-400 hover:text-agora-600 p-1 rounded-lg hover:bg-agora-100 dark:hover:bg-agora-700 text-xs">
            😊
          </button>
          {showReactions && (
            <div className={`absolute bottom-8 ${isOwn ? 'right-0' : 'left-0'} bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-xl shadow-lg p-1.5 flex gap-1 z-10`}>
              {REACTIONS.map(r => (
                <button key={r} onClick={() => { onReact(msg.id, r); setShowReactions(false) }}
                  className="text-lg hover:scale-125 transition-transform p-0.5">{r}</button>
              ))}
            </div>
          )}
        </div>
        {isOwn && (
          <div className="relative">
            <button onClick={() => setShowMenu(v => !v)}
              className="text-agora-400 hover:text-agora-600 p-1 rounded-lg hover:bg-agora-100 dark:hover:bg-agora-700">
              <Edit2 size={12} />
            </button>
            {showMenu && (
              <div className="absolute bottom-8 right-0 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-xl shadow-lg py-1 z-10 min-w-[120px]">
                <button onClick={() => { onEdit(msg); setShowMenu(false) }}
                  className="flex items-center gap-2 w-full px-3 py-1.5 text-sm hover:bg-agora-50 dark:hover:bg-agora-700">
                  <Edit2 size={13} /> Edit
                </button>
                <button onClick={() => { onDelete(msg.id); setShowMenu(false) }}
                  className="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-red-500 hover:bg-agora-50 dark:hover:bg-agora-700">
                  <Trash2 size={13} /> Delete
                </button>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Bubble */}
      <div className={`max-w-[70%] space-y-1 ${isOwn ? 'items-end' : 'items-start'} flex flex-col`}>
        <div className={`rounded-2xl px-3.5 py-2.5 ${isOwn
          ? 'bg-agora-600 text-white rounded-br-sm'
          : 'bg-agora-100 dark:bg-agora-700 text-agora-900 dark:text-agora-100 rounded-bl-sm'}`}>
          {msg.content && <p className="text-sm whitespace-pre-wrap break-words">{msg.content}</p>}
          {msg.image_url && (
            isGifUrl(msg.image_url)
              ? <img src={msg.image_url} alt="" className="rounded-lg max-w-[240px] mt-1" />
              : <img src={msg.image_url} alt="" className="rounded-lg max-w-[240px] mt-1 cursor-zoom-in" />
          )}
        </div>

        {/* Reactions */}
        {msg.reactions && msg.reactions.length > 0 && (
          <div className="flex gap-1 flex-wrap">
            {Object.entries(msg.reactions.reduce((acc, r) => {
              acc[r.reaction] = (acc[r.reaction] || 0) + 1
              return acc
            }, {} as Record<string, number>)).map(([emoji, count]) => (
              <span key={emoji} className="text-xs bg-agora-100 dark:bg-agora-700 rounded-full px-2 py-0.5 border border-agora-200 dark:border-agora-600">
                {emoji} {count}
              </span>
            ))}
          </div>
        )}

        <div className={`flex items-center gap-1.5 ${isOwn ? 'flex-row-reverse' : 'flex-row'}`}>
          <span className="text-[10px] text-agora-400">
            {formatDistanceToNow(new Date(msg.created_at), { addSuffix: true })}
            {msg.edited_at && ' · edited'}
          </span>
          {isOwn && readers.length > 0 && (
            <span className="text-[10px] text-agora-400 flex items-center gap-0.5">
              <Check size={10} className="text-agora-500" />
              {readers.map(r => r.display_name || r.username).join(', ')}
            </span>
          )}
        </div>
      </div>
    </div>
  )
}

// ── Thread View ───────────────────────────────────────────────────────────────

function ThreadView({ convId, onBack }: { convId: string; onBack?: () => void }) {
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [text, setText] = useState('')
  const [imageUrl, setImageUrl] = useState('')
  const [uploading, setUploading] = useState(false)
  const [editing, setEditing] = useState<Message | null>(null)
  const [editText, setEditText] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)
  const fileRef   = useRef<HTMLInputElement>(null)

  const { data: convData } = useQuery({
    queryKey: ['conversation', convId],
    queryFn: () => dmApi.getConversation(convId).then(r => r.data),
  })

  const { data: msgData, refetch: refetchMsgs } = useQuery({
    queryKey: ['messages', convId],
    queryFn: () => dmApi.getMessages(convId).then(r => r.data),
    refetchOnWindowFocus: false,
  })

  const messages: Message[] = msgData?.messages || []
  const conv: Conversation | undefined = convData
  const other = conv ? otherParticipant(conv, user!.id) : undefined

  // Mark read on mount and when new messages arrive
  useEffect(() => {
    dmApi.markRead(convId)
    qc.invalidateQueries({ queryKey: ['conversations'] })
  }, [convId, messages.length])

  // Scroll to bottom on new messages
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages.length])

  // Real-time events
  const { send } = useWebSocket(useCallback((event) => {
    if (event.conversation_id !== convId) return
    if (event.type === 'new_message' || event.type === 'message_edited' || event.type === 'message_deleted' || event.type === 'message_reaction' || event.type === 'read_receipt') {
      refetchMsgs()
      qc.invalidateQueries({ queryKey: ['conversations'] })
    }
  }, [convId]))

  const sendMsg = useMutation({
    mutationFn: () => dmApi.sendMessage(convId, text, imageUrl || undefined),
    onSuccess: () => { setText(''); setImageUrl(''); refetchMsgs(); qc.invalidateQueries({ queryKey: ['conversations'] }) },
  })

  const editMsg = useMutation({
    mutationFn: () => dmApi.editMessage(editing!.id, editText),
    onSuccess: () => { setEditing(null); setEditText(''); refetchMsgs() },
  })

  const deleteMsg = useMutation({
    mutationFn: (id: string) => dmApi.deleteMessage(id),
    onSuccess: () => refetchMsgs(),
  })

  const reactMsg = useMutation({
    mutationFn: ({ id, reaction }: { id: string; reaction: string }) => dmApi.reactMessage(id, reaction),
    onSuccess: () => refetchMsgs(),
  })

  const acceptRequest = useMutation({
    mutationFn: () => dmApi.acceptRequest(convId),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['conversation', convId] }); qc.invalidateQueries({ queryKey: ['conversations'] }) },
  })

  const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setUploading(true)
    try { const res = await feedApi.uploadMedia(file, 'posts'); setImageUrl(res.data.url) }
    catch (err: any) { alert(err?.response?.data?.error || 'Upload failed') }
    finally { setUploading(false); if (fileRef.current) fileRef.current.value = '' }
  }

  if (!other) return <div className="flex-1 flex items-center justify-center text-agora-400">Loading…</div>

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="px-4 py-3 border-b border-agora-100 dark:border-agora-700 flex items-center gap-3">
        {onBack && (
          <button onClick={onBack} className="text-agora-400 hover:text-agora-600 mr-1">
            <ArrowLeft size={18} />
          </button>
        )}
        <Avatar user={other} size={9} showPresence />
        <div className="flex-1 min-w-0">
          <Link to={`/profile/${other.username}`} className="font-semibold text-sm hover:underline">
            {other.display_name || other.username}
          </Link>
          <p className="text-xs text-agora-400">
            {other.is_online
              ? <span className="text-green-500 font-medium">Online</span>
              : other.last_active_at
                ? <>Last seen {formatDistanceToNow(new Date(other.last_active_at), { addSuffix: true })}</>
                : <>@{other.username}</>}
          </p>
        </div>
      </div>

      {/* Message request banner */}
      {conv && !conv.is_accepted && (
        <div className="mx-4 mt-3 p-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-700 rounded-xl text-sm space-y-2">
          <p className="font-medium text-amber-800 dark:text-amber-300">Message request from {other.display_name || other.username}</p>
          <p className="text-amber-700 dark:text-amber-400 text-xs">You can't reply until you accept this request.</p>
          <div className="flex gap-2">
            <button onClick={() => acceptRequest.mutate()} className="btn-primary text-xs py-1 px-3">Accept</button>
            <button onClick={() => dmApi.leaveConversation(convId)} className="btn-secondary text-xs py-1 px-3 text-red-500">Decline</button>
          </div>
        </div>
      )}

      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-4 py-3 space-y-0.5">
        {messages.length === 0 && (
          <div className="text-center py-12 text-agora-400 text-sm">
            <p>No messages yet. Say hello!</p>
          </div>
        )}
        {messages.map(msg => (
          <MessageBubble
            key={msg.id}
            msg={msg}
            isOwn={msg.author_id === user!.id}
            participants={conv?.participants || []}
            onEdit={(m) => { setEditing(m); setEditText(m.content) }}
            onDelete={(id) => { if (confirm('Delete this message?')) deleteMsg.mutate(id) }}
            onReact={(id, reaction) => reactMsg.mutate({ id, reaction })}
          />
        ))}
        <div ref={bottomRef} />
      </div>

      {/* Edit mode banner */}
      {editing && (
        <div className="mx-4 mb-1 px-3 py-1.5 bg-agora-50 dark:bg-agora-700 rounded-lg flex items-center justify-between text-xs text-agora-500">
          <span>Editing message</span>
          <button onClick={() => { setEditing(null); setEditText('') }} className="hover:text-agora-700"><X size={13} /></button>
        </div>
      )}

      {/* Composer */}
      {(conv?.is_accepted) && (
        <div className="px-4 py-3 border-t border-agora-100 dark:border-agora-700 space-y-2">
          {imageUrl && (
            <div className="relative w-fit">
              {isGifUrl(imageUrl) && <span className="absolute top-1 left-1 bg-black/60 text-white text-[10px] font-bold px-1 py-0.5 rounded z-10">GIF</span>}
              <img src={imageUrl} alt="" className="rounded-lg max-h-24 object-contain" />
              <button onClick={() => setImageUrl('')} className="absolute top-1 right-1 bg-black/60 text-white rounded-full w-5 h-5 flex items-center justify-center">
                <X size={10} />
              </button>
            </div>
          )}
          <div className="flex items-end gap-2">
            <label className={`flex-shrink-0 p-2 rounded-lg text-agora-400 hover:text-agora-600 hover:bg-agora-100 dark:hover:bg-agora-700 cursor-pointer transition-colors ${uploading ? 'opacity-50 pointer-events-none' : ''}`}>
              <Image size={18} />
              <input ref={fileRef} type="file" accept="image/*" className="hidden" onChange={handleImageUpload} disabled={uploading} />
            </label>
            <textarea
              className="input flex-1 text-sm py-2 resize-none min-h-[40px] max-h-32 overflow-y-auto"
              placeholder={editing ? 'Edit message…' : `Message ${other.display_name || other.username}…`}
              autoComplete="off"
              rows={1}
              value={editing ? editText : text}
              onChange={e => {
                editing ? setEditText(e.target.value) : setText(e.target.value)
                e.target.style.height = 'auto'
                e.target.style.height = Math.min(e.target.scrollHeight, 128) + 'px'
              }}
              onKeyDown={e => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  if (editing) { if (editText.trim()) editMsg.mutate() }
                  else { if (text.trim() || imageUrl) sendMsg.mutate() }
                }
                if (e.key === 'Escape' && editing) { setEditing(null); setEditText('') }
              }}
            />
            <button
              onClick={() => editing ? editMsg.mutate() : sendMsg.mutate()}
              disabled={(editing ? !editText.trim() : (!text.trim() && !imageUrl)) || sendMsg.isPending || uploading}
              className="btn-primary p-2 flex-shrink-0 rounded-xl"
            >
              <Send size={16} />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ── Main Page ─────────────────────────────────────────────────────────────────

export default function MessagesPage() {
  const { convId } = useParams<{ convId?: string }>()
  const nav = useNavigate()
  const [activeId, setActiveId] = useState<string | undefined>(convId)
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768)

  useEffect(() => {
    const onResize = () => setIsMobile(window.innerWidth < 768)
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  useEffect(() => { setActiveId(convId) }, [convId])

  const handleSelect = (id: string) => {
    setActiveId(id)
    nav(`/messages/${id}`, { replace: true })
  }

  // Break out of the max-w container padding
  return (
    <div className="-mx-4 -my-6 overflow-hidden bg-white dark:bg-agora-900 border border-agora-200 dark:border-agora-700 rounded-xl"
      style={{ height: 'calc(100vh - 96px)' }}>
      <div className="flex h-full">
        {/* Left: conversation list — fixed width, never collapses on desktop */}
        <div className={`${activeId && isMobile ? 'hidden' : 'flex'} flex-col flex-shrink-0 border-r border-agora-100 dark:border-agora-700`}
          style={{ width: 280 }}>
          <ConvList activeId={activeId} onSelect={handleSelect} />
        </div>

        {/* Right: thread or empty state */}
        {activeId ? (
          <div className="flex-1 flex flex-col min-w-0">
            <ThreadView convId={activeId} onBack={isMobile ? () => { setActiveId(undefined); nav('/messages') } : undefined} />
          </div>
        ) : (
          <div className="flex-1 flex items-center justify-center text-agora-400 flex-col gap-2">
            <MessageCircle size={40} className="opacity-30" />
            <p className="text-sm">Select a conversation to start messaging</p>
          </div>
        )}
      </div>
    </div>
  )
}
