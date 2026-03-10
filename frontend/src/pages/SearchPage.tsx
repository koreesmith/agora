import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { searchApi, friendsApi } from '../api'
import { Search, UserPlus } from 'lucide-react'

export default function SearchPage() {
  const [q, setQ] = useState('')
  const qc = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['search', q],
    queryFn: () => searchApi.searchUsers(q).then(r => r.data),
    enabled: q.length >= 2,
  })

  const send = useMutation({
    mutationFn: (id: string) => friendsApi.sendRequest(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['search', q] }),
  })

  const users = data?.users || []

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold">Search</h1>
      <div className="relative">
        <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-agora-400" />
        <input className="input pl-9" placeholder="Search for people…" value={q} onChange={e => setQ(e.target.value)} autoFocus />
      </div>
      {isLoading && <div className="text-center text-agora-400 py-4">Searching…</div>}
      {users.map((u: any) => (
        <div key={u.id} className="card p-3 flex items-center gap-3">
          <Link to={`/profile/${u.username}`} className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
            {u.avatar_url ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
              : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600">{(u.display_name||u.username)[0].toUpperCase()}</span>}
          </Link>
          <div className="flex-1 min-w-0">
            <Link to={`/profile/${u.username}`} className="font-medium text-sm hover:underline">{u.display_name||u.username}</Link>
            <p className="text-xs text-agora-400">@{u.username}{u.is_remote && ` · ${u.remote_instance}`}</p>
          </div>
          {!u.friendship_status && u.friendship_status !== 'self' && (
            <button onClick={() => send.mutate(u.id)} className="btn-primary text-xs py-1 px-2"><UserPlus size={13}/> Add</button>
          )}
          {u.friendship_status === 'accepted' && <span className="text-xs text-agora-400">Friends</span>}
          {u.friendship_status === 'pending' && <span className="text-xs text-agora-400">Pending</span>}
        </div>
      ))}
      {q.length >= 2 && !isLoading && users.length === 0 && (
        <div className="card p-8 text-center text-agora-400">No users found for "{q}".</div>
      )}
      {q.length < 2 && <div className="card p-8 text-center text-agora-400 text-sm">Type at least 2 characters to search.</div>}
    </div>
  )
}
