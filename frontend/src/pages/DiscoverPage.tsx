import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { usersApi, friendsApi, federationApi } from '../api'
import { UserPlus, Users, Compass, Check, Clock, Globe } from 'lucide-react'
import { handle } from '../utils/handle'

export default function DiscoverPage() {
  const qc = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['discover'],
    queryFn: () => usersApi.discover().then(r => r.data),
  })

  const users: any[] = data?.users || []
  // AGORA-364: previews from instances this one is directly federated
  // with, kept separate from the local suggestions above since they carry
  // no id, mutual-friend count, or friend status the way those do.
  const federatedSuggestions: any[] = data?.federated_suggestions || []
  const invalidate = () => qc.invalidateQueries({ queryKey: ['discover'] })

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Compass size={22} className="text-agora-600" />
        <h1 className="text-xl font-bold">Find Friends</h1>
      </div>
      <p className="text-sm text-agora-500">People you might know through your friends.</p>

      {isLoading && (
        <div className="text-center py-12 text-agora-400">Finding people you might know…</div>
      )}

      {!isLoading && users.length === 0 && federatedSuggestions.length === 0 && (
        <div className="card p-12 text-center text-agora-400 space-y-2">
          <Users size={32} className="mx-auto" />
          <p className="font-medium">No suggestions yet.</p>
          <p className="text-sm">Add more friends to see people you might know.</p>
        </div>
      )}

      {users.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2">
          {users.map((u: any) => (
            <UserCard key={u.id} user={u} onRequestSent={invalidate} />
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
              <FederatedUserCard key={`${u.username}@${u.instance}`} user={u} onRequestSent={invalidate} />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function UserCard({ user: u, onRequestSent }: { user: any, onRequestSent: () => void }) {
  // Optimistic local state — starts from server-provided status
  const [status, setStatus] = useState<string>(u.friend_status || '')

  const sendReq = useMutation({
    mutationFn: () => friendsApi.sendRequest(u.id),
    onMutate: () => setStatus('pending'),
    onError: () => setStatus(''),
    onSuccess: onRequestSent,
  })

  return (
    <div className="card p-4 flex gap-3">
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
        <Link to={`/profile/${u.username}`} className="font-semibold hover:underline block truncate">
          {u.display_name || u.username}
        </Link>
        <Link to={`/profile/${u.username}`} className="text-xs text-agora-400 block">
          {handle(u.username, u.is_remote, u.remote_instance)}
        </Link>
        {u.bio && (
          <p className="text-xs text-agora-500 mt-1 line-clamp-1">{u.bio}</p>
        )}

        <div className="flex items-center gap-1 mt-1.5 text-xs text-agora-500">
          <Users size={11} />
          {u.mutual_count > 0 ? (
            <span>
              {u.mutual_count} mutual friend{u.mutual_count !== 1 ? 's' : ''}
              {u.mutual_friends?.length > 0 && (
                <span className="text-agora-400">
                  {' '}· {u.mutual_friends.slice(0, 3).join(', ')}
                  {u.mutual_count > 3 ? ` +${u.mutual_count - 3} more` : ''}
                </span>
              )}
            </span>
          ) : (
            <span className="text-agora-400">Member of this instance</span>
          )}
        </div>

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
function FederatedUserCard({ user: u, onRequestSent }: { user: any, onRequestSent: () => void }) {
  const [status, setStatus] = useState<'' | 'resolving' | 'sent' | 'error'>('')

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
    <div className="card p-4 flex gap-3">
      <div className="flex-shrink-0 w-12 h-12 rounded-full bg-agora-100 dark:bg-agora-700 overflow-hidden">
        {u.avatar_url
          ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
          : <div className="w-full h-full flex items-center justify-center text-agora-400 font-bold text-lg">
              {(u.display_name || u.username).charAt(0).toUpperCase()}
            </div>
        }
      </div>

      <div className="flex-1 min-w-0">
        <p className="font-semibold truncate">{u.display_name || u.username}</p>
        <p className="text-xs text-agora-400">{handle(u.username, true, u.instance)}</p>

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
