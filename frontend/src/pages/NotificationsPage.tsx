import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { notificationsApi, friendsApi } from '../api'
import { formatDistanceToNow } from 'date-fns'
import { Bell, Heart, MessageCircle, UserPlus, UserCheck, Repeat2 } from 'lucide-react'

const icons: Record<string, React.ReactNode> = {
  friend_request:  <UserPlus size={16} className="text-blue-500" />,
  friend_accepted: <UserCheck size={16} className="text-green-500" />,
  post_like:       <Heart size={16} className="text-red-500" />,
  post_comment:    <MessageCircle size={16} className="text-agora-500" />,
  post_repost:     <Repeat2 size={16} className="text-green-500" />,
}

export default function NotificationsPage() {
  const qc = useQueryClient()

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
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notifications'] })
      qc.invalidateQueries({ queryKey: ['friends'] })
      qc.invalidateQueries({ queryKey: ['requests'] })
    },
  })

  const decline = useMutation({
    mutationFn: (actorId: string) => friendsApi.declineRequest(actorId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notifications'] })
      qc.invalidateQueries({ queryKey: ['requests'] })
    },
  })

  const notifs = data?.notifications || []
  const hasUnread = notifs.some((n: any) => !n.read)

  const notifText = (n: any) => {
    switch (n.type) {
      case 'friend_request':  return 'sent you a friend request'
      case 'friend_accepted': return 'accepted your friend request'
      case 'post_like':       return 'liked your post'
      case 'post_comment':    return 'commented on your post'
      case 'post_repost':     return 'reposted your post'
      default:                return ''
    }
  }

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
                  : icons[n.type] || <Bell size={16} />
                }
              </Link>
              <div className="flex-1 min-w-0">
                <p className="text-sm">
                  {n.actor_username && (
                    <Link to={`/profile/${n.actor_username}`} className="font-semibold hover:underline">
                      {n.actor_display_name || n.actor_username}
                    </Link>
                  )}{' '}
                  {notifText(n)}
                </p>
                <p className="text-xs text-agora-400 mt-0.5">
                  {formatDistanceToNow(new Date(n.created_at), { addSuffix: true })}
                </p>
              </div>
            </div>

            {/* Inline accept/decline for pending friend requests */}
            {n.type === 'friend_request' && n.actor_id && (
              <FriendRequestActions
                actorId={n.actor_id}
                username={n.actor_username}
                onAccept={() => accept.mutate(n.actor_id)}
                onDecline={() => decline.mutate(n.actor_id)}
                isPending={accept.isPending || decline.isPending}
              />
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

function FriendRequestActions({ actorId, username, onAccept, onDecline, isPending }: {
  actorId: string
  username: string
  onAccept: () => void
  onDecline: () => void
  isPending: boolean
}) {
  // Check current friendship status
  const { data } = useQuery({
    queryKey: ['profile', username],
    queryFn: () => fetch(`/api/users/${username}`, {
      headers: { Authorization: `Bearer ${localStorage.getItem('agora_token')}` }
    }).then(r => r.json()),
    enabled: !!username,
  })

  const status = data?.friend_status

  if (status === 'accepted') {
    return (
      <div className="flex items-center gap-2 pl-12">
        <span className="text-xs text-green-600 font-medium flex items-center gap-1">
          <UserCheck size={13} /> Friends
        </span>
      </div>
    )
  }

  if (status !== 'pending_incoming') return null

  return (
    <div className="flex gap-2 pl-12">
      <button onClick={onAccept} disabled={isPending}
        className="btn-primary text-xs py-1 px-3 flex items-center gap-1">
        <UserCheck size={13} /> Accept
      </button>
      <button onClick={onDecline} disabled={isPending}
        className="btn-secondary text-xs py-1 px-3">
        Decline
      </button>
    </div>
  )
}
