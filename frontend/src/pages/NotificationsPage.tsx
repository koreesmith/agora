import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { notificationsApi, friendsApi } from '../api'
import { formatDistanceToNow } from 'date-fns'
import { Bell, Heart, MessageCircle, UserPlus, UserCheck, UserX, Repeat2 } from 'lucide-react'

const typeIcon: Record<string, React.ReactNode> = {
  friend_request:  <UserPlus size={16} className="text-blue-500" />,
  friend_accepted: <UserCheck size={16} className="text-green-500" />,
  post_like:       <Heart size={16} className="text-red-500" />,
  post_comment:    <MessageCircle size={16} className="text-agora-500" />,
  post_repost:     <Repeat2 size={16} className="text-green-500" />,
}

const notifText: Record<string, string> = {
  friend_request:  'sent you a friend request',
  friend_accepted: 'accepted your friend request',
  post_like:       'liked your post',
  post_comment:    'commented on your post',
  post_repost:     'reposted your post',
}

export default function NotificationsPage() {
  const qc = useQueryClient()
  // Track per-notification local state so UI updates instantly on action
  const [localState, setLocalState] = useState<Record<string, 'accepted' | 'declined'>>({})

  const { data, isLoading } = useQuery({
    queryKey: ['notifications'],
    queryFn: () => notificationsApi.list().then(r => r.data),
  })

  const markAll = useMutation({
    mutationFn: () => notificationsApi.markAllRead(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notifications'] })
      qc.invalidateQueries({ queryKey: ['unread-count'] })
    },
  })

  const accept = useMutation({
    mutationFn: (actorId: string) => friendsApi.acceptRequest(actorId),
    onSuccess: (_, actorId) => {
      qc.invalidateQueries({ queryKey: ['friends'] })
      qc.invalidateQueries({ queryKey: ['requests'] })
      qc.invalidateQueries({ queryKey: ['unread-count'] })
    },
  })

  const decline = useMutation({
    mutationFn: (actorId: string) => friendsApi.declineRequest(actorId),
  })

  const handleAccept = (notifId: string, actorId: string) => {
    setLocalState(s => ({ ...s, [notifId]: 'accepted' }))
    accept.mutate(actorId)
  }

  const handleDecline = (notifId: string, actorId: string) => {
    setLocalState(s => ({ ...s, [notifId]: 'declined' }))
    decline.mutate(actorId)
  }

  const notifs = data?.notifications || []
  const hasUnread = notifs.some((n: any) => !n.read)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold">Notifications</h1>
        {hasUnread && (
          <button onClick={() => markAll.mutate()} className="btn-ghost text-sm">
            Mark all read
          </button>
        )}
      </div>

      {isLoading && <div className="text-center py-8 text-agora-400">Loading…</div>}

      {notifs.length === 0 && !isLoading && (
        <div className="card p-12 text-center text-agora-400">
          <Bell size={32} className="mx-auto mb-2" />
          <p>No notifications yet.</p>
        </div>
      )}

      <div className="space-y-1">
        {notifs.map((n: any) => (
          <div key={n.id}
            className={`card p-3 space-y-2 ${!n.read ? 'border-l-2 border-l-agora-600' : ''}`}>

            <div className="flex items-start gap-3">
              <Link to={`/profile/${n.actor_username}`}
                className="w-9 h-9 rounded-full bg-agora-100 dark:bg-agora-700 flex items-center justify-center flex-shrink-0 overflow-hidden">
                {n.actor_avatar_url
                  ? <img src={n.actor_avatar_url} alt="" className="w-full h-full object-cover" />
                  : (typeIcon[n.type] || <Bell size={16} />)
                }
              </Link>
              <div className="flex-1 min-w-0">
                <p className="text-sm">
                  {n.actor_username && (
                    <Link to={`/profile/${n.actor_username}`} className="font-semibold hover:underline">
                      {n.actor_display_name || n.actor_username}
                    </Link>
                  )}{' '}
                  {notifText[n.type] || ''}
                </p>
                <p className="text-xs text-agora-400 mt-0.5">
                  {formatDistanceToNow(new Date(n.created_at), { addSuffix: true })}
                </p>
              </div>
            </div>

            {/* Friend request actions — only for friend_request type */}
            {n.type === 'friend_request' && n.actor_id && (() => {
              const state = localState[n.id]
              if (state === 'accepted') return (
                <div className="pl-12 flex items-center gap-1.5 text-xs text-green-600 font-medium">
                  <UserCheck size={13} /> You are now friends
                </div>
              )
              if (state === 'declined') return (
                <div className="pl-12 text-xs text-agora-400">
                  Request declined
                </div>
              )
              return (
                <div className="pl-12 flex gap-2">
                  <button
                    onClick={() => handleAccept(n.id, n.actor_id)}
                    disabled={accept.isPending}
                    className="btn-primary text-xs py-1 px-3 flex items-center gap-1">
                    <UserCheck size={13} /> Accept
                  </button>
                  <button
                    onClick={() => handleDecline(n.id, n.actor_id)}
                    disabled={decline.isPending}
                    className="btn-secondary text-xs py-1 px-3 flex items-center gap-1">
                    <UserX size={13} /> Decline
                  </button>
                </div>
              )
            })()}
          </div>
        ))}
      </div>
    </div>
  )
}
