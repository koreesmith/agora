import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { searchApi, friendsApi, federationApi } from '../api'
import { useAuthStore } from '../store/auth'
import { handle } from '../utils/handle'
import { formatDistanceToNow } from 'date-fns'
import { renderContent } from '../components/feed/CommentsSection'
import { Search, Users, FileText, Heart, MessageCircle, Clock, UserPlus, Check, Link2 } from 'lucide-react'

export default function SearchPage() {
  const [input, setInput] = useState('')
  const [q, setQ] = useState('')
  const [tab, setTab] = useState<'users'|'posts'>('users')
  const debounceTimer = useRef<ReturnType<typeof setTimeout>>()
  const { user } = useAuthStore()
  const qc = useQueryClient()

  useEffect(() => {
    clearTimeout(debounceTimer.current)
    debounceTimer.current = setTimeout(() => setQ(input), 350)
    return () => clearTimeout(debounceTimer.current)
  }, [input])

  const isHandleLookup = /^[a-zA-Z0-9_-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(q.trim())

  const enabled = q.length >= 2

  const { data: usersData, isFetching: usersFetching } = useQuery({
    queryKey: ['search-users', q],
    queryFn: () => searchApi.searchUsers(q).then(r => r.data),
    enabled: enabled && tab === 'users' && !isHandleLookup,
  })
  const { data: postsData, isFetching: postsFetching } = useQuery({
    queryKey: ['search-posts', q],
    queryFn: () => searchApi.searchPosts(q).then(r => r.data),
    enabled: enabled && tab === 'posts' && !isHandleLookup,
  })

  // Cross-instance handle lookup: user@instance.com
  const { data: lookupData, isFetching: lookupFetching, error: lookupError } = useQuery({
    queryKey: ['federation-lookup', q],
    queryFn: () => federationApi.lookupUser(q.trim()).then(r => r.data),
    enabled: isHandleLookup,
    retry: false,
  })

  const send = useMutation({
    mutationFn: (id: string) => friendsApi.sendRequest(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['search-users', q] }),
  })

  const users = usersData?.users || []
  const posts = postsData?.posts || []
  const isFetching = isHandleLookup ? lookupFetching : (tab === 'users' ? usersFetching : postsFetching)

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold">Search</h1>

      {/* Search input */}
      <div className="relative">
        <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-agora-400" />
        <input
          className="input pl-9"
          placeholder="Search people and posts…"
          value={input}
          onChange={e => setInput(e.target.value)}
          autoFocus
        />
        {isFetching && q.length >= 2 && (
          <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-agora-400 animate-pulse">searching…</span>
        )}
      </div>

      {/* Cross-instance handle lookup */}
      {isHandleLookup ? (
        <div className="space-y-3">
          <p className="text-xs text-agora-400 px-1 flex items-center gap-1.5">
            <Link2 size={12} /> Looking up federated user
          </p>
          {lookupFetching && <div className="text-center py-6 text-agora-400 text-sm">Contacting remote instance…</div>}
          {!lookupFetching && lookupError && (
            <div className="card p-8 text-center text-agora-400 space-y-1">
              <p className="font-medium">User not found</p>
              <p className="text-sm">Make sure the handle is correct and the instance is reachable.</p>
            </div>
          )}
          {!lookupFetching && lookupData?.user && (
            <UserResult
              user={{ ...lookupData.user, friendship_status: '' }}
              currentUserId={user?.id}
              onAdd={() => {}}
              addPending={false}
            />
          )}
        </div>
      ) : (
        <>
          {/* Tabs */}
          <div className="flex gap-1 bg-agora-100 dark:bg-agora-800 rounded-lg p-1">
            <button onClick={() => setTab('users')}
              className={`flex-1 flex items-center justify-center gap-1.5 py-1.5 text-sm font-medium rounded-md transition-colors ${tab === 'users' ? 'bg-white dark:bg-agora-700 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
              <Users size={13} /> People
            </button>
            <button onClick={() => setTab('posts')}
              className={`flex-1 flex items-center justify-center gap-1.5 py-1.5 text-sm font-medium rounded-md transition-colors ${tab === 'posts' ? 'bg-white dark:bg-agora-700 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
              <FileText size={13} /> Posts
            </button>
          </div>

          {/* Empty state */}
          {!enabled && (
            <div className="card p-8 text-center text-agora-400 text-sm">
              <Search size={28} className="mx-auto mb-2 opacity-40" />
              Type at least 2 characters to search, or enter <span className="font-mono">user@instance.com</span> to find someone on another instance.
            </div>
          )}

          {/* People tab */}
          {tab === 'users' && enabled && (
            <div className="space-y-2">
              {users.length === 0 && !usersFetching && (
                <div className="card p-8 text-center text-agora-400">No people found for "{q}".</div>
              )}
              {users.map((u: any) => (
                <UserResult key={u.id} user={u} currentUserId={user?.id}
                  onAdd={() => send.mutate(u.id)} addPending={send.isPending} />
              ))}
            </div>
          )}

          {/* Posts tab */}
          {tab === 'posts' && enabled && (
            <div className="space-y-3">
              {posts.length === 0 && !postsFetching && (
                <div className="card p-8 text-center text-agora-400">No posts found for "{q}".</div>
              )}
              {posts.map((p: any) => (
                <PostResult key={p.id} post={p} query={q} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}

// ── User result card ───────────────────────────────────────────────────────────

function UserResult({ user: u, currentUserId, onAdd, addPending }: {
  user: any, currentUserId?: string, onAdd: () => void, addPending: boolean
}) {
  const [sent, setSent] = useState(u.friendship_status === 'pending')

  const handleAdd = () => { setSent(true); onAdd() }
  const isSelf = u.id === currentUserId

  return (
    <div className="card p-3 flex items-center gap-3">
      <Link to={`/profile/${u.username}`} className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
        {u.avatar_url
          ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
          : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600">{(u.display_name||u.username)[0].toUpperCase()}</span>}
      </Link>
      <div className="flex-1 min-w-0">
        <Link to={`/profile/${u.username}`} className="font-medium text-sm hover:underline block truncate">
          {u.display_name || u.username}
        </Link>
        <p className="text-xs text-agora-400 truncate">
          {handle(u.username, u.is_remote, u.remote_instance)}
        </p>
        {u.bio && <p className="text-xs text-agora-500 truncate mt-0.5">{u.bio}</p>}
      </div>
      {!isSelf && (
        <div className="flex-shrink-0">
          {u.friendship_status === 'accepted' && (
            <span className="text-xs text-agora-400 flex items-center gap-1"><Check size={12} /> Friends</span>
          )}
          {(u.friendship_status === 'pending' || sent) && (
            <span className="text-xs text-agora-400 flex items-center gap-1"><Clock size={12} /> Pending</span>
          )}
          {u.friendship_status === 'pending_incoming' && (
            <Link to={`/profile/${u.username}`} className="text-xs text-agora-600 hover:underline">Respond</Link>
          )}
          {!u.friendship_status && !sent && (
            <button onClick={handleAdd} disabled={addPending}
              className="btn-primary text-xs py-1 px-2.5 flex items-center gap-1">
              <UserPlus size={12} /> Add
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// ── Post result card ───────────────────────────────────────────────────────────

function PostResult({ post: p, query }: { post: any, query: string }) {
  // Highlight the matched query term in the content snippet
  const highlight = (text: string) => {
    const idx = text.toLowerCase().indexOf(query.toLowerCase())
    if (idx === -1) return <span>{text.slice(0, 200)}</span>
    const start = Math.max(0, idx - 60)
    const end = Math.min(text.length, idx + query.length + 100)
    const snippet = (start > 0 ? '…' : '') + text.slice(start, end) + (end < text.length ? '…' : '')
    const matchStart = snippet.toLowerCase().indexOf(query.toLowerCase())
    if (matchStart === -1) return <span>{snippet}</span>
    return (
      <>
        {snippet.slice(0, matchStart)}
        <mark className="bg-yellow-100 dark:bg-yellow-900/40 text-inherit rounded px-0.5">
          {snippet.slice(matchStart, matchStart + query.length)}
        </mark>
        {snippet.slice(matchStart + query.length)}
      </>
    )
  }

  return (
    <Link to={`/post/${p.id}`} className="card p-4 block hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors">
      <div className="flex items-center gap-2 mb-2">
        <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {p.avatar_url
            ? <img src={p.avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{(p.display_name||p.username)[0].toUpperCase()}</span>}
        </div>
        <span className="text-sm font-medium">{p.display_name || p.username}</span>
        <span className="text-xs text-agora-400">@{p.username}</span>
        <span className="text-xs text-agora-400 ml-auto flex items-center gap-1">
          <Clock size={11} />
          {formatDistanceToNow(new Date(p.created_at), { addSuffix: true })}
        </span>
      </div>
      <p className="text-sm text-agora-700 dark:text-agora-300 line-clamp-3">
        {highlight(p.content)}
      </p>
      {p.image_url && (
        <img src={p.image_url} alt="" className="mt-2 rounded-lg max-h-32 object-cover" />
      )}
      <div className="flex items-center gap-4 mt-2 text-xs text-agora-400">
        <span className="flex items-center gap-1"><Heart size={11} />{p.like_count}</span>
        <span className="flex items-center gap-1"><MessageCircle size={11} />{p.comment_count}</span>
      </div>
    </Link>
  )
}
