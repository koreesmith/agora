import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { pagesApi } from '../api'
import { Users, BookOpen, PlusCircle, Search } from 'lucide-react'

function PageCard({ page }: { page: any }) {
  return (
    <Link to={`/pages/${page.slug}`}
      className="card p-4 flex gap-3 hover:border-agora-300 dark:hover:border-agora-500 transition-colors">
      <div className="w-12 h-12 rounded-xl bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
        {page.avatar_url
          ? <img src={page.avatar_url} alt="" className="w-full h-full object-cover" />
          : <span className="w-full h-full flex items-center justify-center text-lg font-bold text-agora-500">
              {page.display_name[0]}
            </span>}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <p className="font-semibold text-sm truncate">{page.display_name}</p>
          {page.is_verified && <span className="text-blue-500 text-xs">✓</span>}
        </div>
        <p className="text-xs text-agora-400">@{page.slug}</p>
        {page.bio && <p className="text-xs text-agora-500 mt-1 line-clamp-2">{page.bio}</p>}
        <div className="flex items-center gap-3 mt-1.5 text-xs text-agora-400">
          <span className="flex items-center gap-1"><Users size={10} /> {page.subscriber_count}</span>
          <span className="flex items-center gap-1"><BookOpen size={10} /> {page.post_count}</span>
          {page.is_subscribed && <span className="text-agora-600 dark:text-agora-400 font-medium">Subscribed</span>}
          {page.is_owner && <span className="text-agora-600 dark:text-agora-400 font-medium">Owner</span>}
        </div>
      </div>
    </Link>
  )
}

export default function PagesPage() {
  const [q, setQ] = useState('')
  const [searchQuery, setSearchQuery] = useState('')

  const { data: mineData } = useQuery({
    queryKey: ['pages-mine'],
    queryFn: () => pagesApi.mine().then(r => r.data),
  })
  const myPages: any[] = mineData?.pages ?? []

  const { data: discoverData, isLoading } = useQuery({
    queryKey: ['pages-discover', searchQuery],
    queryFn: () => pagesApi.list(searchQuery ? { q: searchQuery } : undefined).then(r => r.data),
  })
  const pages: any[] = discoverData?.pages ?? []

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    setSearchQuery(q.trim())
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">Pages</h1>
          <p className="text-sm text-agora-400">Follow pages to see their posts in your feed</p>
        </div>
        <Link to="/pages/new" className="btn-primary text-sm flex items-center gap-1.5">
          <PlusCircle size={15} /> Create Page
        </Link>
      </div>

      {/* My pages */}
      {myPages.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-sm font-semibold text-agora-600 dark:text-agora-300 uppercase tracking-wide">Your pages</h2>
          <div className="grid gap-2 sm:grid-cols-2">
            {myPages.map((p: any) => <PageCard key={p.id} page={p} />)}
          </div>
        </div>
      )}

      {/* Discover */}
      <div className="space-y-3">
        <h2 className="text-sm font-semibold text-agora-600 dark:text-agora-300 uppercase tracking-wide">
          {searchQuery ? `Results for "${searchQuery}"` : 'Discover pages'}
        </h2>

        {/* Search bar */}
        <form onSubmit={handleSearch} className="relative">
          <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-agora-400" />
          <input
            value={q}
            onChange={e => { setQ(e.target.value); if (!e.target.value) setSearchQuery('') }}
            placeholder="Search pages…"
            className="input pl-9 w-full text-sm"
          />
        </form>

        {isLoading ? (
          <div className="text-center py-8 text-agora-400 text-sm">Loading…</div>
        ) : pages.length === 0 ? (
          <div className="card p-8 text-center text-agora-400 text-sm">
            {searchQuery ? `No pages found for "${searchQuery}".` : 'No pages yet.'}
          </div>
        ) : (
          <div className="grid gap-2 sm:grid-cols-2">
            {pages.map((p: any) => <PageCard key={p.id} page={p} />)}
          </div>
        )}
      </div>
    </div>
  )
}
