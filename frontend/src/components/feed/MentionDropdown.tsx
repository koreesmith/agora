import type { MentionUser } from './useMentions'
import { handle } from '../../utils/handle'

interface Props {
  users: MentionUser[]
  onSelect: (username: string) => void
}

export default function MentionDropdown({ users, onSelect }: Props) {
  if (users.length === 0) return null
  return (
    <div className="absolute left-0 top-full mt-1 w-72 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-lg shadow-lg z-50 overflow-hidden">
      {users.map(u => (
        <button
          key={u.id}
          onMouseDown={e => { e.preventDefault(); onSelect(u.username) }}
          className="w-full flex items-center gap-2.5 px-3 py-2 hover:bg-agora-50 dark:hover:bg-agora-700 text-left"
        >
          <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-600 overflow-hidden flex-shrink-0">
            {u.avatar_url
              ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
              : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">
                  {(u.display_name || u.username)[0].toUpperCase()}
                </span>
            }
          </div>
          <div className="min-w-0">
            <p className="text-sm font-medium truncate">{u.display_name || u.username}</p>
            <p className="text-xs text-agora-400 truncate">{handle(u.username, u.is_remote, u.remote_instance)}</p>
          </div>
          {u.is_friend && <span className="ml-auto text-xs text-agora-400 flex-shrink-0">Friend</span>}
        </button>
      ))}
    </div>
  )
}
