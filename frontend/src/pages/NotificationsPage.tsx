import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
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

// Determine where a notification should navigate to
function notifTarget(n: any): string | null {
  switch (n.type) {
    case 'friend_request':
    case 'friend_accepted':
      return n.actor_username ? `/profile/${n.actor_username}` : null
    case 'post_like':
    case 'post_repost':
      return n.post_id ? `/post/${n.post_id}` : null
    case 'post_comment':
      return n.post_id ? `/post/${n.post_id}#comment-${n.post_id}` : null
    default:
      return null
  }
}

export default function NotificationsPage() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [localState, setLocalState] = useState<Record<string, 'accepted' | 'declined'>>({})

  const { data, isLoading } = useQuery({
    queryKey: ['notifications'],
    queryFn: () => notificationsApi.list().then(r => r.data),
  })

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['notifications'] })
    qc.invalidateQueries({ queryKey: ['unread-count'] })
  }

  const markAll = useMutation({
    mutationFn: () => notificationsApi.markAllRead(),
    onSuccess: invalidate,
  })

  const markRead = useMutation({
    mutationFn: (id: string) => notificationsApi.markRead(id),
    onSuccess: invalidate,
  })

  const accept = useMutation({
    mutationFn: (actorId: string) => friendsApi.acceptRequest(actorId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['friends'] })
      qc.invalidateQueries({ queryKey: ['requests'] })
      invalidate()
    },
  })

  const decline = useMutation({
    mutationFn: (actorId: string) => friendsApi.declineRequest(actorId),
    onSuccess: invalidate,
  })

  const handleAccept = (notifId: string, actorId: string) => {
    setLocalState(s => ({ ...s, [notifId]: 'accepted' }))
    accept.mutate(actorId)
    markRead.mutate(notifId)
  }

  const handleDecline = (notifId: string, actorId: string) => {
    setLocalState(s => ({ ...s, [notifId]: 'declined' }))
    decline.mutate(actorId)
    markRead.mutate(notifId)
  }

  const handleClick = (n: any) => {
    // Don't navigate on friend_request — let the buttons handle it
    if (n.type === 'friend_request') return
    const target = notifTarget(n)
    if (!target) return
    if (!n.read) markRead.mutate(n.id)
    navigate(target)
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
        {notifs.map((n: any) => {
          const isUnread = !n.read
          const isFriendReq = n.type === 'friend_request'
          const target = notifTarget(n)
          const isClickable = !isFriendReq && !!target
          const localAction = localState[n.id]

          return (
            <div
              key={n.id}
              onClick={() => handleClick(n)}
              className={`card p-3 space-y-2 transition-colors
                ${isUnread ? 'border-l-2 border-l-agora-600 bg-agora-50/50 dark:bg-agora-800/50' : ''}
                ${isClickable ? 'cursor-pointer hover:bg-agora-50 dark:hover:bg-agora-800' : ''}
              `}
            >
              <div className="flex items-start gap-3">
                {/* Avatar — always links to actor profile */}
                <Link
                  to={n.actor_username ? `/profile/${n.actor_username}` : '#'}
                  onClick={e => e.stopPropagation()}
                  className="w-9 h-9 rounded-full bg-agora-100 dark:bg-agora-700 flex items-center justify-center flex-shrink-0 overflow-hidden"
                >
                  {n.actor_avatar_url
                    ? <img src={n.actor_avatar_url} alt="" className="w-full h-full object-cover" />
                    : (typeIcon[n.type] || <Bell size={16} />)
                  }
                </Link>

                <div className="flex-1 min-w-0">
                  <p className="text-sm">
                    {n.actor_username && (
                      <Link
                        to={`/profile/${n.actor_username}`}
                        onClick={e => e.stopPropagation()}
                        className="font-semibold hover:underline"
                      >
                        {n.actor_display_name || n.actor_username}
                      </Link>
                    )}{' '}
                    {notifText[n.type] || ''}
                  </p>
                  <p className="text-xs text-agora-400 mt-0.5">
                    {formatDistanceToNow(new Date(n.created_at), { addSuffix: true })}
                    {isClickable && isUnread && (
                      <span className="ml-2 text-agora-600 dark:text-agora-400">· tap to view</span>
                    )}
                  </p>
                </div>

                {isUnread && (
                  <div className="w-2 h-2 rounded-full bg-agora-600 flex-shrink-0 mt-1.5" />
                )}
              </div>

              {/* Friend request inline actions */}
              {isFriendReq && n.actor_id && (() => {
                if (localAction === 'accepted') return (
                  <div className="pl-12 flex items-center gap-1.5 text-xs text-green-600 font-medium">
                    <UserCheck size={13} /> You are now friends
                  </div>
                )
                if (localAction === 'declined') return (
                  <div className="pl-12 text-xs text-agora-400">Request declined</div>
                )
                return (
                  <div className="pl-12 flex gap-2">
                    <button
                      onClick={e => { e.stopPropagation(); handleAccept(n.id, n.actor_id) }}
                      disabled={accept.isPending}
                      className="btn-primary text-xs py-1 px-3 flex items-center gap-1"
                    >
                      <UserCheck size={13} /> Accept
                    </button>
                    <button
                      onClick={e => { e.stopPropagation(); handleDecline(n.id, n.actor_id) }}
                      disabled={decline.isPending}
                      className="btn-secondary text-xs py-1 px-3 flex items-center gap-1"
                    >
                      <UserX size={13} /> Decline
                    </button>
                    <Link
                      to={`/profile/${n.actor_username}`}
                      onClick={e => { e.stopPropagation(); if (!n.read) markRead.mutate(n.id) }}
                      className="btn-ghost text-xs py-1 px-2 text-agora-500"
                    >
                      View profile
                    </Link>
                  </div>
                )
              })()}
            </div>
          )
        })}
      </div>
    </div>
  )
}
