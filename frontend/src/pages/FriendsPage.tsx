import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { friendsApi } from '../api'
import { UserCheck, UserX, Users, Trash2, Plus, ChevronRight, ChevronDown, UserMinus, List } from 'lucide-react'

export default function FriendsPage() {
  const [tab, setTab] = useState<'friends'|'requests'|'lists'>('friends')
  const [newListName, setNewListName] = useState('')
  const [expandedList, setExpandedList] = useState<string | null>(null)
  const qc = useQueryClient()
  const inv = (k: string) => qc.invalidateQueries({ queryKey: [k] })

  const { data: friendsData } = useQuery({ queryKey: ['friends'],       queryFn: () => friendsApi.listFriends().then(r => r.data) })
  const { data: reqData }     = useQuery({ queryKey: ['requests'],      queryFn: () => friendsApi.listRequests().then(r => r.data) })
  const { data: listsData }   = useQuery({ queryKey: ['friend-groups'], queryFn: () => friendsApi.listGroups().then(r => r.data) })

  const accept   = useMutation({ mutationFn: (id: string) => friendsApi.acceptRequest(id),  onSuccess: () => { inv('friends'); inv('requests') } })
  const decline  = useMutation({ mutationFn: (id: string) => friendsApi.declineRequest(id), onSuccess: () => inv('requests') })
  const unfriend = useMutation({ mutationFn: (id: string) => friendsApi.unfriend(id),       onSuccess: () => inv('friends') })
  const createList = useMutation({
    mutationFn: (name: string) => friendsApi.createGroup(name),
    onSuccess: () => { inv('friend-groups'); setNewListName('') },
  })
  const deleteList = useMutation({
    mutationFn: (id: string) => friendsApi.deleteGroup(id),
    onSuccess: () => { inv('friend-groups'); setExpandedList(null) },
  })
  const addToList = useMutation({
    mutationFn: ({ listID, friendID }: { listID: string, friendID: string }) => friendsApi.addToGroup(listID, friendID),
    onSuccess: (_,  { listID }) => qc.invalidateQueries({ queryKey: ['list-members', listID] }),
  })
  const removeFromList = useMutation({
    mutationFn: ({ listID, friendID }: { listID: string, friendID: string }) => friendsApi.removeFromGroup(listID, friendID),
    onSuccess: (_, { listID }) => qc.invalidateQueries({ queryKey: ['list-members', listID] }),
  })

  const friends  = friendsData?.friends || []
  const incoming = reqData?.incoming || []
  const outgoing = reqData?.outgoing || []
  const lists    = listsData?.groups || []
  const pendingCount = incoming.length

  const tabs = [
    { id: 'friends',  label: `Friends (${friends.length})` },
    { id: 'requests', label: `Requests${pendingCount ? ` (${pendingCount})` : ''}` },
    { id: 'lists',    label: 'Friend Lists' },
  ]

  const Avatar = ({ u }: { u: any }) => (
    <Link to={`/profile/${u.username}`} className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
      {u.avatar_url
        ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
        : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600">{(u.display_name || u.username)[0].toUpperCase()}</span>}
    </Link>
  )

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold text-agora-900 dark:text-agora-100">Friends</h1>

      <div className="flex gap-1 bg-agora-100 dark:bg-agora-800 rounded-lg p-1">
        {tabs.map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`flex-1 py-1.5 text-sm font-medium rounded-md transition-colors ${tab === t.id ? 'bg-white dark:bg-agora-700 text-agora-900 dark:text-agora-100 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
            {t.label}
          </button>
        ))}
      </div>

      {/* ── Friends tab ── */}
      {tab === 'friends' && (
        <div className="space-y-2">
          {friends.length === 0 && (
            <div className="card p-8 text-center text-agora-400"><Users size={32} className="mx-auto mb-2" /><p>No friends yet. Search for people to add!</p></div>
          )}
          {friends.map((f: any) => (
            <div key={f.id} className="card p-3 flex items-center gap-3">
              <Avatar u={f} />
              <div className="flex-1 min-w-0">
                <Link to={`/profile/${f.username}`} className="font-medium text-sm hover:underline">{f.display_name || f.username}</Link>
                <p className="text-xs text-agora-400">@{f.username}</p>
              </div>
              <button onClick={() => { if (confirm('Unfriend?')) unfriend.mutate(f.id) }} className="btn-ghost p-1.5 text-agora-400 hover:text-red-500">
                <Trash2 size={15} />
              </button>
            </div>
          ))}
        </div>
      )}

      {/* ── Requests tab ── */}
      {tab === 'requests' && (
        <div className="space-y-4">
          {incoming.length > 0 && <>
            <h3 className="font-semibold text-sm text-agora-700 dark:text-agora-300">Incoming</h3>
            {incoming.map((f: any) => (
              <div key={f.id} className="card p-3 flex items-center gap-3">
                <Avatar u={f} />
                <div className="flex-1 min-w-0">
                  <Link to={`/profile/${f.username}`} className="font-medium text-sm hover:underline">{f.display_name || f.username}</Link>
                  <p className="text-xs text-agora-400">@{f.username}</p>
                </div>
                <div className="flex gap-2">
                  <button onClick={() => accept.mutate(f.id)} className="btn-primary text-xs py-1 px-2"><UserCheck size={13} /> Accept</button>
                  <button onClick={() => decline.mutate(f.id)} className="btn-secondary text-xs py-1 px-2"><UserX size={13} /> Decline</button>
                </div>
              </div>
            ))}
          </>}
          {outgoing.length > 0 && <>
            <h3 className="font-semibold text-sm text-agora-700 dark:text-agora-300 mt-2">Sent</h3>
            {outgoing.map((f: any) => (
              <div key={f.id} className="card p-3 flex items-center gap-3">
                <Avatar u={f} />
                <div className="flex-1 min-w-0">
                  <Link to={`/profile/${f.username}`} className="font-medium text-sm hover:underline">{f.display_name || f.username}</Link>
                </div>
                <span className="text-xs text-agora-400">Pending</span>
              </div>
            ))}
          </>}
          {incoming.length === 0 && outgoing.length === 0 && (
            <div className="card p-8 text-center text-agora-400"><p>No pending requests.</p></div>
          )}
        </div>
      )}

      {/* ── Friend Lists tab ── */}
      {tab === 'lists' && (
        <div className="space-y-3">
          {/* Create new list */}
          <div className="flex gap-2">
            <input className="input flex-1" placeholder="New list name (e.g. Close Friends, Work…)"
              value={newListName} onChange={e => setNewListName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && newListName.trim() && createList.mutate(newListName.trim())} />
            <button onClick={() => newListName.trim() && createList.mutate(newListName.trim())}
              disabled={!newListName.trim() || createList.isPending}
              className="btn-primary px-3"><Plus size={16} /></button>
          </div>

          {lists.length === 0 && (
            <div className="card p-6 text-center text-agora-400 text-sm">
              <List size={28} className="mx-auto mb-2 opacity-50" />
              <p>No friend lists yet.</p>
              <p className="mt-1 text-xs">Create a list to share posts with a specific group of friends, or browse just their posts.</p>
            </div>
          )}

          {lists.map((list: any) => (
            <ListCard
              key={list.id}
              list={list}
              friends={friends}
              expanded={expandedList === list.id}
              onToggle={() => setExpandedList(expandedList === list.id ? null : list.id)}
              onDelete={() => { if (confirm(`Delete "${list.name}"?`)) deleteList.mutate(list.id) }}
              onAdd={(friendID) => addToList.mutate({ listID: list.id, friendID })}
              onRemove={(friendID) => removeFromList.mutate({ listID: list.id, friendID })}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ── ListCard ──────────────────────────────────────────────────────────────────

function ListCard({ list, friends, expanded, onToggle, onDelete, onAdd, onRemove }: {
  list: any
  friends: any[]
  expanded: boolean
  onToggle: () => void
  onDelete: () => void
  onAdd: (id: string) => void
  onRemove: (id: string) => void
}) {
  const { data } = useQuery({
    queryKey: ['list-members', list.id],
    queryFn: () => friendsApi.listGroupMembers(list.id).then(r => r.data),
    enabled: expanded,
  })
  const members: any[] = data?.members || []
  const memberIDs = new Set(members.map((m: any) => m.id))
  const nonMembers = friends.filter(f => !memberIDs.has(f.id))

  return (
    <div className="card overflow-hidden">
      {/* Header row */}
      <div className="flex items-center gap-3 p-3">
        <button onClick={onToggle} className="flex items-center gap-2 flex-1 text-left min-w-0">
          {expanded ? <ChevronDown size={16} className="text-agora-400 flex-shrink-0" /> : <ChevronRight size={16} className="text-agora-400 flex-shrink-0" />}
          <List size={15} className="text-agora-400 flex-shrink-0" />
          <span className="font-medium text-sm truncate">{list.name}</span>
          <span className="text-xs text-agora-400 flex-shrink-0">{list.member_count} {list.member_count === 1 ? 'person' : 'people'}</span>
        </button>
        <Link to={`/lists/${list.id}`}
          className="text-xs text-agora-500 hover:text-agora-700 dark:hover:text-agora-300 px-2 py-1 rounded hover:bg-agora-50 dark:hover:bg-agora-700 transition-colors flex-shrink-0">
          View feed
        </Link>
        <button onClick={onDelete} className="btn-ghost p-1.5 text-agora-400 hover:text-red-500 flex-shrink-0">
          <Trash2 size={14} />
        </button>
      </div>

      {/* Expanded member management */}
      {expanded && (
        <div className="border-t border-agora-100 dark:border-agora-700 p-3 space-y-3">
          {/* Current members */}
          {members.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide">Members</p>
              {members.map((m: any) => (
                <div key={m.id} className="flex items-center gap-2.5">
                  <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                    {m.avatar_url
                      ? <img src={m.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{(m.display_name || m.username)[0].toUpperCase()}</span>}
                  </div>
                  <div className="flex-1 min-w-0">
                    <span className="text-sm font-medium truncate">{m.display_name || m.username}</span>
                    <span className="text-xs text-agora-400 ml-1.5">@{m.username}</span>
                  </div>
                  <button onClick={() => onRemove(m.id)} className="btn-ghost p-1 text-agora-300 hover:text-red-500" title="Remove from list">
                    <UserMinus size={14} />
                  </button>
                </div>
              ))}
            </div>
          )}

          {/* Add friends not yet on this list */}
          {nonMembers.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide">Add friends</p>
              {nonMembers.map((f: any) => (
                <div key={f.id} className="flex items-center gap-2.5">
                  <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                    {f.avatar_url
                      ? <img src={f.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{(f.display_name || f.username)[0].toUpperCase()}</span>}
                  </div>
                  <div className="flex-1 min-w-0">
                    <span className="text-sm font-medium truncate">{f.display_name || f.username}</span>
                    <span className="text-xs text-agora-400 ml-1.5">@{f.username}</span>
                  </div>
                  <button onClick={() => onAdd(f.id)} className="btn-ghost p-1 text-agora-400 hover:text-agora-600 dark:hover:text-agora-200" title="Add to list">
                    <Plus size={14} />
                  </button>
                </div>
              ))}
            </div>
          )}

          {members.length === 0 && nonMembers.length === 0 && (
            <p className="text-sm text-agora-400 text-center py-2">Add friends first, then organize them into lists.</p>
          )}
        </div>
      )}
    </div>
  )
}
