import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Home, Rss, Settings2 } from 'lucide-react'
import { customFeedsApi } from '../../api'

interface Props {
  activeFeedId: string | null
  onChange: (id: string | null) => void
}

export default function FeedSwitcher({ activeFeedId, onChange }: Props) {
  const { data } = useQuery({
    queryKey: ['custom-feeds'],
    queryFn: () => customFeedsApi.list().then(r => r.data),
  })
  const feeds: any[] = data ?? []

  return (
    <div className="flex items-center gap-2 overflow-x-auto pb-1 scrollbar-none">
      <button
        onClick={() => onChange(null)}
        className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-medium whitespace-nowrap transition-colors flex-shrink-0 ${
          activeFeedId === null
            ? 'bg-agora-700 text-white'
            : 'bg-agora-100 dark:bg-agora-800 text-agora-700 dark:text-agora-300 hover:bg-agora-200 dark:hover:bg-agora-700'
        }`}
      >
        <Home size={13} />
        Home
      </button>

      {feeds.map(feed => (
        <button
          key={feed.id}
          onClick={() => onChange(feed.id)}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-medium whitespace-nowrap transition-colors flex-shrink-0 ${
            activeFeedId === feed.id
              ? 'bg-agora-700 text-white'
              : 'bg-agora-100 dark:bg-agora-800 text-agora-700 dark:text-agora-300 hover:bg-agora-200 dark:hover:bg-agora-700'
          }`}
        >
          <Rss size={13} />
          {feed.name}
        </button>
      ))}

      <Link
        to="/my-feeds"
        className="flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-medium whitespace-nowrap text-agora-500 dark:text-agora-400 hover:bg-agora-100 dark:hover:bg-agora-800 transition-colors flex-shrink-0 ml-auto"
      >
        <Settings2 size={13} />
        Manage Feeds
      </Link>
    </div>
  )
}
