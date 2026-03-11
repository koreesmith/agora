import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { usersApi, feedApi, friendsApi } from '../api'
import { useAuthStore } from '../store/auth'
import PostCard from '../components/feed/PostCard'
import { UserPlus, UserCheck, UserX, Clock, Lock } from 'lucide-react'

export default function ProfilePage() {
  const { username } = useParams<{ username: string }>()
  const { user: me } = useAuthStore()
  const qc = useQueryClient()

  const { data: profile, isLoading } = useQuery({
    queryKey: ['profile', username],
    queryFn: () => usersApi.getProfile(username!).then(r => r.data),
  })

  const { data: postsData } = useQuery({
    queryKey: ['user-posts', username],
    queryFn: () => feedApi.getUserPosts(username!).then(r => r.data),
    enabled: !!profile && !profile.profile_private,
  })

  const inv = () => {
    qc.invalidateQueries({ queryKey: ['profile', username] })
    qc.invalidateQueries({ queryKey: ['friends'] })
    qc.invalidateQueries({ queryKey: ['requests'] })
    qc.invalidateQueries({ queryKey: ['notifications'] })
  }

  const sendReq = useMutation({ mutationFn: () => friendsApi.sendRequest(profile.id), onSuccess: inv })
  const accept  = useMutation({ mutationFn: () => friendsApi.acceptRequest(profile.id), onSuccess: inv })
  const decline = useMutation({ mutationFn: () => friendsApi.declineRequest(profile.id), onSuccess: inv })
  const unfriend= useMutation({ mutationFn: () => friendsApi.unfriend(profile.id), onSuccess: inv })

  if (isLoading) return <div className="text-center py-12 text-agora-400">Loading…</div>
  if (!profile)  return <div className="text-center py-12 text-agora-400">User not found.</div>

  const isSelf = me?.id === profile.id || me?.username === username
  const status = profile.friend_status

  return (
    <div className="space-y-4">
      {/* Cover + avatar */}
      <div className="card overflow-hidden">
        <div className="h-32 bg-gradient-to-r from-agora-300 to-agora-500 dark:from-agora-700 dark:to-agora-900">
          {profile.cover_url && <img src={profile.cover_url} alt="" className="w-full h-full object-cover" />}
        </div>
        <div className="px-4 pb-4">
          <div className="flex items-end justify-between -mt-10 mb-3">
            <div className="w-20 h-20 rounded-full border-4 border-white dark:border-agora-800 bg-agora-200 dark:bg-agora-700 overflow-hidden">
              {profile.avatar_url
                ? <img src={profile.avatar_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center text-2xl font-bold text-agora-600">{profile.display_name?.[0]?.toUpperCase()}</span>
              }
            </div>
            <div className="flex gap-2 mt-10">
              {!isSelf && !status && (
                <button onClick={() => sendReq.mutate()} className="btn-primary text-sm flex items-center gap-1">
                  <UserPlus size={16}/> Add friend
                </button>
              )}
              {!isSelf && status === 'pending' && (
                <button disabled className="btn-secondary text-sm flex items-center gap-1"><Clock size={16}/> Pending</button>
              )}
              {!isSelf && status === 'pending_incoming' && (
                <div className="flex gap-2">
                  <button onClick={() => accept.mutate()} className="btn-primary text-sm flex items-center gap-1">
                    <UserCheck size={16}/> Accept request
                  </button>
                  <button onClick={() => { if(confirm('Decline request?')) decline.mutate() }} className="btn-secondary text-sm flex items-center gap-1">
                    <UserX size={16}/> Decline
                  </button>
                </div>
              )}
              {!isSelf && status === 'accepted' && (
                <button onClick={() => { if(confirm('Unfriend?')) unfriend.mutate() }} className="btn-secondary text-sm flex items-center gap-1">
                  <UserCheck size={16}/> Friends
                </button>
              )}
            </div>
          </div>
          <h1 className="text-xl font-bold">{profile.display_name}</h1>
          <p className="text-agora-500 text-sm">@{profile.username}</p>
          {profile.bio && <p className="text-sm mt-2 text-agora-700 dark:text-agora-300">{profile.bio}</p>}
          <div className="flex items-center gap-4 mt-3 text-sm text-agora-500">
            <span><strong className="text-agora-800 dark:text-agora-200">{profile.friend_count || 0}</strong> friends</span>
            {profile.location && <span>{profile.location}</span>}
            {profile.website && <a href={profile.website} className="text-agora-600 hover:underline" target="_blank" rel="noreferrer">{profile.website}</a>}
          </div>
        </div>
      </div>

      {profile.profile_private && !isSelf && status !== 'accepted' ? (
        <div className="card p-8 text-center text-agora-400">
          <Lock size={32} className="mx-auto mb-2" />
          <p className="font-medium">This profile is private</p>
          <p className="text-sm mt-1">Add {profile.display_name} as a friend to see their posts.</p>
        </div>
      ) : (
        <div className="space-y-4">
          {(postsData?.posts || []).map((p: any) => <PostCard key={p.id} post={p} invalidateKey={`user-posts-${username}`} />)}
          {(postsData?.posts || []).length === 0 && <div className="card p-6 text-center text-agora-400 text-sm">No posts yet.</div>}
        </div>
      )}
    </div>
  )
}
