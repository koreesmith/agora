import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { usersApi, friendsApi, federationApi, atprotoApi } from '../api'
import { UserPlus, Users, Compass, Check, Clock, Globe, X } from 'lucide-react'
import { handle } from '../utils/handle'

export default function DiscoverPage() {
  const qc = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['discover'],
    queryFn: () => usersApi.discover().then(r => r.data),
  })

  // Dismissed people disappear at once; the server remembers them so they
  // stay gone on the next load (AGORA-423).
  const [dismissed, setDismissed] = useState<Set<string>>(new Set())
  const dismiss = (kind: 'user' | 'peer' | 'fediverse' | 'bluesky', key: string) => {
    setDismissed(prev => new Set(prev).add(`${kind}:${key}`))
    usersApi.dismissSuggestion(kind, key).catch(() => {
      setDismissed(prev => { const n = new Set(prev); n.delete(`${kind}:${key}`); return n })
    })
  }
  const visible = (kind: string, key: string) => !dismissed.has(`${kind}:${key}`)

  const users: any[] = (data?.users || []).filter((u: any) => visible('user', u.id))
  // AGORA-364: previews from instances this one is directly federated
  // with, kept separate from the local suggestions above since they carry
  // no id, mutual-friend count, or friend status the way those do.
  const federatedSuggestions: any[] = (data?.federated_suggestions || [])
    .filter((u: any) => visible('peer', `${u.username}@${u.instance}`))
  // AGORA-423: Fediverse and Bluesky accounts, each in its own section so no
  // network crowds out the others.
  const fediverseSuggestions: any[] = (data?.fediverse_suggestions || [])
    .filter((u: any) => visible('fediverse', u.actor_url))
  const blueskySuggestions: any[] = (data?.bluesky_suggestions || [])
    .filter((u: any) => visible('bluesky', u.did))
  const invalidate = () => qc.invalidateQueries({ queryKey: ['discover'] })
  const nothing = users.length === 0 && federatedSuggestions.length === 0
    && fediverseSuggestions.length === 0 && blueskySuggestions.length === 0

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Compass size={22} className="text-agora-600" />
        <h1 className="text-xl font-bold">Find Friends</h1>
      </div>
      <p className="text-sm text-agora-500">People you might know, from Agora, the Fediverse and Bluesky.</p>

      {isLoading && (
        <div className="text-center py-12 text-agora-400">Finding people you might know…</div>
      )}

      {!isLoading && nothing && (
        <div className="card p-12 text-center text-agora-400 space-y-2">
          <Users size={32} className="mx-auto" />
          <p className="font-medium">No suggestions yet.</p>
          <p className="text-sm">Add more friends to see people you might know.</p>
        </div>
      )}

      {users.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2">
          {users.map((u: any) => (
            <UserCard key={u.id} user={u} onRequestSent={invalidate} onDismiss={() => dismiss('user', u.id)} />
          ))}
        </div>
      )}

      {federatedSuggestions.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-sm font-semibold text-agora-500 flex items-center gap-1.5">
            <Globe size={14} /> On federated Agora instances
          </h2>
          <div className="grid gap-3 sm:grid-cols-2">
            {federatedSuggestions.map((u: any) => (
              <FederatedUserCard key={`${u.username}@${u.instance}`} user={u} onRequestSent={invalidate}
                onDismiss={() => dismiss('peer', `${u.username}@${u.instance}`)} />
            ))}
          </div>
        </div>
      )}

      {fediverseSuggestions.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-sm font-semibold text-agora-500 flex items-center gap-1.5">
            <Globe size={14} /> On the Fediverse
          </h2>
          <div className="grid gap-3 sm:grid-cols-2">
            {fediverseSuggestions.map((u: any) => (
              <FollowSuggestionCard key={u.actor_url}
                name={u.display_name || u.username} handleText={handle(u.username, true, u.instance)}
                avatarUrl={u.avatar_url} bio={u.bio} reason={u.reason} lookup={`${u.username}@${u.instance}`}
                follow={() => federationApi.followFediverseAccount(u.actor_url)}
                onDismiss={() => dismiss('fediverse', u.actor_url)} />
            ))}
          </div>
        </div>
      )}

      {blueskySuggestions.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-sm font-semibold text-agora-500 flex items-center gap-1.5">
            <Globe size={14} /> On Bluesky
          </h2>
          <div className="grid gap-3 sm:grid-cols-2">
            {blueskySuggestions.map((u: any) => (
              <FollowSuggestionCard key={u.did}
                name={u.display_name || u.handle} handleText={`@${u.handle}`}
                avatarUrl={u.avatar_url} bio={u.bio} reason={u.reason} lookup={u.handle}
                follow={() => atprotoApi.followBlueskyAccount(u.did)}
                onDismiss={() => dismiss('bluesky', u.did)} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// AGORA-454: a suggested Fediverse, Bluesky or peer-instance account is not
// stored here yet, so there is no local profile to link to. Opening one looks
// the handle up (which creates the profile, as AGORA-421 does for search
// previews) and then goes to it.
function useOpenProfile() {
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const open = useMutation({
    mutationFn: (h: string) => federationApi.lookupUser(h).then(r => r.data),
    onMutate: () => setError(''),
    onSuccess: (data) => {
      if (data?.user?.username) navigate(`/profile/${data.user.username}`)
      else setError('Could not open that profile.')
    },
    onError: (e: any) => setError(e.response?.data?.error || 'Could not open that profile.'),
  })
  return { open: (h: string) => open.mutate(h), pending: open.isPending, error }
}

// A small dismiss control shared by every suggestion card.
function DismissButton({ onDismiss }: { onDismiss: () => void }) {
  return (
    <button onClick={onDismiss} title="Not interested" aria-label="Not interested"
      className="absolute top-2 right-2 p-1 rounded-full text-agora-400 hover:text-agora-600 hover:bg-agora-100 dark:hover:bg-agora-700">
      <X size={14} />
    </button>
  )
}

function UserCard({ user: u, onRequestSent, onDismiss }: { user: any, onRequestSent: () => void, onDismiss: () => void }) {
  // Optimistic local state — starts from server-provided status
  const [status, setStatus] = useState<string>(u.friend_status || '')

  const sendReq = useMutation({
    mutationFn: () => friendsApi.sendRequest(u.id),
    onMutate: () => setStatus('pending'),
    onError: () => setStatus(''),
    onSuccess: onRequestSent,
  })

  return (
    <div className="card p-4 flex gap-3 relative">
      <DismissButton onDismiss={onDismiss} />
      <Link to={`/profile/${u.username}`} className="flex-shrink-0">
        <div className="w-12 h-12 rounded-full bg-agora-100 dark:bg-agora-700 overflow-hidden">
          {u.avatar_url
            ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
            : <div className="w-full h-full flex items-center justify-center text-agora-400 font-bold text-lg">
                {(u.display_name || u.username).charAt(0).toUpperCase()}
              </div>
          }
        </div>
      </Link>

      <div className="flex-1 min-w-0">
        <Link to={`/profile/${u.username}`} className="font-semibold hover:underline block truncate pr-6">
          {u.display_name || u.username}
        </Link>
        <Link to={`/profile/${u.username}`} className="text-xs text-agora-400 block">
          {handle(u.username, u.is_remote, u.remote_instance)}
        </Link>
        {u.bio && (
          <p className="text-xs text-agora-500 mt-1 line-clamp-1">{u.bio}</p>
        )}

        <ul className="mt-1.5 space-y-0.5 text-xs text-agora-500">
          {(u.reasons?.length ? u.reasons : ['Member of this instance']).slice(0, 3).map((r: string) => (
            <li key={r} className="flex items-start gap-1">
              <Users size={11} className="mt-0.5 flex-shrink-0" /> <span>{r}</span>
            </li>
          ))}
        </ul>

        <div className="mt-2">
          {status === 'pending' ? (
            <span className="inline-flex items-center gap-1.5 text-xs text-agora-400 font-medium py-1 px-3 rounded-lg bg-agora-100 dark:bg-agora-700">
              <Clock size={12} /> Request sent
            </span>
          ) : status === 'pending_incoming' ? (
            <Link to={`/profile/${u.username}`}
              className="inline-flex items-center gap-1.5 text-xs text-agora-600 dark:text-agora-400 font-medium py-1 px-3 rounded-lg bg-agora-100 dark:bg-agora-700">
              <Check size={12} /> Respond to request
            </Link>
          ) : (
            <button
              onClick={() => sendReq.mutate()}
              disabled={sendReq.isPending}
              className="btn-primary text-xs py-1 px-3 flex items-center gap-1"
            >
              <UserPlus size={12} /> Add friend
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// AGORA-364: a preview from a peer's own /federation/search sample, not a
// local user row, so there's no id to send a friend request against yet.
// Adding one resolves it through LookupUser first (the same handle-lookup
// path unified search uses), which creates the local stub and hands back an
// id, then sends the request against that id.
function FederatedUserCard({ user: u, onRequestSent, onDismiss }: { user: any, onRequestSent: () => void, onDismiss: () => void }) {
  const [status, setStatus] = useState<'' | 'resolving' | 'sent' | 'error'>('')
  const profile = useOpenProfile()
  const lookupHandle = `${u.username}@${u.instance}`

  const addFriend = useMutation({
    mutationFn: async () => {
      const lookup = await federationApi.lookupUser(`${u.username}@${u.instance}`)
      const id = lookup.data?.user?.id
      if (!id) throw new Error('could not resolve this account')
      await friendsApi.sendRequest(id)
    },
    onMutate: () => setStatus('resolving'),
    onSuccess: () => { setStatus('sent'); onRequestSent() },
    onError: () => setStatus('error'),
  })

  return (
    <div className="card p-4 flex gap-3 relative">
      <DismissButton onDismiss={onDismiss} />
      <button onClick={() => profile.open(lookupHandle)} disabled={profile.pending} aria-label={`Open ${u.display_name || u.username}'s profile`}
        className="flex-shrink-0 w-12 h-12 rounded-full bg-agora-100 dark:bg-agora-700 overflow-hidden">
        {u.avatar_url
          ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
          : <div className="w-full h-full flex items-center justify-center text-agora-400 font-bold text-lg">
              {(u.display_name || u.username).charAt(0).toUpperCase()}
            </div>
        }
      </button>

      <div className="flex-1 min-w-0">
        <button onClick={() => profile.open(lookupHandle)} disabled={profile.pending}
          className="font-semibold hover:underline block truncate pr-6 max-w-full text-left">{u.display_name || u.username}</button>
        <p className="text-xs text-agora-400">{handle(u.username, true, u.instance)}</p>
        {profile.error && <p className="text-xs text-red-500 mt-0.5">{profile.error}</p>}

        <div className="flex items-center gap-1 mt-1.5 text-xs text-agora-400">
          <Globe size={11} /> On {u.instance}
        </div>

        <div className="mt-2">
          {status === 'sent' ? (
            <span className="inline-flex items-center gap-1.5 text-xs text-agora-400 font-medium py-1 px-3 rounded-lg bg-agora-100 dark:bg-agora-700">
              <Clock size={12} /> Request sent
            </span>
          ) : (
            <button
              onClick={() => addFriend.mutate()}
              disabled={status === 'resolving'}
              className="btn-primary text-xs py-1 px-3 flex items-center gap-1"
            >
              <UserPlus size={12} /> {status === 'resolving' ? 'Adding…' : status === 'error' ? 'Try again' : 'Add friend'}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// AGORA-423: a Fediverse or Bluesky account. Unlike a friend request these are
// follows, so there is nothing to wait on once it goes through.
function FollowSuggestionCard({ name, handleText, avatarUrl, bio, reason, lookup, follow, onDismiss }: {
  name: string, handleText: string, avatarUrl?: string, bio?: string, reason?: string, lookup: string,
  follow: () => Promise<unknown>, onDismiss: () => void,
}) {
  const [status, setStatus] = useState<'' | 'working' | 'done' | 'error'>('')
  const profile = useOpenProfile()

  const run = useMutation({
    mutationFn: follow,
    onMutate: () => setStatus('working'),
    onSuccess: () => setStatus('done'),
    onError: () => setStatus('error'),
  })

  return (
    <div className="card p-4 flex gap-3 relative">
      <DismissButton onDismiss={onDismiss} />
      <button onClick={() => profile.open(lookup)} disabled={profile.pending} aria-label={`Open ${name}'s profile`}
        className="flex-shrink-0 w-12 h-12 rounded-full bg-agora-100 dark:bg-agora-700 overflow-hidden">
        {avatarUrl
          ? <img src={avatarUrl} alt="" className="w-full h-full object-cover" />
          : <div className="w-full h-full flex items-center justify-center text-agora-400 font-bold text-lg">
              {name.charAt(0).toUpperCase()}
            </div>
        }
      </button>

      <div className="flex-1 min-w-0">
        <button onClick={() => profile.open(lookup)} disabled={profile.pending}
          className="font-semibold hover:underline block truncate pr-6 max-w-full text-left">{name}</button>
        <p className="text-xs text-agora-400 truncate">{handleText}</p>
        {profile.error && <p className="text-xs text-red-500 mt-0.5">{profile.error}</p>}
        {bio && <p className="text-xs text-agora-500 mt-1 line-clamp-1">{bio}</p>}
        {reason && (
          <div className="flex items-center gap-1 mt-1.5 text-xs text-agora-500">
            <Users size={11} /> {reason}
          </div>
        )}

        <div className="mt-2">
          {status === 'done' ? (
            <span className="inline-flex items-center gap-1.5 text-xs text-agora-400 font-medium py-1 px-3 rounded-lg bg-agora-100 dark:bg-agora-700">
              <Check size={12} /> Following
            </span>
          ) : (
            <button
              onClick={() => run.mutate()}
              disabled={status === 'working'}
              className="btn-primary text-xs py-1 px-3 flex items-center gap-1"
            >
              <UserPlus size={12} /> {status === 'working' ? 'Following…' : status === 'error' ? 'Try again' : 'Follow'}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
