import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { usersApi, friendsApi } from '../api'
import { UserPlus, Users, Compass } from 'lucide-react'

export default function DiscoverPage() {
  const qc = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['discover'],
    queryFn: () => usersApi.discover().then(r => r.data),
  })

  const sendReq = useMutation({
    mutationFn: (userId: string) => friendsApi.sendRequest(userId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['discover'] }),
  })

  const users: any[] = data?.users || []

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Compass size={22} className="text-agora-600" />
        <h1 className="text-xl font-bold">Discover People</h1>
      </div>
      <p className="text-sm text-agora-500">People you might know through your friends.</p>

      {isLoading && (
        <div className="text-center py-12 text-agora-400">Finding people you might know…</div>
      )}

      {!isLoading && users.length === 0 && (
        <div className="card p-12 text-center text-agora-400 space-y-2">
          <Users size={32} className="mx-auto" />
          <p className="font-medium">No suggestions yet.</p>
          <p className="text-sm">Add more friends to see people you might know.</p>
        </div>
      )}

      <div className="grid gap-3 sm:grid-cols-2">
        {users.map((u: any) => (
          <div key={u.id} className="card p-4 flex gap-3">
            {/* Avatar */}
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

            {/* Info */}
            <div className="flex-1 min-w-0">
              <Link to={`/profile/${u.username}`} className="font-semibold hover:underline block truncate">
                {u.display_name || u.username}
              </Link>
              <Link to={`/profile/${u.username}`} className="text-xs text-agora-400 block">
                @{u.username}
              </Link>
              {u.bio && (
                <p className="text-xs text-agora-500 mt-1 line-clamp-1">{u.bio}</p>
              )}

              {/* Mutual friends */}
              <div className="flex items-center gap-1 mt-1.5 text-xs text-agora-500">
                <Users size={11} />
                <span>
                  {u.mutual_count} mutual friend{u.mutual_count !== 1 ? 's' : ''}
                  {u.mutual_friends?.length > 0 && (
                    <span className="text-agora-400">
                      {' '}· {u.mutual_friends.slice(0, 3).join(', ')}
                      {u.mutual_count > 3 ? ` +${u.mutual_count - 3} more` : ''}
                    </span>
                  )}
                </span>
              </div>

              {/* Add friend button */}
              <button
                onClick={() => sendReq.mutate(u.id)}
                disabled={sendReq.isPending}
                className="btn-primary text-xs py-1 px-3 mt-2 flex items-center gap-1"
              >
                <UserPlus size={12} /> Add friend
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
