import { useState, useEffect, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { groupsApi } from '../api'
import { Users, Lock, Globe, Plus, Search, TrendingUp, UserCheck } from 'lucide-react'
import CreateGroupModal from '../components/groups/CreateGroupModal'

export default function GroupsPage() {
  const [filter, setFilter] = useState<'discover'|'joined'|'mine'>('discover')
  const [searchInput, setSearchInput] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const qc = useQueryClient()
  const debounceTimer = useRef<ReturnType<typeof setTimeout>>()

  // Debounce search input — fires 300ms after user stops typing
  useEffect(() => {
    clearTimeout(debounceTimer.current)
    debounceTimer.current = setTimeout(() => setDebouncedSearch(searchInput), 300)
    return () => clearTimeout(debounceTimer.current)
  }, [searchInput])

  const isSearching = debouncedSearch.trim().length > 0

  const { data: searchData, isFetching: searchFetching } = useQuery({
    queryKey: ['groups-search', debouncedSearch],
    queryFn: () => groupsApi.list({ q: debouncedSearch }).then(r => r.data),
    enabled: isSearching,
  })

  const { data: browseData, isLoading: browseLoading } = useQuery({
    queryKey: ['groups-browse', filter],
    queryFn: () => groupsApi.list({ filter: filter === 'discover' ? undefined : filter }).then(r => r.data),
    enabled: !isSearching,
  })

  const refresh = () => qc.invalidateQueries({ queryKey: ['groups-browse'] })

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

      {/* Search bar — always visible */}
      <div className="relative">
        <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-agora-400" />
        <input className="input pl-9 text-sm" placeholder="Search groups…"
          value={searchInput} onChange={e => setSearchInput(e.target.value)} />
        {searchFetching && (
          <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-agora-400 animate-pulse">searching…</span>
        )}
      </div>

      {isSearching ? (
        <SearchResults groups={searchData?.groups ?? []} loading={searchFetching} onJoinLeave={refresh} />
      ) : (
        <>
          <div className="flex gap-1 bg-agora-100 dark:bg-agora-800 rounded-lg p-1">
            {filters.map(f => (
              <button key={f.id} onClick={() => setFilter(f.id as any)}
                className={`flex-1 py-1.5 text-sm font-medium rounded-md transition-colors ${filter === f.id ? 'bg-white dark:bg-agora-700 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
                {f.label}
              </button>
            ))}
          </div>

          {browseLoading && <div className="text-center py-8 text-agora-400">Loading…</div>}

          {!browseLoading && filter === 'discover' && (
            <DiscoverView data={browseData} onJoinLeave={refresh} />
          )}

          {!browseLoading && filter !== 'discover' && (
            <BrowseList groups={browseData?.groups ?? []} filter={filter} onJoinLeave={refresh} onCreateClick={() => setShowCreate(true)} />
          )}
        </>
      )}

      {showCreate && (
        <CreateGroupModal onClose={() => setShowCreate(false)} onCreated={() => { refresh(); setShowCreate(false) }} />
      )}
    </div>
  )
}

function SearchResults({ groups, loading, onJoinLeave }: { groups: any[], loading: boolean, onJoinLeave: () => void }) {
  if (loading) return <div className="text-center py-8 text-agora-400">Searching…</div>
  if (groups.length === 0) return (
    <div className="card p-10 text-center text-agora-400 space-y-1">
      <Search size={28} className="mx-auto opacity-40" />
      <p className="font-medium">No groups found</p>
      <p className="text-sm">Try different keywords.</p>
    </div>
  )
  return (
    <div className="space-y-3">
      <p className="text-xs text-agora-400 font-medium uppercase tracking-wide px-1">
        {groups.length} result{groups.length !== 1 ? 's' : ''}
      </p>
      {groups.map((g: any) => <GroupCard key={g.id} group={g} onJoinLeave={onJoinLeave} />)}
    </div>
  )
}

function DiscoverView({ data, onJoinLeave }: { data: any, onJoinLeave: () => void }) {
  const friendGroups: any[] = data?.friend_groups ?? []
  const popularGroups: any[] = data?.popular_groups ?? []

  if (friendGroups.length === 0 && popularGroups.length === 0) return (
    <div className="card p-10 text-center text-agora-400 space-y-2">
      <Users size={32} className="mx-auto opacity-40" />
      <p className="font-medium">No groups to discover yet</p>
      <p className="text-sm">Be the first — create a group!</p>
    </div>
  )

  return (
    <div className="space-y-5">
      {friendGroups.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-sm font-semibold text-agora-500 flex items-center gap-1.5 px-1">
            <UserCheck size={14} /> Groups your friends are in
          </h2>
          {friendGroups.map((g: any) => (
            <GroupCard key={g.id} group={g} onJoinLeave={onJoinLeave} friendCount={g.friend_count} />
          ))}
        </section>
      )}

      {popularGroups.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-sm font-semibold text-agora-500 flex items-center gap-1.5 px-1">
            <TrendingUp size={14} /> Popular groups
          </h2>
          {popularGroups.map((g: any) => (
            <GroupCard key={g.id} group={g} onJoinLeave={onJoinLeave} />
          ))}
        </section>
      )}
    </div>
  )
}

function BrowseList({ groups, filter, onJoinLeave, onCreateClick }: {
  groups: any[], filter: string, onJoinLeave: () => void, onCreateClick: () => void
}) {
  if (groups.length === 0) return (
    <div className="card p-10 text-center text-agora-400 space-y-2">
      <Users size={32} className="mx-auto opacity-40" />
      {filter === 'joined' && <><p className="font-medium">You haven't joined any groups yet</p><p className="text-sm">Discover groups to get started.</p></>}
      {filter === 'mine'   && <><p className="font-medium">You haven't created any groups yet</p><button onClick={onCreateClick} className="btn-primary text-sm mt-2">Create a group</button></>}
    </div>
  )
  return (
    <div className="space-y-3">
      {groups.map((g: any) => <GroupCard key={g.id} group={g} onJoinLeave={onJoinLeave} />)}
    </div>
  )
}

function GroupCard({ group: g, onJoinLeave, friendCount }: {
  group: any, onJoinLeave: () => void, friendCount?: number
}) {
  const [requestSent, setRequestSent] = useState(false)

  const handleJoin = async () => { await groupsApi.join(g.slug); onJoinLeave() }
  const handleLeave = async () => {
    if (!confirm(`Leave "${g.name}"?`)) return
    await groupsApi.leave(g.slug); onJoinLeave()
  }
  const handleRequest = async () => { await groupsApi.requestJoin(g.slug); setRequestSent(true) }

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
            <div className="min-w-0">
              <Link to={`/groups/${g.slug}`} className="font-semibold hover:underline">{g.name}</Link>
              <div className="flex items-center gap-1.5 text-xs text-agora-400 mt-0.5 flex-wrap">
                {g.privacy === 'private' ? <Lock size={11} /> : <Globe size={11} />}
                <span className="capitalize">{g.privacy}</span>
                <span>·</span>
                <Users size={11} />
                <span>{g.member_count} {g.member_count === 1 ? 'member' : 'members'}</span>
                {g.post_count > 0 && <><span>·</span><span>{g.post_count} posts</span></>}
                {friendCount != null && friendCount > 0 && (
                  <span className="text-agora-600 dark:text-agora-400 font-medium">
                    · {friendCount} {friendCount === 1 ? 'friend' : 'friends'} here
                  </span>
                )}
              </div>
            </div>
            <div className="flex-shrink-0">
              {g.is_member
                ? <button onClick={handleLeave} className="btn-secondary text-xs py-1 px-2.5">Leave</button>
                : g.privacy === 'public'
                  ? <button onClick={handleJoin} className="btn-primary text-xs py-1 px-2.5">Join</button>
                  : requestSent
                    ? <span className="text-xs text-agora-400">Requested ✓</span>
                    : <button onClick={handleRequest} className="btn-secondary text-xs py-1 px-2.5">Request</button>
              }
            </div>
          </div>
          {g.description && <p className="text-sm text-agora-500 mt-1.5 line-clamp-2">{g.description}</p>}
          {g.is_member && (
            <Link to={`/groups/${g.slug}`} className="inline-block mt-2 text-xs text-agora-600 dark:text-agora-400 hover:underline font-medium">
              {g.member_role === 'owner' ? '👑 Owner' : g.member_role === 'mod' ? '🛡 Moderator' : 'Member'} · View group →
            </Link>
          )}
        </div>
      </div>
    </div>
  )
}
