import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { feedApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import { formatDistanceToNow } from 'date-fns'
import { Trash2, Send } from 'lucide-react'
import { Link } from 'react-router-dom'

export default function CommentsSection({ postId, postAuthorId }: { postId: string, postAuthorId: string }) {
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [text, setText] = useState('')

  const { data } = useQuery({
    queryKey: ['comments', postId],
    queryFn: () => feedApi.getComments(postId).then(r => r.data),
  })

  const create = useMutation({
    mutationFn: () => feedApi.createComment(postId, { content: text }),
    onSuccess: () => {
      setText('')
      qc.invalidateQueries({ queryKey: ['comments', postId] })
      qc.invalidateQueries({ queryKey: ['feed'] })
    },
  })

  const del = useMutation({
    mutationFn: (commentId: string) => feedApi.deleteComment(postId, commentId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['comments', postId] }),
  })

  const comments = data?.comments || []

  return (
    <div className="mt-4 pt-4 border-t border-agora-100 dark:border-agora-700 space-y-3">
      {comments.map((c: any) => (
        <div key={c.id} className="flex gap-2">
          <Link to={`/profile/${c.username}`} className="flex-shrink-0">
            <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden">
              {c.avatar_url
                ? <img src={c.avatar_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">
                    {(c.display_name || c.username)[0].toUpperCase()}
                  </span>
              }
            </div>
          </Link>
          <div className="flex-1 min-w-0">
            <div className="bg-agora-50 dark:bg-agora-700/50 rounded-xl px-3 py-2">
              <Link to={`/profile/${c.username}`}
                className="text-xs font-semibold text-agora-800 dark:text-agora-200 hover:underline">
                {c.display_name || c.username}
              </Link>
              <p className="text-sm text-agora-700 dark:text-agora-300 mt-0.5 break-words">{c.content}</p>
              {c.image_url && <img src={c.image_url} alt="" className="mt-1 rounded-lg max-h-48 object-cover" />}
            </div>
            <div className="flex items-center gap-3 mt-1 px-1">
              <span className="text-xs text-agora-400">
                {formatDistanceToNow(new Date(c.created_at), { addSuffix: true })}
              </span>
              {(c.author_id === user?.id || user?.id === postAuthorId || user?.role === 'admin') && (
                <button onClick={() => del.mutate(c.id)}
                  className="text-xs text-agora-300 hover:text-red-500 transition-colors">
                  <Trash2 size={12} />
                </button>
              )}
            </div>
          </div>
        </div>
      ))}

      {/* New comment input */}
      <div className="flex gap-2 pt-1">
        <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {user?.avatar_url
            ? <img src={user.avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">
                {user?.username?.[0]?.toUpperCase()}
              </span>
          }
        </div>
        <div className="flex-1 flex gap-2">
          <input
            className="input flex-1 text-sm py-1.5"
            placeholder="Write a comment…"
            value={text}
            onChange={e => setText(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && !e.shiftKey && text.trim() && create.mutate()}
          />
          <button
            onClick={() => create.mutate()}
            disabled={!text.trim() || create.isPending}
            className="btn-primary px-3 py-1.5">
            <Send size={14} />
          </button>
        </div>
      </div>
    </div>
  )
}
