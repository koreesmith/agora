import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { notificationsApi, friendsApi } from '../api'
import { formatDistanceToNow } from 'date-fns'
import { Bell, Heart, MessageCircle, UserPlus, UserCheck, UserX, Repeat2, Users, CheckCircle, XCircle } from 'lucide-react'

const typeIcon: Record<string, React.ReactNode> = {
  friend_request:        <UserPlus size={16} className="text-blue-500" />,
  friend_accepted:       <UserCheck size={16} className="text-green-500" />,
  post_like:             <Heart size={16} className="text-red-500" />,
  post_comment:          <MessageCircle size={16} className="text-agora-500" />,
  post_repost:           <Repeat2 size={16} className="text-green-500" />,
  post_mention:          <MessageCircle size={16} className="text-blue-500" />,
  comment_reply:         <MessageCircle size={16} className="text-agora-600" />,
  group_join_request:    <Users size={16} className="text-agora-500" />,
  group_join_approved:   <CheckCircle size={16} className="text-green-500" />,
  group_join_rejected:   <XCircle size={16} className="text-red-400" />,
  group_invite_accepted: <UserCheck size={16} className="text-green-500" />,
}

const notifText: Record<string, string> = {
  friend_request:        'sent you a friend request',
  friend_accepted:       'accepted your friend request',
  post_like:             'liked your post',
  post_comment:          'commented on your post',
  post_repost:           'shared your post',
  post_mention:          'mentioned you in a post',
  comment_reply:         'replied to your comment',
  group_join_request:    'wants to join your group',
  group_join_approved:   'approved your request to join a group',
  group_join_rejected:   'declined your request to join a group',
  group_invite_accepted: 'added you to a group',
}

function notifTarget(n: any): string | null {
  switch (n.type) {
    case 'friend_request':
    case 'friend_accepted':
      return n.actor_username ? `/profile/${n.actor_username}` : null
    case 'post_like':
    case 'post_repost':
    case 'post_comment':
    case 'post_mention':
    case 'comment_reply':
      return n.post_id ? `/post/${n.post_id}` : null
    case 'group_join_request':
    case 'group_join_approved':
    case 'group_join_rejected':
    case 'group_invite_accepted':
      return n.data ? `/groups/${n.data}` : '/groups'
    default:
      return null
  }
}

export default function NotificationsPage() {
  const qc = useQueryClient()
  // localState: track accepted/declined per notif ID so it persists through refetches
  const [localState, setLocalState] = useState<Record<string, 'accepted' | 'declined'>>({})

  const { data, isLoading } = useQuery({
    queryKey: ['notifications'],
    queryFn: () => notificationsApi.list().then(r => r.data),
  })

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['notifications'] })
    qc.invalidateQueries({ queryKey: ['unread-count'] })
  }

  const markAll  = useMutation({ mutationFn: () => notificationsApi.markAllRead(), onSuccess: invalidate })
  const markRead = useMutation({ mutationFn: (id: string) => notificationsApi.markRead(id), onSuccess: invalidate })
  const accept   = useMutation({ mutationFn: (id: string) => friendsApi.acceptRequest(id), onSuccess: () => { qc.invalidateQueries({ queryKey: ['friends'] }); invalidate() } })
  const decline  = useMutation({ mutationFn: (id: string) => friendsApi.declineRequest(id), onSuccess: invalidate })

  const notifs   = data?.notifications || []
  const hasUnread = notifs.some((n: any) => !n.read)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold">Notifications</h1>
        {hasUnread && (
          <button onClick={() => markAll.mutate()} className="btn-ghost text-sm">Mark all read</button>
        )}
      </div>

      {isLoading && <div className="text-center py-8 text-agora-400">Loading…</div>}
      {notifs.length === 0 && !isLoading && (
        <div className="card p-12 text-center text-agora-400">
          <Bell size={32} className="mx-auto mb-2" /><p>No notifications yet.</p>
        </div>
      )}

      <div className="space-y-1">
        {notifs.map((n: any) => {
          const target      = notifTarget(n)
          const isFriendReq = n.type === 'friend_request'
          const localAction = localState[n.id]

          return (
            <div key={n.id} className={`card p-3 space-y-2 ${!n.read ? 'border-l-2 border-l-agora-600' : ''}`}>

              {/* Main row — entire thing is a link except friend_request which has buttons */}
              {target && !isFriendReq ? (
                <Link
                  to={target}
                  onClick={() => { if (!n.read) markRead.mutate(n.id) }}
                  className="flex items-start gap-3 hover:opacity-80 transition-opacity"
                >
                  <NotifAvatar n={n} />
                  <NotifBody n={n} />
                  {!n.read && <div className="w-2 h-2 rounded-full bg-agora-600 flex-shrink-0 mt-1.5" />}
                </Link>
              ) : (
                <div className="flex items-start gap-3">
                  <NotifAvatar n={n} />
                  <NotifBody n={n} />
                  {!n.read && <div className="w-2 h-2 rounded-full bg-agora-600 flex-shrink-0 mt-1.5" />}
                </div>
              )}

              {/* Friend request actions */}
              {isFriendReq && n.actor_id && (
                <div className="pl-12 flex gap-2 flex-wrap">
                  {(localAction === 'accepted' || n.friend_status === 'accepted') ? (
                    <span className="text-xs text-green-600 font-medium flex items-center gap-1">
                      <UserCheck size={13} /> You are now friends
                    </span>
                  ) : (localAction === 'declined' || n.friend_status === 'declined') ? (
                    <span className="text-xs text-agora-400">Request declined</span>
                  ) : (
                    <>
                      <button
                        onClick={() => { setLocalState(s => ({ ...s, [n.id]: 'accepted' })); accept.mutate(n.actor_id); markRead.mutate(n.id) }}
                        className="btn-primary text-xs py-1 px-3 flex items-center gap-1"
                      >
                        <UserCheck size={13} /> Accept
                      </button>
                      <button
                        onClick={() => { setLocalState(s => ({ ...s, [n.id]: 'declined' })); decline.mutate(n.actor_id); markRead.mutate(n.id) }}
                        className="btn-secondary text-xs py-1 px-3 flex items-center gap-1"
                      >
                        <UserX size={13} /> Decline
                      </button>
                      <Link
                        to={`/profile/${n.actor_username}`}
                        onClick={() => { if (!n.read) markRead.mutate(n.id) }}
                        className="btn-ghost text-xs py-1 px-2 text-agora-500"
                      >
                        View profile
                      </Link>
                    </>
                  )}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function NotifAvatar({ n }: { n: any }) {
  return (
    <div className="w-9 h-9 rounded-full bg-agora-100 dark:bg-agora-700 flex items-center justify-center flex-shrink-0 overflow-hidden">
      {n.actor_avatar_url
        ? <img src={n.actor_avatar_url} alt="" className="w-full h-full object-cover" />
        : (typeIcon[n.type] || <Bell size={16} />)
      }
    </div>
  )
}

function NotifBody({ n }: { n: any }) {
  return (
    <div className="flex-1 min-w-0">
      <p className="text-sm">
        <span className="font-semibold">{n.actor_display_name || n.actor_username}</span>
        {' '}{notifText[n.type] || ''}
      </p>
      <p className="text-xs text-agora-400 mt-0.5">
        {formatDistanceToNow(new Date(n.created_at), { addSuffix: true })}
      </p>
    </div>
  )
}
