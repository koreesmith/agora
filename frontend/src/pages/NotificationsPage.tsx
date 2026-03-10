import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { notificationsApi } from '../api'
import { formatDistanceToNow } from 'date-fns'
import { Bell, Heart, MessageCircle, UserPlus, UserCheck, Repeat2 } from 'lucide-react'

const icons: Record<string, React.ReactNode> = {
  friend_request: <UserPlus size={16} className="text-blue-500" />,
  friend_accepted: <UserCheck size={16} className="text-green-500" />,
  post_like:    <Heart size={16} className="text-red-500" />,
  post_comment: <MessageCircle size={16} className="text-agora-500" />,
  post_repost:  <Repeat2 size={16} className="text-green-500" />,
}

export default function NotificationsPage() {
  const qc = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['notifications'],
    queryFn: () => notificationsApi.list().then(r => r.data),
  })

  const markAll = useMutation({
    mutationFn: () => notificationsApi.markAllRead(),
    onSuccess: () => { qc.invalidateQueries({ queryKey:['notifications'] }); qc.invalidateQueries({ queryKey:['unread-count'] }) },
  })

  const notifs = data?.notifications || []
  const hasUnread = notifs.some((n:any) => !n.read)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold">Notifications</h1>
        {hasUnread && <button onClick={() => markAll.mutate()} className="btn-ghost text-sm">Mark all read</button>}
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
          <div key={n.id} className={`card p-3 flex items-start gap-3 ${!n.read ? 'border-l-2 border-l-agora-600' : ''}`}>
            <div className="w-9 h-9 rounded-full bg-agora-100 dark:bg-agora-700 flex items-center justify-center flex-shrink-0">
              {icons[n.type] || <Bell size={16} />}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm">
                {n.actor_username && (
                  <Link to={`/profile/${n.actor_username}`} className="font-semibold hover:underline">
                    {n.actor_display_name || n.actor_username}
                  </Link>
                )}{' '}
                {n.type === 'friend_request' && 'sent you a friend request'}
                {n.type === 'friend_accepted' && 'accepted your friend request'}
                {n.type === 'post_like'    && 'liked your post'}
                {n.type === 'post_comment' && 'commented on your post'}
                {n.type === 'post_repost'  && 'reposted your post'}
              </p>
              <p className="text-xs text-agora-400 mt-0.5">
                {formatDistanceToNow(new Date(n.created_at), { addSuffix: true })}
              </p>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
