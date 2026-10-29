import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Search, X, BookOpen, FileText, Users } from 'lucide-react'
import { searchApi } from '../../api'
import { handle, profilePath } from '../../utils/handle'
import { renderName } from '../feed/CommentsSection'
import CustomHandle from '../common/CustomHandle'

// AGORA-304: the global search bar that replaced the sidebar's Search link.
//
// This is a typeahead, not a second search page. It only ever hits the local
// endpoints (searchApi) — the live network calls the full page makes
// (atprotoApi.searchBlueskyActors/searchBlueskyPosts, federationApi.lookupUser)
// reach out to bsky.app and to remote instances, which is fine once on a page
// you deliberately navigated to and much too expensive on every keystroke from
// a bar that now sits on every screen. Anything wider than the local instance
// is the full page's job.
const MAX_PER_GROUP = 3

interface Props {
  // Layout's mobile drawer. The dropdown lives inside a sticky, z-indexed
  // header, so it can't paint over the drawer anyway, but leaving it open
  // behind an overlay the user just opened is its own kind of wrong.
  menuOpen?: boolean
}

export default function SearchBar({ menuOpen }: Props) {
  const navigate = useNavigate()
  const location = useLocation()
  const [params] = useSearchParams()
  const [input, setInput]     = useState('')
  const [typed, setTyped]     = useState('')
  const [open, setOpen]       = useState(false)
  const [cursor, setCursor]   = useState(-1)
  const boxRef      = useRef<HTMLDivElement>(null)
  const inputRef    = useRef<HTMLInputElement>(null)
  const debounce    = useRef<ReturnType<typeof setTimeout>>()

  // On /search this bar is the page's only input (SearchPage dropped its own),
  // so the two stay in step through the URL rather than through props.
  const onSearchPage = location.pathname === '/search'
  const urlQ = (params.get('q') || '').trim()

  // Exactly one of these owns the query at a time, which is what keeps the two
  // directions of URL sync from fighting: on /search the URL is the truth and
  // this bar only writes to it, everywhere else the debounced local value is.
  // Mirroring the URL into state and pushing that state back would be two
  // effects each reacting to the other's output, and on mount each one reads
  // the other's pre-update value: the seed sets the input from ?q= while the
  // publisher, still seeing an empty query, strips ?q= right back off.
  const q = onSearchPage ? urlQ : typed

  const pushQuery = (term: string) => {
    const next = new URLSearchParams(params)
    if (term) next.set('q', term)
    else      next.delete('q')
    // replace so one search doesn't leave a history entry per keystroke-batch.
    navigate({ pathname: '/search', search: next.toString() }, { replace: true })
  }

  // Same 350ms the search page used, so the dropdown and the full results agree
  // on when a query is worth running. Driven from the change handler rather than
  // an effect on `input`, so a URL that changes underneath us never looks like
  // the user typing and never bounces back out as a navigation.
  const onChange = (value: string) => {
    setInput(value)
    setOpen(true)
    clearTimeout(debounce.current)
    debounce.current = setTimeout(() => {
      const term = value.trim()
      if (location.pathname === '/search') pushQuery(term)
      else setTyped(term)
    }, 350)
  }

  useEffect(() => () => clearTimeout(debounce.current), [])

  useEffect(() => setCursor(-1), [q])

  // URL → input, the only sync that remains. A hashtag in post content
  // (renderContent) links straight to /search?tab=posts&q=%23tag without going
  // through this input, and Layout never unmounts across that navigation, so a
  // mount-time seed would miss it. Nothing navigates in response to this, so
  // there is no cycle: our own writes come back identical and fall through.
  useEffect(() => {
    if (!onSearchPage) return
    setInput(prev => prev.trim() === urlQ ? prev : urlQ)
    setTyped(urlQ)
  }, [onSearchPage, urlQ])

  // The full page is already showing everything the dropdown would, in more
  // detail and with the remote sources the dropdown deliberately skips.
  const dropdownEnabled = open && !onSearchPage && q.length >= 2

  const { data: usersData, isFetching: usersFetching } = useQuery({
    queryKey: ['search-users', q],
    queryFn: () => searchApi.searchUsers(q).then(r => r.data),
    enabled: dropdownEnabled,
  })
  const { data: postsData, isFetching: postsFetching } = useQuery({
    queryKey: ['search-posts', q],
    queryFn: () => searchApi.searchPosts(q).then(r => r.data),
    enabled: dropdownEnabled,
  })
  const { data: pagesData, isFetching: pagesFetching } = useQuery({
    queryKey: ['search-pages', q],
    queryFn: () => searchApi.searchPages(q).then(r => r.data),
    enabled: dropdownEnabled,
  })

  const users = (usersData?.users || []).slice(0, MAX_PER_GROUP)
  const posts = (postsData?.posts || []).slice(0, MAX_PER_GROUP)
  const pages = (pagesData?.pages || []).slice(0, MAX_PER_GROUP)
  const isFetching = usersFetching || postsFetching || pagesFetching

  // One flat list behind the three visible groups, so arrow keys walk the
  // dropdown the way it reads rather than per-group.
  const hits = useMemo(() => [
    ...users.map((u: any) => profilePath(u.username)),
    ...posts.map((p: any) => `/post/${p.id}`),
    ...pages.map((p: any) => `/pages/${p.slug}`),
  ], [users, posts, pages])

  const postsFrom = users.length
  const pagesFrom = users.length + posts.length
  const hasResults = hits.length > 0

  const close = () => { setOpen(false); setCursor(-1) }

  const goTo = (to: string) => {
    // You found what you came for; leaving the query behind would just mean
    // the next page loads with a stale search sitting in the bar.
    clearTimeout(debounce.current)
    setInput('')
    setTyped('')
    close()
    inputRef.current?.blur()
    navigate(to)
  }

  const seeAll = () => {
    const term = input.trim()
    if (term.length < 2) return
    clearTimeout(debounce.current)
    close()
    inputRef.current?.blur()
    navigate(`/search?q=${encodeURIComponent(term)}`)
  }

  const clear = () => {
    clearTimeout(debounce.current)
    setInput('')
    setTyped('')
    // On /search the URL is the query, so emptying the box has to empty that too.
    if (onSearchPage) pushQuery('')
    close()
    inputRef.current?.focus()
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape') { close(); inputRef.current?.blur(); return }
    if (e.key === 'Enter') {
      e.preventDefault()
      if (cursor >= 0 && hits[cursor]) goTo(hits[cursor])
      else seeAll()
      return
    }
    if (!hasResults) return
    if (e.key === 'ArrowDown') { e.preventDefault(); setCursor(c => (c + 1) % hits.length) }
    if (e.key === 'ArrowUp')   { e.preventDefault(); setCursor(c => c <= 0 ? hits.length - 1 : c - 1) }
  }

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) close()
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  useEffect(() => { if (menuOpen) close() }, [menuOpen])
  useEffect(() => { close() }, [location.pathname])

  const showDropdown = dropdownEnabled

  return (
    <div ref={boxRef} className="relative flex-1 min-w-0">
      <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-agora-400 pointer-events-none" />
      <input
        ref={inputRef}
        className="input pl-9 pr-8 py-1.5"
        placeholder="Search people and posts…"
        autoComplete="off"
        aria-label="Search"
        value={input}
        onChange={e => onChange(e.target.value)}
        onFocus={() => setOpen(true)}
        onKeyDown={onKeyDown}
      />
      {input && (
        <button
          onClick={clear}
          aria-label="Clear search"
          className="absolute right-2.5 top-1/2 -translate-y-1/2 text-agora-400 hover:text-agora-600 dark:hover:text-agora-200">
          <X size={14} />
        </button>
      )}

      {showDropdown && (
        <div className="absolute left-0 right-0 top-full mt-1.5 card p-1.5 max-h-[70vh] overflow-y-auto shadow-lg">
          {!hasResults && (
            <p className="px-3 py-4 text-sm text-center text-agora-400">
              {isFetching ? 'Searching…' : `No matches for "${q}" on this instance.`}
            </p>
          )}

          <Group icon={Users} title="People" count={users.length}>
            {users.map((u: any, i: number) => (
              <Row key={u.id} to={profilePath(u.username)} active={cursor === i}
                onHover={() => setCursor(i)} onPick={goTo}>
                <span className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                  {u.avatar_url
                    ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
                    : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">
                        {(u.display_name || u.username)[0].toUpperCase()}
                      </span>}
                </span>
                <span className="flex-1 min-w-0">
                  <span className="block text-sm font-medium truncate">
                    {u.display_name ? renderName(u.display_name, u.emojis) : u.username}
                  </span>
                  <span className="block text-xs text-agora-400 truncate">
                    {handle(u.username, u.is_remote, u.remote_instance)}
                    <CustomHandle domain={u.custom_domain} />
                  </span>
                </span>
              </Row>
            ))}
          </Group>

          <Group icon={FileText} title="Posts" count={posts.length}>
            {posts.map((p: any, i: number) => (
              <Row key={p.id} to={`/post/${p.id}`} active={cursor === postsFrom + i}
                onHover={() => setCursor(postsFrom + i)} onPick={goTo}>
                <span className="flex-1 min-w-0">
                  <span className="block text-sm truncate">{p.content}</span>
                  <span className="block text-xs text-agora-400 truncate">
                    {p.display_name || p.username} · @{p.username}
                  </span>
                </span>
              </Row>
            ))}
          </Group>

          <Group icon={BookOpen} title="Pages" count={pages.length}>
            {pages.map((p: any, i: number) => (
              <Row key={p.id} to={`/pages/${p.slug}`} active={cursor === pagesFrom + i}
                onHover={() => setCursor(pagesFrom + i)} onPick={goTo}>
                <span className="w-7 h-7 rounded-xl bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                  {p.avatar_url
                    ? <img src={p.avatar_url} alt="" className="w-full h-full object-cover" />
                    : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-500">
                        {p.display_name[0]}
                      </span>}
                </span>
                <span className="flex-1 min-w-0">
                  <span className="block text-sm font-medium truncate">{p.display_name}</span>
                  <span className="block text-xs text-agora-400 truncate">@{p.slug}</span>
                </span>
              </Row>
            ))}
          </Group>

          {/* Always offered, results or not: the full page searches Bluesky and
              the fediverse as well, so "nothing here" isn't "nothing anywhere". */}
          <button onClick={seeAll}
            className="w-full text-left px-3 py-2 mt-1 rounded-lg text-sm font-medium text-agora-600 dark:text-agora-300 hover:bg-agora-50 dark:hover:bg-agora-700/50 border-t border-agora-100 dark:border-agora-700">
            See all results for "{q}"
          </button>
        </div>
      )}
    </div>
  )
}

function Group({ icon: Icon, title, count, children }: {
  icon: any, title: string, count: number, children: React.ReactNode
}) {
  if (count === 0) return null
  return (
    <div className="mb-1 last:mb-0">
      <p className="flex items-center gap-1.5 px-3 pt-2 pb-1 text-xs font-semibold text-agora-400 uppercase tracking-wide">
        <Icon size={11} /> {title}
      </p>
      {children}
    </div>
  )
}

// Rendered as a Link so middle-click and cmd-click still open a new tab, but
// the plain click is intercepted: goTo also clears the bar and closes the
// dropdown, which a bare navigation wouldn't.
function Row({ to, active, onHover, onPick, children }: {
  to: string, active: boolean, onHover: () => void, onPick: (to: string) => void, children: React.ReactNode
}) {
  return (
    <Link
      to={to}
      onMouseEnter={onHover}
      onClick={e => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
        e.preventDefault()
        onPick(to)
      }}
      className={`flex items-center gap-2.5 px-3 py-2 rounded-lg transition-colors ${
        active ? 'bg-agora-100 dark:bg-agora-700' : 'hover:bg-agora-50 dark:hover:bg-agora-700/50'
      }`}>
      {children}
    </Link>
  )
}
