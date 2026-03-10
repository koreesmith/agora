import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { friendsApi } from '../api'
import { UserCheck, UserX, UserPlus, Users, Trash2, Plus } from 'lucide-react'

export default function FriendsPage() {
  const [tab, setTab] = useState<'friends'|'requests'|'groups'>('friends')
  const [newGroup, setNewGroup] = useState('')
  const qc = useQueryClient()
  const inv = (k: string) => qc.invalidateQueries({ queryKey: [k] })

  const { data: friendsData } = useQuery({ queryKey:['friends'], queryFn: () => friendsApi.listFriends().then(r=>r.data) })
  const { data: reqData }     = useQuery({ queryKey:['requests'], queryFn: () => friendsApi.listRequests().then(r=>r.data) })
  const { data: groupsData }  = useQuery({ queryKey:['friend-groups'], queryFn: () => friendsApi.listGroups().then(r=>r.data) })

  const accept  = useMutation({ mutationFn:(id:string)=>friendsApi.acceptRequest(id), onSuccess:()=>{ inv('friends'); inv('requests') } })
  const decline = useMutation({ mutationFn:(id:string)=>friendsApi.declineRequest(id), onSuccess:()=>inv('requests') })
  const unfriend= useMutation({ mutationFn:(id:string)=>friendsApi.unfriend(id), onSuccess:()=>inv('friends') })
  const createG = useMutation({ mutationFn:(name:string)=>friendsApi.createGroup(name), onSuccess:()=>{ inv('friend-groups'); setNewGroup('') } })
  const deleteG = useMutation({ mutationFn:(id:string)=>friendsApi.deleteGroup(id), onSuccess:()=>inv('friend-groups') })

  const friends  = friendsData?.friends || []
  const incoming = reqData?.incoming || []
  const outgoing = reqData?.outgoing || []
  const groups   = groupsData?.groups || []
  const pendingCount = incoming.length

  const tabs = [
    { id:'friends',  label:`Friends (${friends.length})` },
    { id:'requests', label:`Requests${pendingCount ? ` (${pendingCount})` : ''}` },
    { id:'groups',   label:'Groups' },
  ]

  const Avatar = ({ u }: { u: any }) => (
    <Link to={`/profile/${u.username}`} className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
      {u.avatar_url
        ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
        : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600">{(u.display_name||u.username)[0].toUpperCase()}</span>
      }
    </Link>
  )

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold text-agora-900 dark:text-agora-100">Friends</h1>
      <div className="flex gap-1 bg-agora-100 dark:bg-agora-800 rounded-lg p-1">
        {tabs.map(t => (
          <button key={t.id} onClick={() => setTab(t.id as any)}
            className={`flex-1 py-1.5 text-sm font-medium rounded-md transition-colors ${tab===t.id ? 'bg-white dark:bg-agora-700 text-agora-900 dark:text-agora-100 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'friends' && (
        <div className="space-y-2">
          {friends.length === 0 && <div className="card p-8 text-center text-agora-400"><Users size={32} className="mx-auto mb-2"/><p>No friends yet. Search for people to add!</p></div>}
          {friends.map((f:any) => (
            <div key={f.id} className="card p-3 flex items-center gap-3">
              <Avatar u={f} />
              <div className="flex-1 min-w-0">
                <Link to={`/profile/${f.username}`} className="font-medium text-sm hover:underline">{f.display_name||f.username}</Link>
                <p className="text-xs text-agora-400">@{f.username}</p>
              </div>
              <button onClick={() => { if(confirm('Unfriend?')) unfriend.mutate(f.id) }} className="btn-ghost p-1.5 text-agora-400 hover:text-red-500">
                <Trash2 size={15}/>
              </button>
            </div>
          ))}
        </div>
      )}

      {tab === 'requests' && (
        <div className="space-y-4">
          {incoming.length > 0 && <>
            <h3 className="font-semibold text-sm text-agora-700 dark:text-agora-300">Incoming</h3>
            {incoming.map((f:any) => (
              <div key={f.id} className="card p-3 flex items-center gap-3">
                <Avatar u={f} />
                <div className="flex-1 min-w-0">
                  <Link to={`/profile/${f.username}`} className="font-medium text-sm hover:underline">{f.display_name||f.username}</Link>
                  <p className="text-xs text-agora-400">@{f.username}</p>
                </div>
                <div className="flex gap-2">
                  <button onClick={() => accept.mutate(f.id)} className="btn-primary text-xs py-1 px-2"><UserCheck size={13}/> Accept</button>
                  <button onClick={() => decline.mutate(f.id)} className="btn-secondary text-xs py-1 px-2"><UserX size={13}/> Decline</button>
                </div>
              </div>
            ))}
          </>}
          {outgoing.length > 0 && <>
            <h3 className="font-semibold text-sm text-agora-700 dark:text-agora-300 mt-2">Sent</h3>
            {outgoing.map((f:any) => (
              <div key={f.id} className="card p-3 flex items-center gap-3">
                <Avatar u={f} />
                <div className="flex-1 min-w-0">
                  <Link to={`/profile/${f.username}`} className="font-medium text-sm hover:underline">{f.display_name||f.username}</Link>
                </div>
                <span className="text-xs text-agora-400">Pending</span>
              </div>
            ))}
          </>}
          {incoming.length === 0 && outgoing.length === 0 && <div className="card p-8 text-center text-agora-400"><p>No pending requests.</p></div>}
        </div>
      )}

      {tab === 'groups' && (
        <div className="space-y-3">
          <div className="flex gap-2">
            <input className="input" placeholder="New group name" value={newGroup} onChange={e=>setNewGroup(e.target.value)}
              onKeyDown={e=>e.key==='Enter'&&newGroup.trim()&&createG.mutate(newGroup.trim())} />
            <button onClick={() => newGroup.trim()&&createG.mutate(newGroup.trim())} className="btn-primary"><Plus size={16}/></button>
          </div>
          {groups.map((g:any) => (
            <div key={g.id} className="card p-3 flex items-center gap-3">
              <Users size={16} className="text-agora-400"/>
              <div className="flex-1">
                <span className="font-medium text-sm">{g.name}</span>
                <span className="text-xs text-agora-400 ml-2">{g.member_count} members</span>
              </div>
              <button onClick={() => { if(confirm('Delete group?')) deleteG.mutate(g.id) }} className="btn-ghost p-1.5 text-agora-400 hover:text-red-500"><Trash2 size={15}/></button>
            </div>
          ))}
          {groups.length === 0 && <div className="card p-6 text-center text-agora-400 text-sm">No groups yet. Create one to organize your friends.</div>}
        </div>
      )}
    </div>
  )
}
