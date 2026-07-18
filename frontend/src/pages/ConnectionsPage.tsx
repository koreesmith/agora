import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { friendsApi, federationApi, atprotoApi } from '../api'
import { handle } from '../utils/handle'
import { UserCheck, UserX, Users, Trash2, Plus, ChevronRight, ChevronDown, UserMinus, List, Search, UserPlus, Clock, Bell, BellOff, Globe, Home, Cloud } from 'lucide-react'
import FriendListModal from '../components/common/FriendListModal'

// AGORA-196: a fediverse actor federated through Bridgy Fed's bsky.brid.gy
// (or any *.brid.gy) is a Bluesky account, not a real fediverse one.
function isBridgedBlueskyInstance(instance?: string): boolean {
  return !!instance && /(^|\.)brid\.gy$/i.test(instance)
}

export default function ConnectionsPage() {
  const [searchParams] = useSearchParams()
  const [tab, setTab] = useState<'friends'|'requests'|'lists'|'fediverse'|'bluesky'>(
    (searchParams.get('tab') as any) || 'friends'
  )
  const [newListName, setNewListName] = useState('')
  const [expandedList, setExpandedList] = useState<string | null>(null)
  const [listModalFriend, setListModalFriend] = useState<any | null>(null)
  const qc = useQueryClient()
  const inv = (k: string) => qc.invalidateQueries({ queryKey: [k] })

  const { data: friendsData } = useQuery({ queryKey: ['friends'],       queryFn: () => friendsApi.listFriends().then(r => r.data) })
  const { data: reqData }     = useQuery({ queryKey: ['requests'],      queryFn: () => friendsApi.listRequests().then(r => r.data) })
  const { data: listsData }   = useQuery({ queryKey: ['friend-groups'], queryFn: () => friendsApi.listFriendLists().then(r => r.data) })

  // ── Fediverse follows (moved from the standalone Fediverse page) ──────────
  const [fediHandle, setFediHandle] = useState('')
  const [fediPreview, setFediPreview] = useState<any>(null)
  const [fediSearchError, setFediSearchError] = useState('')

  const { data: followingData } = useQuery({
    queryKey: ['fediverse-following'],
    queryFn: () => federationApi.listFollowing().then(r => r.data),
    enabled: tab === 'fediverse' || tab === 'lists',
  })
  const following: any[] = followingData?.following ?? []

  const resolveFediHandle = useMutation({
    mutationFn: (h: string) => federationApi.resolveFediverseHandle(h).then(r => r.data),
    onSuccess: (data) => { setFediPreview(data); setFediSearchError('') },
    onError: (e: any) => { setFediPreview(null); setFediSearchError(e.response?.data?.error || 'Could not resolve that handle.') },
  })
  const followFedi = useMutation({
    mutationFn: (actorUrl: string) => federationApi.followFediverseAccount(actorUrl),
    onSuccess: () => {
      setFediPreview(null)
      setFediHandle('')
      qc.invalidateQueries({ queryKey: ['fediverse-following'] })
    },
  })
  const unfollowFedi = useMutation({
    mutationFn: (id: string) => federationApi.unfollowFediverseAccount(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['fediverse-following'] }),
  })
  const toggleFediNotify = useMutation({
    mutationFn: ({ id, notify }: { id: string, notify: boolean }) => federationApi.toggleFollowNotify(id, notify),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['fediverse-following'] }),
  })
  const toggleFediShowInFeed = useMutation({
    mutationFn: ({ id, showInFeed }: { id: string, showInFeed: boolean }) => federationApi.toggleShowInFeed(id, showInFeed),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['fediverse-following'] }),
  })
  // AGORA-196: reconcile a Bridgy-Fed-bridged Bluesky follow to a native one.
  const migrateBridged = useMutation({
    mutationFn: (id: string) => atprotoApi.migrateBridgedFollow(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['fediverse-following'] })
      qc.invalidateQueries({ queryKey: ['bluesky-following'] })
    },
  })

  function handleFediSearch(e: React.FormEvent) {
    e.preventDefault()
    if (!fediHandle.trim()) return
    resolveFediHandle.mutate(fediHandle.trim())
  }

  const alreadyFollowingFedi = fediPreview && following.some(f => f.actor_url === fediPreview.actor_url)

  // ── Bluesky follows (AGORA-195) ────────────────────────────────────────────
  const [bskyHandle, setBskyHandle] = useState('')
  const [bskyPreview, setBskyPreview] = useState<any>(null)
  const [bskySearchError, setBskySearchError] = useState('')

  const { data: bskyFollowingData } = useQuery({
    queryKey: ['bluesky-following'],
    queryFn: () => atprotoApi.listBlueskyFollowing().then(r => r.data),
    enabled: tab === 'bluesky',
  })
  const bskyFollowing: any[] = bskyFollowingData?.following ?? []

  const resolveBskyHandle = useMutation({
    mutationFn: (h: string) => atprotoApi.resolveBlueskyHandle(h).then(r => r.data),
    onSuccess: (data) => { setBskyPreview(data); setBskySearchError('') },
    onError: (e: any) => { setBskyPreview(null); setBskySearchError(e.response?.data?.error || 'Could not resolve that handle.') },
  })
  const followBsky = useMutation({
    mutationFn: (actor: string) => atprotoApi.followBlueskyAccount(actor),
    onSuccess: () => {
      setBskyPreview(null)
      setBskyHandle('')
      qc.invalidateQueries({ queryKey: ['bluesky-following'] })
    },
    onError: (e: any) => setBskySearchError(e.response?.data?.error || 'Could not follow that account.'),
  })
  const unfollowBsky = useMutation({
    mutationFn: (id: string) => atprotoApi.unfollowBlueskyAccount(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['bluesky-following'] }),
  })
  const toggleBskyNotify = useMutation({
    mutationFn: ({ id, notify }: { id: string, notify: boolean }) => atprotoApi.toggleFollowNotify(id, notify),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['bluesky-following'] }),
  })

  function handleBskySearch(e: React.FormEvent) {
    e.preventDefault()
    if (!bskyHandle.trim()) return
    resolveBskyHandle.mutate(bskyHandle.trim())
  }

  const alreadyFollowingBsky = bskyPreview && bskyFollowing.some(f => f.did === bskyPreview.did)

  const accept   = useMutation({
    mutationFn: (friend: any) => friendsApi.acceptRequest(friend.id),
    onSuccess: (_, friend) => {
      inv('friends'); inv('requests')
      setListModalFriend(friend)
    },
  })
  const decline  = useMutation({ mutationFn: (id: string) => friendsApi.declineRequest(id), onSuccess: () => inv('requests') })
  const unfriend = useMutation({ mutationFn: (id: string) => friendsApi.unfriend(id),       onSuccess: () => inv('friends') })
  const createList = useMutation({
    mutationFn: (name: string) => friendsApi.createFriendList(name),
    onSuccess: () => { inv('friend-groups'); setNewListName('') },
  })
  const deleteList = useMutation({
    mutationFn: (id: string) => friendsApi.deleteFriendList(id),
    onSuccess: () => { inv('friend-groups'); setExpandedList(null) },
  })
  const addToList = useMutation({
    mutationFn: ({ listID, friendID }: { listID: string, friendID: string }) => friendsApi.addToFriendList(listID, friendID),
    onSuccess: (_,  { listID }) => qc.invalidateQueries({ queryKey: ['list-members', listID] }),
  })
  const removeFromList = useMutation({
    mutationFn: ({ listID, friendID }: { listID: string, friendID: string }) => friendsApi.removeFromFriendList(listID, friendID),
    onSuccess: (_, { listID }) => qc.invalidateQueries({ queryKey: ['list-members', listID] }),
  })

  const friends  = friendsData?.friends || []
  const incoming = reqData?.incoming || []
  const outgoing = reqData?.outgoing || []
  const lists    = listsData?.groups || []
  const pendingCount = incoming.length

  // AGORA-182: Friend Lists aren't friendship-only anymore — an accepted
  // fediverse follow (with a resolved cached user row) can join a list too,
  // read-side only. Merged here so ListCard doesn't need to know there are
  // two underlying relationship types.
  const fediverseConnections = following
    .filter((f: any) => f.accepted && f.user_id)
    .map((f: any) => ({ id: f.user_id, username: f.username, display_name: f.display_name, avatar_url: f.avatar_url, is_remote: true, remote_instance: f.instance }))
  const connections = [...friends, ...fediverseConnections]

  const tabs = [
    { id: 'friends',   label: `Friends (${friends.length})` },
    { id: 'requests',  label: `Requests${pendingCount ? ` (${pendingCount})` : ''}` },
    { id: 'lists',     label: 'Friend Lists' },
    { id: 'fediverse', label: 'Fediverse' },
    { id: 'bluesky',   label: 'Bluesky' },
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
      <h1 className="text-xl font-bold text-agora-900 dark:text-agora-100">Connections</h1>

      {listModalFriend && (
        <FriendListModal
          friend={listModalFriend}
          onClose={() => setListModalFriend(null)}
        />
      )}

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
                <p className="text-xs text-agora-400">{handle(f.username, f.is_remote, f.remote_instance)}</p>
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
                  <p className="text-xs text-agora-400">{handle(f.username, f.is_remote, f.remote_instance)}</p>
                </div>
                <div className="flex gap-2">
                  <button onClick={() => accept.mutate(f)} className="btn-primary text-xs py-1 px-2"><UserCheck size={13} /> Accept</button>
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
            <input className="input flex-1" autoComplete="off" placeholder="New list name (e.g. Close Friends, Work…)"
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
              connections={connections}
              expanded={expandedList === list.id}
              onToggle={() => setExpandedList(expandedList === list.id ? null : list.id)}
              onDelete={() => { if (confirm(`Delete "${list.name}"?`)) deleteList.mutate(list.id) }}
              onAdd={(friendID) => addToList.mutate({ listID: list.id, friendID })}
              onRemove={(friendID) => removeFromList.mutate({ listID: list.id, friendID })}
            />
          ))}
        </div>
      )}

      {/* ── Fediverse tab (moved from the standalone Fediverse page) ── */}
      {tab === 'fediverse' && (
        <div className="space-y-4">
          <div className="card p-5 space-y-4">
            <div>
              <h2 className="font-semibold text-sm">Follow a fediverse account</h2>
              <p className="text-xs text-agora-400 mt-1">
                Enter a full handle (e.g. <code>user@mastodon.social</code>) or a profile URL. There's no way to
                search the fediverse by name — like Mastodon's own remote search, you need the exact handle.
              </p>
            </div>
            <form onSubmit={handleFediSearch} className="flex gap-2">
              <input
                value={fediHandle}
                onChange={e => setFediHandle(e.target.value)}
                placeholder="user@instance.social"
                className="input flex-1 text-sm"
              />
              <button type="submit" disabled={resolveFediHandle.isPending || !fediHandle.trim()} className="btn-secondary text-sm flex items-center gap-1.5">
                <Search size={14} /> {resolveFediHandle.isPending ? 'Searching…' : 'Search'}
              </button>
            </form>
            {fediSearchError && <p className="text-sm text-red-500">{fediSearchError}</p>}

            {/* AGORA-196: a Bluesky account bridged into the fediverse via
                Bridgy Fed (handle@bsky.brid.gy) should be followed natively
                instead — no "follow via bridge" option offered for it. */}
            {fediPreview && isBridgedBlueskyInstance(fediPreview.instance) && (
              <div className="flex items-center gap-3 p-3 rounded-xl border border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/10">
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-amber-700 dark:text-amber-400">This is a Bluesky account</p>
                  <p className="text-xs text-agora-500 mt-1">
                    @{fediPreview.preferred_username} is a Bluesky account bridged into the fediverse — follow it natively from the Bluesky tab instead.
                  </p>
                </div>
                <button
                  onClick={() => { setTab('bluesky'); setBskyHandle(fediPreview.preferred_username); setFediPreview(null); setFediHandle('') }}
                  className="btn-secondary text-xs flex-shrink-0">
                  Go to Bluesky tab
                </button>
              </div>
            )}
            {fediPreview && !isBridgedBlueskyInstance(fediPreview.instance) && (
              <div className="flex items-center gap-3 p-3 rounded-xl border border-agora-100 dark:border-agora-700">
                <div className="w-12 h-12 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                  {fediPreview.icon_url
                    ? <img src={fediPreview.icon_url} alt="" className="w-full h-full object-cover" />
                    : <span className="w-full h-full flex items-center justify-center font-bold text-agora-500">
                        {(fediPreview.name || fediPreview.preferred_username || '?')[0]}
                      </span>}
                </div>
                <div className="flex-1 min-w-0">
                  <p className="font-medium text-sm truncate">{fediPreview.name || fediPreview.preferred_username}</p>
                  <p className="text-xs text-agora-400 truncate">@{fediPreview.preferred_username}@{fediPreview.instance}</p>
                  {fediPreview.summary && <p className="text-xs text-agora-500 mt-1 line-clamp-2">{fediPreview.summary}</p>}
                </div>
                <button
                  onClick={() => followFedi.mutate(fediPreview.actor_url)}
                  disabled={followFedi.isPending || alreadyFollowingFedi}
                  className="btn-primary text-xs flex items-center gap-1 flex-shrink-0">
                  <UserPlus size={13} /> {alreadyFollowingFedi ? 'Following' : followFedi.isPending ? 'Following…' : 'Follow'}
                </button>
              </div>
            )}
          </div>

          <div className="card p-5 space-y-3">
            <h2 className="font-semibold text-sm">Your follows</h2>
            {following.length > 0 && (
              <p className="text-xs text-agora-400">
                <List size={11} className="inline -mt-0.5" /> add to a friend list · <Home size={11} className="inline -mt-0.5" /> show in main feed (off by default) · <Bell size={11} className="inline -mt-0.5" /> notify on new posts
              </p>
            )}
            {following.length === 0 && (
              <p className="text-sm text-agora-400 italic py-3 text-center border border-dashed border-agora-200 dark:border-agora-700 rounded-lg">
                You're not following anyone on the fediverse yet.
              </p>
            )}
            <div className="space-y-2">
              {following.map(f => (
                <div key={f.id} className="flex items-center gap-3 py-2">
                  {f.username
                    ? <Link to={`/profile/${f.username}`} className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                        {f.avatar_url
                          ? <img src={f.avatar_url} alt="" className="w-full h-full object-cover" />
                          : <span className="w-full h-full flex items-center justify-center text-sm font-bold text-agora-500">
                              {(f.display_name || f.username || '?')[0]}
                            </span>}
                      </Link>
                    : <div className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                        {f.avatar_url
                          ? <img src={f.avatar_url} alt="" className="w-full h-full object-cover" />
                          : <span className="w-full h-full flex items-center justify-center text-sm font-bold text-agora-500">
                              {(f.display_name || f.username || '?')[0]}
                            </span>}
                      </div>}
                  <div className="flex-1 min-w-0">
                    {f.username
                      ? <Link to={`/profile/${f.username}`} className="font-medium text-sm truncate hover:underline block">{f.display_name || f.username}</Link>
                      : <p className="font-medium text-sm truncate">{f.display_name || f.actor_url}</p>}
                    {f.username && <p className="text-xs text-agora-400 truncate">@{f.username}</p>}
                  </div>
                  {!f.accepted && (
                    <span className="flex items-center gap-1 text-xs text-agora-400 flex-shrink-0">
                      <Clock size={12} /> Requested
                    </span>
                  )}
                  {f.accepted && isBridgedBlueskyInstance(f.instance) && (
                    <button
                      onClick={() => migrateBridged.mutate(f.id)}
                      disabled={migrateBridged.isPending}
                      title="This is a Bluesky account followed via the fediverse bridge — switch to following it natively"
                      className="btn-secondary text-xs flex items-center gap-1 flex-shrink-0">
                      <Cloud size={13} /> {migrateBridged.isPending ? 'Migrating…' : 'Migrate to Bluesky'}
                    </button>
                  )}
                  {f.accepted && f.user_id && (
                    <button
                      onClick={() => setListModalFriend({ id: f.user_id, username: f.username, display_name: f.display_name, avatar_url: f.avatar_url })}
                      className="flex-shrink-0 p-1.5 rounded-full text-agora-400 hover:text-agora-600 transition-colors"
                      title="Add to a friend list">
                      <List size={15} />
                    </button>
                  )}
                  {f.accepted && (
                    <button
                      onClick={() => toggleFediShowInFeed.mutate({ id: f.id, showInFeed: !f.show_in_feed })}
                      disabled={toggleFediShowInFeed.isPending}
                      title={f.show_in_feed ? 'Showing in main feed' : 'Not shown in main feed'}
                      className={`flex-shrink-0 p-1.5 rounded-full transition-colors ${f.show_in_feed ? 'text-agora-700 bg-agora-100 dark:bg-agora-700 dark:text-white' : 'text-agora-400 hover:text-agora-600'}`}>
                      <Home size={15} />
                    </button>
                  )}
                  {f.accepted && (
                    <button
                      onClick={() => toggleFediNotify.mutate({ id: f.id, notify: !f.notify })}
                      disabled={toggleFediNotify.isPending}
                      title={f.notify ? 'Notifications on for this account' : 'Notifications off for this account'}
                      className={`flex-shrink-0 p-1.5 rounded-full transition-colors ${f.notify ? 'text-agora-700 bg-agora-100 dark:bg-agora-700 dark:text-white' : 'text-agora-400 hover:text-agora-600'}`}>
                      {f.notify ? <Bell size={15} /> : <BellOff size={15} />}
                    </button>
                  )}
                  <button
                    onClick={() => unfollowFedi.mutate(f.id)}
                    disabled={unfollowFedi.isPending}
                    className="btn-secondary text-xs flex items-center gap-1 flex-shrink-0">
                    <UserMinus size={13} /> Unfollow
                  </button>
                </div>
              ))}
            </div>
          </div>

          <p className="text-xs text-agora-400 text-center">
            Want to opt out of the fediverse entirely? That toggle lives in{' '}
            <Link to="/settings?tab=fediverse" className="underline hover:text-agora-600">Settings → Fediverse</Link>.
          </p>
        </div>
      )}

      {/* ── Bluesky tab (AGORA-195) ── */}
      {tab === 'bluesky' && (
        <div className="space-y-4">
          <div className="card p-5 space-y-4">
            <div>
              <h2 className="font-semibold text-sm">Follow a Bluesky account</h2>
              <p className="text-xs text-agora-400 mt-1">
                Enter a full handle (e.g. <code>user.bsky.social</code>) or a DID.
              </p>
            </div>
            <form onSubmit={handleBskySearch} className="flex gap-2">
              <input
                value={bskyHandle}
                onChange={e => setBskyHandle(e.target.value)}
                placeholder="user.bsky.social"
                className="input flex-1 text-sm"
              />
              <button type="submit" disabled={resolveBskyHandle.isPending || !bskyHandle.trim()} className="btn-secondary text-sm flex items-center gap-1.5">
                <Search size={14} /> {resolveBskyHandle.isPending ? 'Searching…' : 'Search'}
              </button>
            </form>
            {bskySearchError && <p className="text-sm text-red-500">{bskySearchError}</p>}

            {bskyPreview && (
              <div className="flex items-center gap-3 p-3 rounded-xl border border-agora-100 dark:border-agora-700">
                <div className="w-12 h-12 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                  {bskyPreview.avatar_url
                    ? <img src={bskyPreview.avatar_url} alt="" className="w-full h-full object-cover" />
                    : <span className="w-full h-full flex items-center justify-center font-bold text-agora-500">
                        {(bskyPreview.display_name || bskyPreview.handle || '?')[0]}
                      </span>}
                </div>
                <div className="flex-1 min-w-0">
                  <p className="font-medium text-sm truncate">{bskyPreview.display_name || bskyPreview.handle}</p>
                  <p className="text-xs text-agora-400 truncate">@{bskyPreview.handle}</p>
                  {bskyPreview.description && <p className="text-xs text-agora-500 mt-1 line-clamp-2">{bskyPreview.description}</p>}
                </div>
                <button
                  onClick={() => followBsky.mutate(bskyPreview.did)}
                  disabled={followBsky.isPending || alreadyFollowingBsky}
                  className="btn-primary text-xs flex items-center gap-1 flex-shrink-0">
                  <UserPlus size={13} /> {alreadyFollowingBsky ? 'Following' : followBsky.isPending ? 'Following…' : 'Follow'}
                </button>
              </div>
            )}
          </div>

          <div className="card p-5 space-y-3">
            <h2 className="font-semibold text-sm">Your follows</h2>
            {bskyFollowing.length > 0 && (
              <p className="text-xs text-agora-400">
                <Bell size={11} className="inline -mt-0.5" /> notify on new posts
              </p>
            )}
            {bskyFollowing.length === 0 && (
              <p className="text-sm text-agora-400 italic py-3 text-center border border-dashed border-agora-200 dark:border-agora-700 rounded-lg">
                You're not following anyone on Bluesky yet.
              </p>
            )}
            <div className="space-y-2">
              {bskyFollowing.map(f => (
                <div key={f.id} className="flex items-center gap-3 py-2">
                  <div className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                    {f.avatar_url
                      ? <img src={f.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center text-sm font-bold text-agora-500">
                          {(f.display_name || f.handle || '?')[0]}
                        </span>}
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="font-medium text-sm truncate">{f.display_name || f.handle}</p>
                    <p className="text-xs text-agora-400 truncate">@{f.handle}</p>
                  </div>
                  <button
                    onClick={() => toggleBskyNotify.mutate({ id: f.id, notify: !f.notify })}
                    disabled={toggleBskyNotify.isPending}
                    title={f.notify ? 'Notifications on for this account' : 'Notifications off for this account'}
                    className={`flex-shrink-0 p-1.5 rounded-full transition-colors ${f.notify ? 'text-agora-700 bg-agora-100 dark:bg-agora-700 dark:text-white' : 'text-agora-400 hover:text-agora-600'}`}>
                    {f.notify ? <Bell size={15} /> : <BellOff size={15} />}
                  </button>
                  <button
                    onClick={() => unfollowBsky.mutate(f.id)}
                    disabled={unfollowBsky.isPending}
                    className="btn-secondary text-xs flex items-center gap-1 flex-shrink-0">
                    <UserMinus size={13} /> Unfollow
                  </button>
                </div>
              ))}
            </div>
          </div>

          <p className="text-xs text-agora-400 text-center">
            Want to opt out of Bluesky entirely? That toggle lives in{' '}
            <Link to="/settings?tab=bluesky" className="underline hover:text-agora-600">Settings → Bluesky</Link>.
          </p>
        </div>
      )}
    </div>
  )
}

// ── ListCard ──────────────────────────────────────────────────────────────────

function ListCard({ list, connections, expanded, onToggle, onDelete, onAdd, onRemove }: {
  list: any
  connections: any[]
  expanded: boolean
  onToggle: () => void
  onDelete: () => void
  onAdd: (id: string) => void
  onRemove: (id: string) => void
}) {
  const { data } = useQuery({
    queryKey: ['list-members', list.id],
    queryFn: () => friendsApi.listFriendListMembers(list.id).then(r => r.data),
    enabled: expanded,
  })
  const members: any[] = data?.members || []
  const memberIDs = new Set(members.map((m: any) => m.id))
  const nonMembers = connections.filter(f => !memberIDs.has(f.id))

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
                    <span className="text-xs text-agora-400 ml-1.5">{handle(m.username, m.is_remote, m.remote_instance)}</span>
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
              <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide">Add people</p>
              {nonMembers.map((f: any) => (
                <div key={f.id} className="flex items-center gap-2.5">
                  <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                    {f.avatar_url
                      ? <img src={f.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{(f.display_name || f.username)[0].toUpperCase()}</span>}
                  </div>
                  <div className="flex-1 min-w-0">
                    <span className="text-sm font-medium truncate">{f.display_name || f.username}</span>
                    <span className="text-xs text-agora-400 ml-1.5">{handle(f.username, f.is_remote, f.remote_instance)}</span>
                  </div>
                  <button onClick={() => onAdd(f.id)} className="btn-ghost p-1 text-agora-400 hover:text-agora-600 dark:hover:text-agora-200" title="Add to list">
                    <Plus size={14} />
                  </button>
                </div>
              ))}
            </div>
          )}

          {members.length === 0 && nonMembers.length === 0 && (
            <p className="text-sm text-agora-400 text-center py-2">Add friends or fediverse follows first, then organize them into lists.</p>
          )}
        </div>
      )}
    </div>
  )
}
