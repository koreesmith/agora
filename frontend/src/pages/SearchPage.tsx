import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { searchApi, friendsApi, federationApi, pagesApi, atprotoApi } from '../api'
import { useAuthStore } from '../store/auth'
import { handle } from '../utils/handle'
import { formatDistanceToNow } from 'date-fns'
import { renderContent } from '../components/feed/CommentsSection'
import { Search, Users, FileText, Heart, MessageCircle, Clock, UserPlus, Check, Link2, BookOpen, ExternalLink } from 'lucide-react'
import { useMutation as useSubscribeMutation } from '@tanstack/react-query'

export default function SearchPage() {
  // AGORA-217: a hashtag link elsewhere in the app (renderContent) navigates
  // here as /search?tab=posts&q=%23tag — pre-fill from the URL once on mount
  // rather than two-way-binding every keystroke back into it.
  const [params] = useSearchParams()
  const [input, setInput] = useState(() => params.get('q') || '')
  const [q, setQ] = useState(() => params.get('q') || '')
  const [tab, setTab] = useState<'users'|'posts'|'pages'>(() => {
    const t = params.get('tab')
    return t === 'posts' || t === 'pages' ? t : 'users'
  })
  const debounceTimer = useRef<ReturnType<typeof setTimeout>>()
  const { user } = useAuthStore()
  const qc = useQueryClient()

  useEffect(() => {
    clearTimeout(debounceTimer.current)
    debounceTimer.current = setTimeout(() => setQ(input), 350)
    return () => clearTimeout(debounceTimer.current)
  }, [input])

  const isHandleLookup = /^[a-zA-Z0-9_-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(q.trim())

  const enabled = q.length >= 2

  const { data: usersData, isFetching: usersFetching } = useQuery({
    queryKey: ['search-users', q],
    queryFn: () => searchApi.searchUsers(q).then(r => r.data),
    enabled: enabled && tab === 'users' && !isHandleLookup,
  })
  const { data: postsData, isFetching: postsFetching } = useQuery({
    queryKey: ['search-posts', q],
    queryFn: () => searchApi.searchPosts(q).then(r => r.data),
    enabled: enabled && tab === 'posts' && !isHandleLookup,
  })
  const { data: pagesData, isFetching: pagesFetching } = useQuery({
    queryKey: ['search-pages', q],
    queryFn: () => searchApi.searchPages(q).then(r => r.data),
    enabled: enabled && tab === 'pages' && !isHandleLookup,
  })
  // AGORA-215/216: real, live, network-wide Bluesky results — kept as
  // separate queries/groups from searchApi's own Agora+cached-remote rows
  // rather than merged, so the UI can label coverage honestly per source.
  const { data: bskyActorsData, isFetching: bskyActorsFetching } = useQuery({
    queryKey: ['search-bsky-actors', q],
    queryFn: () => atprotoApi.searchBlueskyActors(q).then(r => r.data),
    enabled: enabled && tab === 'users' && !isHandleLookup,
  })
  const { data: bskyPostsData, isFetching: bskyPostsFetching } = useQuery({
    queryKey: ['search-bsky-posts', q],
    queryFn: () => atprotoApi.searchBlueskyPosts(q).then(r => r.data),
    enabled: enabled && tab === 'posts' && !isHandleLookup,
  })
  const followBluesky = useMutation({
    mutationFn: (actor: string) => atprotoApi.followBlueskyAccount(actor),
  })

  const subscribeToPage = useSubscribeMutation({
    mutationFn: (slug: string) => pagesApi.subscribe(slug),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['search-pages', q] }),
  })
  const unsubscribeFromPage = useSubscribeMutation({
    mutationFn: (slug: string) => pagesApi.unsubscribe(slug),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['search-pages', q] }),
  })

  // Cross-instance handle lookup: user@instance.com
  const { data: lookupData, isFetching: lookupFetching, error: lookupError } = useQuery({
    queryKey: ['federation-lookup', q],
    queryFn: () => federationApi.lookupUser(q.trim()).then(r => r.data),
    enabled: isHandleLookup,
    retry: false,
  })

  const send = useMutation({
    mutationFn: (id: string) => friendsApi.sendRequest(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['search-users', q] }),
  })

  const users = usersData?.users || []
  const posts = postsData?.posts || []
  const pages = pagesData?.pages || []

  // Agora's own local rows and cached remote rows already tell you whether a
  // row is remote and, if so, what remote_instance it came from — 'bsky.app'
  // is the marker AGORA-197's own ingestion stamps on Bluesky-origin rows
  // (see internal/atproto/ingest.go), never used by fediverse ingestion, so
  // it cleanly separates "on the fediverse" from "on Bluesky" within the same
  // already-cached result set.
  const agoraUsers = users.filter((u: any) => !u.is_remote)
  const fediverseUsers = users.filter((u: any) => u.is_remote && u.remote_instance !== 'bsky.app')
  const bskyActors = bskyActorsData?.disabled ? [] : (bskyActorsData?.actors || [])

  const agoraPosts = posts.filter((p: any) => !p.is_remote)
  const fediversePosts = posts.filter((p: any) => p.is_remote && p.remote_instance !== 'bsky.app')
  const cachedBskyPosts = posts.filter((p: any) => p.is_remote && p.remote_instance === 'bsky.app')
  const liveBskyPosts = bskyPostsData?.disabled ? [] : (bskyPostsData?.posts || [])

  const isFetching = isHandleLookup ? lookupFetching
    : tab === 'users' ? (usersFetching || bskyActorsFetching)
    : tab === 'posts' ? (postsFetching || bskyPostsFetching)
    : pagesFetching

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold">Search</h1>

      {/* Search input */}
      <div className="relative">
        <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-agora-400" />
        <input
          className="input pl-9"
          placeholder="Search people and posts…"
          autoComplete="off"
          value={input}
          onChange={e => setInput(e.target.value)}
          autoFocus
        />
        {isFetching && q.length >= 2 && (
          <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-agora-400 animate-pulse">searching…</span>
        )}
      </div>

      {/* Cross-instance handle lookup */}
      {isHandleLookup ? (
        <div className="space-y-3">
          <p className="text-xs text-agora-400 px-1 flex items-center gap-1.5">
            <Link2 size={12} /> Looking up federated user
          </p>
          {lookupFetching && <div className="text-center py-6 text-agora-400 text-sm">Contacting remote instance…</div>}
          {!lookupFetching && lookupError && (
            <div className="card p-8 text-center text-agora-400 space-y-1">
              <p className="font-medium">User not found</p>
              <p className="text-sm">Make sure the handle is correct and the instance is reachable.</p>
            </div>
          )}
          {!lookupFetching && lookupData?.user && (
            <UserResult
              user={{ ...lookupData.user, friendship_status: '' }}
              currentUserId={user?.id}
              onAdd={() => {}}
              addPending={false}
            />
          )}
        </div>
      ) : (
        <>
          {/* Tabs */}
          <div className="flex gap-1 bg-agora-100 dark:bg-agora-800 rounded-lg p-1">
            <button onClick={() => setTab('users')}
              className={`flex-1 flex items-center justify-center gap-1.5 py-1.5 text-sm font-medium rounded-md transition-colors ${tab === 'users' ? 'bg-white dark:bg-agora-700 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
              <Users size={13} /> People
            </button>
            <button onClick={() => setTab('posts')}
              className={`flex-1 flex items-center justify-center gap-1.5 py-1.5 text-sm font-medium rounded-md transition-colors ${tab === 'posts' ? 'bg-white dark:bg-agora-700 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
              <FileText size={13} /> Posts
            </button>
            <button onClick={() => setTab('pages')}
              className={`flex-1 flex items-center justify-center gap-1.5 py-1.5 text-sm font-medium rounded-md transition-colors ${tab === 'pages' ? 'bg-white dark:bg-agora-700 shadow-sm' : 'text-agora-500 hover:text-agora-700'}`}>
              <BookOpen size={13} /> Pages
            </button>
          </div>

          {/* Empty state */}
          {!enabled && (
            <div className="card p-8 text-center text-agora-400 text-sm">
              <Search size={28} className="mx-auto mb-2 opacity-40" />
              Type at least 2 characters to search, or enter <span className="font-mono">user@instance.com</span> to find someone on another instance.
            </div>
          )}

          {/* People tab — grouped by source so coverage isn't oversold: the
              fediverse group is only ever accounts already known to this
              instance, while the Bluesky group is a real, live, network-wide
              search (AGORA-215/216/217). */}
          {tab === 'users' && enabled && (
            <div className="space-y-4">
              {agoraUsers.length === 0 && fediverseUsers.length === 0 && bskyActors.length === 0 && !isFetching && (
                <div className="card p-8 text-center text-agora-400">No people found for "{q}".</div>
              )}
              <ResultGroup title="On Agora" count={agoraUsers.length}>
                {agoraUsers.map((u: any) => (
                  <UserResult key={u.id} user={u} currentUserId={user?.id}
                    onAdd={() => send.mutate(u.id)} addPending={send.isPending} />
                ))}
              </ResultGroup>
              <ResultGroup title="On the Fediverse (already known to this instance)" count={fediverseUsers.length}>
                {fediverseUsers.map((u: any) => (
                  <UserResult key={u.id} user={u} currentUserId={user?.id}
                    onAdd={() => send.mutate(u.id)} addPending={send.isPending} />
                ))}
              </ResultGroup>
              <ResultGroup title="On Bluesky" count={bskyActors.length}>
                {bskyActors.map((a: any) => (
                  <BlueskyActorResult key={a.did} actor={a}
                    onFollow={() => followBluesky.mutate(a.did)} followPending={followBluesky.isPending} />
                ))}
              </ResultGroup>
            </div>
          )}

          {/* Posts tab — same source grouping as People. The Bluesky group
              mixes already-cached rows (this instance's own posts table,
              AGORA-214) with live network results (AGORA-216); both are
              genuinely "on Bluesky", unlike the fediverse group above which
              is explicitly scoped to what's already cached. */}
          {tab === 'posts' && enabled && (
            <div className="space-y-4">
              {agoraPosts.length === 0 && fediversePosts.length === 0 && cachedBskyPosts.length === 0 && liveBskyPosts.length === 0 && !isFetching && (
                <div className="card p-8 text-center text-agora-400">No posts found for "{q}".</div>
              )}
              <ResultGroup title="On Agora" count={agoraPosts.length}>
                {agoraPosts.map((p: any) => <PostResult key={p.id} post={p} query={q} />)}
              </ResultGroup>
              <ResultGroup title="On the Fediverse (already known to this instance)" count={fediversePosts.length}>
                {fediversePosts.map((p: any) => <PostResult key={p.id} post={p} query={q} />)}
              </ResultGroup>
              <ResultGroup title="On Bluesky" count={cachedBskyPosts.length + liveBskyPosts.length}>
                {cachedBskyPosts.map((p: any) => <PostResult key={p.id} post={p} query={q} />)}
                {liveBskyPosts.map((p: any) => <BlueskyPostResult key={p.uri} post={p} />)}
              </ResultGroup>
            </div>
          )}

          {/* Pages tab */}
          {tab === 'pages' && enabled && (
            <div className="space-y-2">
              {pages.length === 0 && !pagesFetching && (
                <div className="card p-8 text-center text-agora-400">No pages found for "{q}".</div>
              )}
              {pages.map((p: any) => (
                <div key={p.id} className="card p-3 flex items-center gap-3">
                  <Link to={`/pages/${p.slug}`} className="w-10 h-10 rounded-xl bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
                    {p.avatar_url
                      ? <img src={p.avatar_url} alt="" className="w-full h-full object-cover" />
                      : <span className="w-full h-full flex items-center justify-center font-bold text-agora-500 text-sm">{p.display_name[0]}</span>}
                  </Link>
                  <div className="flex-1 min-w-0">
                    <Link to={`/pages/${p.slug}`} className="font-semibold text-sm hover:underline flex items-center gap-1">
                      {p.display_name}
                      {p.is_verified && <span className="text-blue-500 text-xs">✓</span>}
                    </Link>
                    <p className="text-xs text-agora-400">@{p.slug} · {p.subscriber_count} subscribers</p>
                    {p.bio && <p className="text-xs text-agora-500 mt-0.5 line-clamp-1">{p.bio}</p>}
                  </div>
                  <button
                    onClick={() => p.is_subscribed ? unsubscribeFromPage.mutate(p.slug) : subscribeToPage.mutate(p.slug)}
                    className={p.is_subscribed ? 'btn-secondary text-xs' : 'btn-primary text-xs'}>
                    {p.is_subscribed ? 'Subscribed' : 'Subscribe'}
                  </button>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}

// ── User result card ───────────────────────────────────────────────────────────

function UserResult({ user: u, currentUserId, onAdd, addPending }: {
  user: any, currentUserId?: string, onAdd: () => void, addPending: boolean
}) {
  const [sent, setSent] = useState(u.friendship_status === 'pending')

  const handleAdd = () => { setSent(true); onAdd() }
  const isSelf = u.id === currentUserId

  return (
    <div className="card p-3 flex items-center gap-3">
      <Link to={`/profile/${u.username}`} className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
        {u.avatar_url
          ? <img src={u.avatar_url} alt="" className="w-full h-full object-cover" />
          : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600">{(u.display_name||u.username)[0].toUpperCase()}</span>}
      </Link>
      <div className="flex-1 min-w-0">
        <Link to={`/profile/${u.username}`} className="font-medium text-sm hover:underline block truncate">
          {u.display_name || u.username}
        </Link>
        <p className="text-xs text-agora-400 truncate">
          {handle(u.username, u.is_remote, u.remote_instance)}
        </p>
        {u.bio && <p className="text-xs text-agora-500 truncate mt-0.5">{u.bio}</p>}
      </div>
      {!isSelf && (
        <div className="flex-shrink-0">
          {u.friendship_status === 'accepted' && (
            <span className="text-xs text-agora-400 flex items-center gap-1"><Check size={12} /> Friends</span>
          )}
          {(u.friendship_status === 'pending' || sent) && (
            <span className="text-xs text-agora-400 flex items-center gap-1"><Clock size={12} /> Pending</span>
          )}
          {u.friendship_status === 'pending_incoming' && (
            <Link to={`/profile/${u.username}`} className="text-xs text-agora-600 hover:underline">Respond</Link>
          )}
          {!u.friendship_status && !sent && (
            <button onClick={handleAdd} disabled={addPending}
              className="btn-primary text-xs py-1 px-2.5 flex items-center gap-1">
              <UserPlus size={12} /> Add
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// ── Post result card ───────────────────────────────────────────────────────────

function PostResult({ post: p, query }: { post: any, query: string }) {
  // Highlight the matched query term in the content snippet
  const highlight = (text: string) => {
    const idx = text.toLowerCase().indexOf(query.toLowerCase())
    if (idx === -1) return <span>{text.slice(0, 200)}</span>
    const start = Math.max(0, idx - 60)
    const end = Math.min(text.length, idx + query.length + 100)
    const snippet = (start > 0 ? '…' : '') + text.slice(start, end) + (end < text.length ? '…' : '')
    const matchStart = snippet.toLowerCase().indexOf(query.toLowerCase())
    if (matchStart === -1) return <span>{snippet}</span>
    return (
      <>
        {snippet.slice(0, matchStart)}
        <mark className="bg-yellow-100 dark:bg-yellow-900/40 text-inherit rounded px-0.5">
          {snippet.slice(matchStart, matchStart + query.length)}
        </mark>
        {snippet.slice(matchStart + query.length)}
      </>
    )
  }

  return (
    <Link to={`/post/${p.id}`} className="card p-4 block hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors">
      <div className="flex items-center gap-2 mb-2">
        <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {p.avatar_url
            ? <img src={p.avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{(p.display_name||p.username)[0].toUpperCase()}</span>}
        </div>
        <span className="text-sm font-medium">{p.display_name || p.username}</span>
        <span className="text-xs text-agora-400">@{p.username}</span>
        <span className="text-xs text-agora-400 ml-auto flex items-center gap-1">
          <Clock size={11} />
          {formatDistanceToNow(new Date(p.created_at), { addSuffix: true })}
        </span>
      </div>
      <p className="text-sm text-agora-700 dark:text-agora-300 line-clamp-3">
        {highlight(p.content)}
      </p>
      {p.image_url && (
        <img src={p.image_url} alt="" className="mt-2 rounded-lg max-h-32 object-cover" />
      )}
      <div className="flex items-center gap-4 mt-2 text-xs text-agora-400">
        <span className="flex items-center gap-1"><Heart size={11} />{p.like_count}</span>
        <span className="flex items-center gap-1"><MessageCircle size={11} />{p.comment_count}</span>
      </div>
    </Link>
  )
}

// ── Result group wrapper (AGORA-217) — hides itself when empty rather than
// rendering an empty labeled section for a source with no matches.
function ResultGroup({ title, count, children }: { title: string, count: number, children: React.ReactNode }) {
  if (count === 0) return null
  return (
    <div className="space-y-2">
      <h2 className="text-xs font-semibold text-agora-400 uppercase tracking-wide px-1">{title}</h2>
      {children}
    </div>
  )
}

// ── Bluesky actor result (AGORA-215/217) ────────────────────────────────────
// Real, live, network-wide — unlike UserResult's Agora/fediverse rows, this
// never came from this instance's own users table, so it links straight out
// to bsky.app rather than an internal /profile/:username route, and its
// only action is Follow (AGORA-195's existing followBlueskyAccount), no
// friend-request flow.
function BlueskyActorResult({ actor: a, onFollow, followPending }: {
  actor: any, onFollow: () => void, followPending: boolean
}) {
  const [followed, setFollowed] = useState(false)
  const handleFollow = () => { setFollowed(true); onFollow() }

  return (
    <div className="card p-3 flex items-center gap-3">
      <a href={`https://bsky.app/profile/${a.handle}`} target="_blank" rel="noreferrer noopener"
        className="w-10 h-10 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
        {a.avatar_url
          ? <img src={a.avatar_url} alt="" className="w-full h-full object-cover" />
          : <span className="w-full h-full flex items-center justify-center font-bold text-agora-600">{(a.display_name || a.handle)[0].toUpperCase()}</span>}
      </a>
      <div className="flex-1 min-w-0">
        <a href={`https://bsky.app/profile/${a.handle}`} target="_blank" rel="noreferrer noopener"
          className="font-medium text-sm hover:underline flex items-center gap-1 truncate">
          <span className="truncate">{a.display_name || a.handle}</span>
          <ExternalLink size={11} className="text-agora-400 flex-shrink-0" />
        </a>
        <p className="text-xs text-agora-400 truncate">@{a.handle} · Bluesky</p>
        {a.description && <p className="text-xs text-agora-500 truncate mt-0.5">{a.description}</p>}
      </div>
      <button onClick={handleFollow} disabled={followPending || followed}
        className="btn-primary text-xs py-1 px-2.5 flex items-center gap-1 flex-shrink-0">
        <UserPlus size={12} /> {followed ? 'Following' : 'Follow'}
      </button>
    </div>
  )
}

// ── Bluesky post result (AGORA-216/217) ─────────────────────────────────────
// Never ingested into the local posts table (the ticket's own explicit
// read-only constraint), so there's no local post ID to route to — links
// straight out to the real bsky.app post instead.
function BlueskyPostResult({ post: p }: { post: any }) {
  // at://did/app.bsky.feed.post/rkey — bsky.app's own web URL takes the
  // handle (not the DID) plus that trailing rkey.
  const rkey = p.uri.split('/').pop()
  const url = `https://bsky.app/profile/${p.author_handle}/post/${rkey}`

  return (
    <a href={url} target="_blank" rel="noreferrer noopener"
      className="card p-4 block hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors">
      <div className="flex items-center gap-2 mb-2">
        <div className="w-7 h-7 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
          {p.author_avatar_url
            ? <img src={p.author_avatar_url} alt="" className="w-full h-full object-cover" />
            : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-600">{(p.author_display_name || p.author_handle)[0].toUpperCase()}</span>}
        </div>
        <span className="text-sm font-medium">{p.author_display_name || p.author_handle}</span>
        <span className="text-xs text-agora-400">@{p.author_handle}</span>
        <span className="text-xs text-agora-400 ml-auto flex items-center gap-1">
          <Clock size={11} />
          {formatDistanceToNow(new Date(p.created_at), { addSuffix: true })}
        </span>
      </div>
      <p className="text-sm text-agora-700 dark:text-agora-300 line-clamp-3">{p.text}</p>
      <div className="flex items-center gap-4 mt-2 text-xs text-agora-400">
        <span className="flex items-center gap-1"><Heart size={11} />{p.like_count}</span>
        <span className="flex items-center gap-1"><ExternalLink size={11} />Bluesky</span>
      </div>
    </a>
  )
}
