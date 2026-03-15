import { useState, useEffect, useRef, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation } from 'react-router-dom'
import { X, Minus, Send, Image, Edit2, Trash2 } from 'lucide-react'
import { dmApi, feedApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import { useChatStore } from '../../store/chat'
import { useWebSocket } from '../../hooks/useWebSocket'
import { isGifUrl } from '../../utils/gif'
import { formatDistanceToNow } from 'date-fns'

interface Participant {
  user_id: string
  username: string
  display_name: string
  avatar_url: string
  last_read_at?: string
  read_receipts: boolean
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

function otherParticipant(conv: Conversation, myId: string) {
  return conv.participants?.find(p => p.user_id !== myId)
}

const REACTIONS = ['❤️', '😂', '😮', '😢', '👍', '👎']

// ── Single floating chat window ───────────────────────────────────────────────

function ChatWindow({ convId, minimized, index }: { convId: string; minimized: boolean; index: number }) {
  const { user } = useAuthStore()
  const { closeChat, toggleMinimize } = useChatStore()
  const qc = useQueryClient()
  const [text, setText] = useState('')
  const [imageUrl, setImageUrl] = useState('')
  const [uploading, setUploading] = useState(false)
  const [editing, setEditing] = useState<Message | null>(null)
  const [editText, setEditText] = useState('')
  const [hoveredMsg, setHoveredMsg] = useState<string | null>(null)
  const [showReactionPicker, setShowReactionPicker] = useState<string | null>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  const { data: convData } = useQuery({
    queryKey: ['conversation', convId],
    queryFn: () => dmApi.getConversation(convId).then(r => r.data),
  })

  const { data: msgData, refetch: refetchMsgs } = useQuery({
    queryKey: ['messages', convId],
    queryFn: () => dmApi.getMessages(convId).then(r => r.data),
    enabled: !minimized,
    refetchOnWindowFocus: false,
  })

  const messages: Message[] = msgData?.messages || []
  const conv: Conversation | undefined = convData
  const other = conv ? otherParticipant(conv, user!.id) : undefined

  // Mark read when opened
  useEffect(() => {
    if (!minimized) {
      dmApi.markRead(convId)
      qc.invalidateQueries({ queryKey: ['conversations'] })
    }
  }, [convId, minimized, messages.length])

  // Scroll to bottom on new messages
  useEffect(() => {
    if (!minimized) bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages.length, minimized])

  // Real-time events
  useWebSocket(useCallback((event) => {
    if (event.conversation_id !== convId) return
    if (['new_message', 'message_edited', 'message_deleted', 'message_reaction', 'read_receipt'].includes(event.type)) {
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
    onSuccess: () => { refetchMsgs(); setShowReactionPicker(null) },
  })

  const handleImageUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setUploading(true)
    try { const res = await feedApi.uploadMedia(file, 'posts'); setImageUrl(res.data.url) }
    catch (err: any) { alert(err?.response?.data?.error || 'Upload failed') }
    finally { setUploading(false); if (fileRef.current) fileRef.current.value = '' }
  }

  // Detect GIF URLs typed into the composer
  useEffect(() => {
    if (!text) return
    const match = text.match(/https?:\/\/\S+/i)
    if (match && isGifUrl(match[0]) && !imageUrl) {
      setImageUrl(match[0])
      setText(t => t.replace(match[0], '').trim())
    }
  }, [text])

  // Right offset: stack windows from right, each 300px + 8px gap
  const rightOffset = 8 + index * (300 + 8)

  if (!other) return null

  return (
    <div
      className="fixed bottom-0 z-40 flex flex-col bg-white dark:bg-agora-900 border border-agora-200 dark:border-agora-700 rounded-t-xl shadow-2xl"
      style={{ width: 300, right: rightOffset }}
    >
      {/* Header */}
      <div
        className="flex items-center gap-2 px-3 py-2.5 cursor-pointer select-none rounded-t-xl bg-agora-50 dark:bg-agora-800 border-b border-agora-200 dark:border-agora-700"
        onClick={() => toggleMinimize(convId)}
      >
        <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {other.avatar_url
            ? <img src={other.avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{(other.display_name || other.username)[0].toUpperCase()}</span>}
        </div>
        <Link
          to={`/profile/${other.username}`}
          className="flex-1 text-sm font-semibold truncate text-agora-900 dark:text-agora-100 hover:underline"
          onClick={e => e.stopPropagation()}
        >
          {other.display_name || other.username}
        </Link>
        <button
          onClick={e => { e.stopPropagation(); toggleMinimize(convId) }}
          className="text-agora-400 hover:text-agora-600 p-0.5 rounded"
          title={minimized ? 'Expand' : 'Minimize'}
        >
          <Minus size={14} />
        </button>
        <button
          onClick={e => { e.stopPropagation(); closeChat(convId) }}
          className="text-agora-400 hover:text-red-500 p-0.5 rounded"
          title="Close"
        >
          <X size={14} />
        </button>
      </div>

      {/* Body — hidden when minimized */}
      {!minimized && (
        <>
          {/* Messages */}
          <div className="flex-1 overflow-y-auto px-3 py-2 space-y-1" style={{ height: 320 }}>
            {messages.length === 0 && (
              <p className="text-center text-xs text-agora-400 py-8">No messages yet. Say hello!</p>
            )}
            {messages.map(msg => {
              const isOwn = msg.author_id === user!.id
              if (msg.deleted_at) {
                return (
                  <div key={msg.id} className={`flex ${isOwn ? 'justify-end' : 'justify-start'}`}>
                    <span className="text-xs text-agora-400 italic px-2 py-1">Message deleted</span>
                  </div>
                )
              }

              // Read receipts
              const readers = (conv?.participants || []).filter(p =>
                p.user_id !== msg.author_id && p.last_read_at && p.last_read_at >= msg.created_at
              )

              return (
                <div
                  key={msg.id}
                  className={`flex items-end gap-1 group ${isOwn ? 'flex-row-reverse' : 'flex-row'}`}
                  onMouseEnter={() => setHoveredMsg(msg.id)}
                  onMouseLeave={() => { setHoveredMsg(null); setShowReactionPicker(null) }}
                >
                  {/* Quick actions on hover */}
                  <div className={`flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity ${isOwn ? 'flex-row-reverse' : 'flex-row'}`}>
                    <div className="relative">
                      <button
                        onClick={() => setShowReactionPicker(p => p === msg.id ? null : msg.id)}
                        className="text-agora-400 hover:text-agora-600 text-xs p-0.5 rounded"
                      >😊</button>
                      {showReactionPicker === msg.id && (
                        <div className={`absolute bottom-6 ${isOwn ? 'right-0' : 'left-0'} bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-xl shadow-lg p-1 flex gap-0.5 z-10`}>
                          {REACTIONS.map(r => (
                            <button key={r} onClick={() => reactMsg.mutate({ id: msg.id, reaction: r })}
                              className="text-base hover:scale-125 transition-transform p-0.5">{r}</button>
                          ))}
                        </div>
                      )}
                    </div>
                    {isOwn && (
                      <>
                        <button onClick={() => { setEditing(msg); setEditText(msg.content); inputRef.current?.focus() }}
                          className="text-agora-400 hover:text-agora-600 p-0.5 rounded">
                          <Edit2 size={11} />
                        </button>
                        <button onClick={() => { if (confirm('Delete?')) deleteMsg.mutate(msg.id) }}
                          className="text-agora-400 hover:text-red-500 p-0.5 rounded">
                          <Trash2 size={11} />
                        </button>
                      </>
                    )}
                  </div>

                  <div className={`max-w-[75%] space-y-0.5 flex flex-col ${isOwn ? 'items-end' : 'items-start'}`}>
                    <div className={`rounded-2xl px-3 py-2 text-sm ${isOwn
                      ? 'bg-agora-600 text-white rounded-br-sm'
                      : 'bg-agora-100 dark:bg-agora-700 text-agora-900 dark:text-agora-100 rounded-bl-sm'}`}>
                      {msg.content && <p className="whitespace-pre-wrap break-words">{msg.content}</p>}
                      {msg.image_url && (
                        isGifUrl(msg.image_url)
                          ? <img src={msg.image_url} alt="" className="rounded-lg max-w-[180px] mt-1" />
                          : <img src={msg.image_url} alt="" className="rounded-lg max-w-[180px] mt-1" />
                      )}
                    </div>
                    {msg.reactions && msg.reactions.length > 0 && (
                      <div className="flex gap-0.5 flex-wrap">
                        {Object.entries(msg.reactions.reduce((acc, r) => {
                          acc[r.reaction] = (acc[r.reaction] || 0) + 1
                          return acc
                        }, {} as Record<string, number>)).map(([emoji, count]) => (
                          <span key={emoji} className="text-xs bg-agora-100 dark:bg-agora-700 rounded-full px-1.5 py-0.5">
                            {emoji} {count}
                          </span>
                        ))}
                      </div>
                    )}
                    <span className="text-[10px] text-agora-400">
                      {formatDistanceToNow(new Date(msg.created_at), { addSuffix: true })}
                      {msg.edited_at && ' · edited'}
                      {isOwn && readers.length > 0 && ' · seen'}
                    </span>
                  </div>
                </div>
              )
            })}
            <div ref={bottomRef} />
          </div>

          {/* Image preview */}
          {imageUrl && (
            <div className="px-3 pt-1 relative w-fit">
              {isGifUrl(imageUrl) && <span className="absolute top-2 left-4 bg-black/60 text-white text-[9px] font-bold px-1 rounded z-10">GIF</span>}
              <img src={imageUrl} alt="" className="rounded-lg max-h-16 object-contain" />
              <button onClick={() => setImageUrl('')} className="absolute top-0 right-0 bg-black/60 text-white rounded-full w-4 h-4 flex items-center justify-center">
                <X size={8} />
              </button>
            </div>
          )}

          {/* Edit banner */}
          {editing && (
            <div className="px-3 py-1 bg-agora-50 dark:bg-agora-800 flex items-center justify-between text-xs text-agora-500 border-t border-agora-100 dark:border-agora-700">
              <span>Editing message</span>
              <button onClick={() => { setEditing(null); setEditText('') }}><X size={12} /></button>
            </div>
          )}

          {/* Composer */}
          <div className="px-2 py-2 border-t border-agora-200 dark:border-agora-700 flex items-end gap-1.5">
            <label className={`flex-shrink-0 p-1.5 rounded-lg text-agora-400 hover:text-agora-600 hover:bg-agora-100 dark:hover:bg-agora-700 cursor-pointer transition-colors ${uploading ? 'opacity-50 pointer-events-none' : ''}`}>
              <Image size={15} />
              <input ref={fileRef} type="file" accept="image/*" className="hidden" onChange={handleImageUpload} disabled={uploading} />
            </label>
            <textarea
              ref={inputRef}
              className="input flex-1 text-sm py-1.5 resize-none min-h-[34px] max-h-24 overflow-y-auto"
              placeholder="Aa"
              autoComplete="off"
              rows={1}
              value={editing ? editText : text}
              onChange={e => {
                editing ? setEditText(e.target.value) : setText(e.target.value)
                e.target.style.height = 'auto'
                e.target.style.height = Math.min(e.target.scrollHeight, 96) + 'px'
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
              disabled={editing ? !editText.trim() : (!text.trim() && !imageUrl) || sendMsg.isPending}
              className="btn-primary p-1.5 rounded-lg flex-shrink-0"
            >
              <Send size={14} />
            </button>
          </div>
        </>
      )}
    </div>
  )
}

// ── Container: renders all open chat windows ──────────────────────────────────

export default function ChatWindows() {
  const { openChats } = useChatStore()
  const location = useLocation()
  // Don't render on messages page (redundant with full page experience)
  if (location.pathname.startsWith('/messages')) return null

  return (
    <>
      {openChats.map((chat, i) => (
        <ChatWindow key={chat.convId} convId={chat.convId} minimized={chat.minimized} index={i} />
      ))}
    </>
  )
}
