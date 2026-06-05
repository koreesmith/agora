import type { MentionUser, MentionGroup, MentionPage } from './useMentions'
import { handle } from '../../utils/handle'

interface Props {
  users: MentionUser[]
  groups: MentionGroup[]
  pages: MentionPage[]
  onSelect: (tag: string) => void
}

function Avatar({ url, name, rounded = 'full' }: { url: string, name: string, rounded?: 'full' | 'lg' }) {
  const shape = rounded === 'full' ? 'rounded-full' : 'rounded-lg'
  return (
    <div className={`w-7 h-7 ${shape} bg-agora-200 dark:bg-agora-600 overflow-hidden flex-shrink-0`}>
      {url
        ? <img src={url} alt="" className="w-full h-full object-cover" />
        : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">
            {name[0]?.toUpperCase()}
          </span>}
    </div>
  )
}

export default function MentionDropdown({ users, groups, pages, onSelect }: Props) {
  const total = users.length + groups.length + pages.length
  if (total === 0) return null

  return (
    <div className="absolute left-0 top-full mt-1 w-72 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-600 rounded-lg shadow-lg z-50 overflow-hidden">
      {/* Users */}
      {users.map(u => (
        <button key={u.id} onMouseDown={e => { e.preventDefault(); onSelect('@' + u.username) }}
          className="w-full flex items-center gap-2.5 px-3 py-2 hover:bg-agora-50 dark:hover:bg-agora-700 text-left">
          <Avatar url={u.avatar_url} name={u.display_name || u.username} />
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium truncate">{u.display_name || u.username}</p>
            <p className="text-xs text-agora-400 truncate">{handle(u.username, u.is_remote, u.remote_instance)}</p>
          </div>
          {u.is_friend && <span className="text-xs text-agora-400 flex-shrink-0">Friend</span>}
        </button>
      ))}

      {/* Groups */}
      {groups.length > 0 && (
        <>
          {users.length > 0 && <div className="border-t border-agora-100 dark:border-agora-700 mx-2" />}
          {groups.map(g => (
            <button key={g.slug} onMouseDown={e => { e.preventDefault(); onSelect('+' + g.slug) }}
              className="w-full flex items-center gap-2.5 px-3 py-2 hover:bg-agora-50 dark:hover:bg-agora-700 text-left">
              <Avatar url={g.avatar_url} name={g.name} rounded="lg" />
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium truncate">{g.name}</p>
                <p className="text-xs text-agora-400">Group · +{g.slug}</p>
              </div>
            </button>
          ))}
        </>
      )}

      {/* Pages */}
      {pages.length > 0 && (
        <>
          {(users.length > 0 || groups.length > 0) && <div className="border-t border-agora-100 dark:border-agora-700 mx-2" />}
          {pages.map(p => (
            <button key={p.slug} onMouseDown={e => { e.preventDefault(); onSelect('+' + p.slug) }}
              className="w-full flex items-center gap-2.5 px-3 py-2 hover:bg-agora-50 dark:hover:bg-agora-700 text-left">
              <Avatar url={p.avatar_url} name={p.display_name} rounded="lg" />
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium truncate">{p.display_name}</p>
                <p className="text-xs text-agora-400">Page · +{p.slug}</p>
              </div>
            </button>
          ))}
        </>
      )}
    </div>
  )
}
