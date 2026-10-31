import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { CornerLeftUp, ExternalLink } from 'lucide-react'
import { federationApi } from '../../api'
import { handle } from '../../utils/handle'
import { renderContent, renderName } from './CommentsSection'

// AGORA-419: the conversation above a fediverse post that was a reply.
//
// A boosted reply arrives here on its own, so without this the post page showed
// it with nothing it was answering. The backend walks its inReplyTo chain (and
// stores each ancestor, so it is fetched once); this lists them oldest first,
// each opening its own post page, directly above the post itself.
export default function ThreadContext({ post }: { post: any }) {
  const eligible = !!post?.is_remote && post.remote_instance !== 'bsky.app'
  const { data } = useQuery({
    queryKey: ['thread-context', post?.id],
    queryFn: () => federationApi.threadContext(post.id).then(r => r.data),
    enabled: eligible,
    staleTime: 5 * 60 * 1000,
    retry: false,
  })

  const ancestors: any[] = data?.ancestors || []
  if (!eligible || (ancestors.length === 0 && !data?.more)) return null

  // When the chain couldn't be followed all the way up, the conversation's own
  // server still has it.
  const earlierURL = ancestors[0]?.url || data?.url

  return (
    <div className="space-y-1.5">
      <p className="flex items-center gap-1.5 px-1 text-xs font-medium text-agora-400">
        <CornerLeftUp size={12} /> In reply to
      </p>
      {data?.more && earlierURL && (
        <a href={earlierURL} target="_blank" rel="noreferrer noopener"
          className="flex items-center gap-1.5 px-1 text-xs text-agora-500 hover:text-agora-700 dark:hover:text-agora-300 hover:underline">
          <ExternalLink size={11} /> Earlier posts in this conversation are on its original server
        </a>
      )}
      <div className="card divide-y divide-agora-100 dark:divide-agora-700 overflow-hidden">
        {ancestors.map(a => (
          <Link key={a.id} to={`/post/${a.id}`}
            className="flex gap-3 p-3 hover:bg-agora-50 dark:hover:bg-agora-700/40 transition-colors">
            <span className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
              {a.avatar_url
                ? <img src={a.avatar_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">
                    {(a.display_name || a.username)[0]?.toUpperCase()}
                  </span>}
            </span>
            <span className="flex-1 min-w-0">
              <span className="flex items-baseline gap-1.5 min-w-0">
                <span className="text-sm font-medium truncate">
                  {a.display_name ? renderName(a.display_name, a.author_emojis) : a.username}
                </span>
                <span className="text-xs text-agora-400 truncate">
                  {handle(a.username, a.is_remote, a.remote_instance)}
                </span>
                {a.published_at && (
                  <span className="text-xs text-agora-400 flex-shrink-0">
                    · {formatDistanceToNow(new Date(a.published_at), { addSuffix: true })}
                  </span>
                )}
              </span>
              {a.content_warning
                ? <span className="block text-sm text-agora-500 italic mt-0.5">CW: {a.content_warning}</span>
                : <span className="block text-sm text-agora-700 dark:text-agora-200 whitespace-pre-wrap break-words line-clamp-6 mt-0.5">
                    {renderContent(a.content)}
                  </span>}
            </span>
          </Link>
        ))}
      </div>
    </div>
  )
}
