import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { federationApi } from '../api'
import { Search, UserPlus, UserMinus, Clock } from 'lucide-react'

export default function FediversePage() {
  const qc = useQueryClient()
  const [handle, setHandle] = useState('')
  const [preview, setPreview] = useState<any>(null)
  const [searchError, setSearchError] = useState('')

  const { data: followingData } = useQuery({
    queryKey: ['fediverse-following'],
    queryFn: () => federationApi.listFollowing().then(r => r.data),
  })
  const following: any[] = followingData?.following ?? []

  const resolve = useMutation({
    mutationFn: (h: string) => federationApi.resolveFediverseHandle(h).then(r => r.data),
    onSuccess: (data) => { setPreview(data); setSearchError('') },
    onError: (e: any) => { setPreview(null); setSearchError(e.response?.data?.error || 'Could not resolve that handle.') },
  })

  const follow = useMutation({
    mutationFn: (actorUrl: string) => federationApi.followFediverseAccount(actorUrl),
    onSuccess: () => {
      setPreview(null)
      setHandle('')
      qc.invalidateQueries({ queryKey: ['fediverse-following'] })
    },
  })

  const unfollow = useMutation({
    mutationFn: (id: string) => federationApi.unfollowFediverseAccount(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['fediverse-following'] }),
  })

  function handleSearch(e: React.FormEvent) {
    e.preventDefault()
    if (!handle.trim()) return
    resolve.mutate(handle.trim())
  }

  const alreadyFollowing = preview && following.some(f => f.actor_url === preview.actor_url)

  return (
    <div className="max-w-xl mx-auto space-y-6">
      <h1 className="text-xl font-bold">Fediverse</h1>

      <div className="card p-5 space-y-4">
        <div>
          <h2 className="font-semibold text-sm">Follow a fediverse account</h2>
          <p className="text-xs text-agora-400 mt-1">
            Enter a full handle (e.g. <code>user@mastodon.social</code>) or a profile URL. There's no way to search
            the fediverse by name — like Mastodon's own remote search, you need the exact handle.
          </p>
        </div>
        <form onSubmit={handleSearch} className="flex gap-2">
          <input
            value={handle}
            onChange={e => setHandle(e.target.value)}
            placeholder="user@instance.social"
            className="input flex-1 text-sm"
          />
          <button type="submit" disabled={resolve.isPending || !handle.trim()} className="btn-secondary text-sm flex items-center gap-1.5">
            <Search size={14} /> {resolve.isPending ? 'Searching…' : 'Search'}
          </button>
        </form>
        {searchError && <p className="text-sm text-red-500">{searchError}</p>}

        {preview && (
          <div className="flex items-center gap-3 p-3 rounded-xl border border-agora-100 dark:border-agora-700">
            <div className="w-12 h-12 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
              {preview.icon_url
                ? <img src={preview.icon_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center font-bold text-agora-500">
                    {(preview.name || preview.preferred_username || '?')[0]}
                  </span>}
            </div>
            <div className="flex-1 min-w-0">
              <p className="font-medium text-sm truncate">{preview.name || preview.preferred_username}</p>
              <p className="text-xs text-agora-400 truncate">@{preview.preferred_username}@{preview.instance}</p>
              {preview.summary && <p className="text-xs text-agora-500 mt-1 line-clamp-2">{preview.summary}</p>}
            </div>
            <button
              onClick={() => follow.mutate(preview.actor_url)}
              disabled={follow.isPending || alreadyFollowing}
              className="btn-primary text-xs flex items-center gap-1 flex-shrink-0">
              <UserPlus size={13} /> {alreadyFollowing ? 'Following' : follow.isPending ? 'Following…' : 'Follow'}
            </button>
          </div>
        )}
      </div>

      <div className="card p-5 space-y-3">
        <h2 className="font-semibold text-sm">Your follows</h2>
        {following.length === 0 && (
          <p className="text-sm text-agora-400 italic py-3 text-center border border-dashed border-agora-200 dark:border-agora-700 rounded-lg">
            You're not following anyone on the fediverse yet.
          </p>
        )}
        <div className="space-y-2">
          {following.map(f => (
            <div key={f.id} className="flex items-center gap-3 py-2">
              <div className="w-9 h-9 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                {f.avatar_url
                  ? <img src={f.avatar_url} alt="" className="w-full h-full object-cover" />
                  : <span className="w-full h-full flex items-center justify-center text-sm font-bold text-agora-500">
                      {(f.display_name || f.username || '?')[0]}
                    </span>}
              </div>
              <div className="flex-1 min-w-0">
                <p className="font-medium text-sm truncate">{f.display_name || f.username || f.actor_url}</p>
                {f.username && <p className="text-xs text-agora-400 truncate">@{f.username}</p>}
              </div>
              {!f.accepted && (
                <span className="flex items-center gap-1 text-xs text-agora-400 flex-shrink-0">
                  <Clock size={12} /> Requested
                </span>
              )}
              <button
                onClick={() => unfollow.mutate(f.id)}
                disabled={unfollow.isPending}
                className="btn-secondary text-xs flex items-center gap-1 flex-shrink-0">
                <UserMinus size={13} /> Unfollow
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
