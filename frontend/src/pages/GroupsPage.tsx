import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { groupsApi } from '../api'
import { Users, Lock, Globe, Plus, Search } from 'lucide-react'
import CreateGroupModal from '../components/groups/CreateGroupModal'

export default function GroupsPage() {
  const [filter, setFilter] = useState<'discover'|'joined'|'mine'>('discover')
  const [q, setQ] = useState('')
  const [search, setSearch] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const qc = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['groups', filter, search],
    queryFn: () => groupsApi.list({ filter: filter === 'discover' ? '' : filter, q: search }).then(r => r.data),
  })
  const groups = data?.groups ?? []

  const filters = [
    { id: 'discover', label: 'Discover' },
    { id: 'joined',   label: 'Joined' },
    { id: 'mine',     label: 'My Groups' },
  ]

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold">Groups</h1>
        <button onClick={() => setShowCreate(true)} className="btn-primary flex items-center gap-1.5 text-sm">
          <Plus size={15} /> New Group
        </button>
      </div>

      {/* Filter tabs */}
      <div className="flex gap-1 bg-agora-100 dark:bg-agora-800 rounded-lg p-1">
        {filters.map(f => (
          <button key={f.id} onClick={() => setFilter(f.id as any)}
            className={`flex-1 py-1.5 text-sm font-medium rounded-md transition-colors ${filter === f.id ? 'bg-white dark:bg-agora-700 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
            {f.label}
          </button>
        ))}
      </div>

      {/* Search */}
      {filter === 'discover' && (
        <div className="relative">
          <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-agora-400" />
          <input className="input pl-9 text-sm" placeholder="Search groups…"
            value={q} onChange={e => setQ(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && setSearch(q)} />
        </div>
      )}

      {isLoading && <div className="text-center py-8 text-agora-400">Loading…</div>}

      {!isLoading && groups.length === 0 && (
        <div className="card p-10 text-center text-agora-400 space-y-2">
          <Users size={32} className="mx-auto opacity-40" />
          {filter === 'discover' && <><p className="font-medium">No groups found</p><p className="text-sm">Be the first — create a group!</p></>}
          {filter === 'joined'   && <><p className="font-medium">You haven't joined any groups yet</p><p className="text-sm">Discover groups to get started.</p></>}
          {filter === 'mine'     && <><p className="font-medium">You haven't created any groups yet</p><button onClick={() => setShowCreate(true)} className="btn-primary text-sm mt-2">Create a group</button></>}
        </div>
      )}

      <div className="space-y-3">
        {groups.map((g: any) => <GroupCard key={g.id} group={g} onJoinLeave={() => qc.invalidateQueries({ queryKey: ['groups'] })} />)}
      </div>

      {showCreate && <CreateGroupModal onClose={() => setShowCreate(false)} onCreated={(slug) => { qc.invalidateQueries({ queryKey: ['groups'] }); setShowCreate(false) }} />}
    </div>
  )
}

function GroupCard({ group: g, onJoinLeave }: { group: any, onJoinLeave: () => void }) {
  const qc = useQueryClient()

  const handleJoin = async () => {
    await groupsApi.join(g.slug)
    onJoinLeave()
  }
  const handleLeave = async () => {
    if (!confirm(`Leave "${g.name}"?`)) return
    await groupsApi.leave(g.slug)
    onJoinLeave()
  }

  return (
    <div className="card overflow-hidden">
      {g.cover_url && <img src={g.cover_url} alt="" className="w-full h-24 object-cover" />}
      <div className="p-4 flex gap-3">
        <div className="w-12 h-12 rounded-xl bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {g.avatar_url
            ? <img src={g.avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center text-xl font-bold text-agora-500">{g.name[0].toUpperCase()}</span>}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-start justify-between gap-2">
            <div>
              <Link to={`/groups/${g.slug}`} className="font-semibold hover:underline">{g.name}</Link>
              <div className="flex items-center gap-1.5 text-xs text-agora-400 mt-0.5">
                {g.privacy === 'private' ? <Lock size={11} /> : <Globe size={11} />}
                <span className="capitalize">{g.privacy}</span>
                <span>·</span>
                <Users size={11} />
                <span>{g.member_count} {g.member_count === 1 ? 'member' : 'members'}</span>
                <span>·</span>
                <span>{g.post_count} posts</span>
              </div>
            </div>
            <div className="flex-shrink-0">
              {g.is_member
                ? <button onClick={handleLeave} className="btn-secondary text-xs py-1 px-2.5">Leave</button>
                : g.privacy === 'public'
                  ? <button onClick={handleJoin} className="btn-primary text-xs py-1 px-2.5">Join</button>
                  : <span className="text-xs text-agora-400 flex items-center gap-1"><Lock size={11}/> Private</span>
              }
            </div>
          </div>
          {g.description && <p className="text-sm text-agora-500 mt-1.5 line-clamp-2">{g.description}</p>}
          {g.is_member && (
            <div className="mt-2">
              <Link to={`/groups/${g.slug}`} className="text-xs text-agora-600 dark:text-agora-400 hover:underline font-medium">
                {g.member_role === 'owner' ? '👑 Owner' : g.member_role === 'mod' ? '🛡 Moderator' : 'Member'} · View group →
              </Link>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
