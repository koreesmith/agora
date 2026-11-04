import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { Clapperboard, Plus } from 'lucide-react'
import { instanceApi, reelsApi } from '../api'
import ReelSlide from '../components/reels/ReelSlide'
import CreateReelModal from '../components/reels/CreateReelModal'
import { useReelsStore } from '../store/reels'
import type { Post } from '../components/feed/PostCard'

// AGORA-440: Reels. A full-height column of videos, one per screen, that snaps
// from one to the next. Scroll, swipe, the arrow keys or j/k move between them.
// Only the one on screen plays (ReelSlide); this page decides which that is,
// pages more in ahead of the viewer, and tells the server what was watched so
// the same clips don't come back.

type Tab = 'following' | 'discover'

const prefersReducedMotion = () =>
  typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

export default function ReelsPage() {
  const [params, setParams] = useSearchParams()
  const { data: info } = useQuery({
    queryKey: ['instance-info'],
    queryFn: () => instanceApi.getInfo().then(r => r.data),
    staleTime: 5 * 60_000,
  })
  // On unless an admin turned it off (and for an older server that doesn't say).
  const discoverOn = info?.reels_discover_enabled !== false
  const tab: Tab = params.get('tab') === 'discover' && discoverOn ? 'discover' : 'following'
  const setTab = (t: Tab) => setParams(t === 'following' ? {} : { tab: t }, { replace: true })

  const [creating, setCreating] = useState(false)
  const [notice, setNotice] = useState('')

  // "Watch older videos" after being caught up asks the server for everything.
  const [includeViewed, setIncludeViewed] = useState(false)
  useEffect(() => { setIncludeViewed(false) }, [tab])

  const q = useInfiniteQuery({
    queryKey: ['reels', tab, includeViewed],
    queryFn: ({ pageParam }) =>
      reelsApi.list({ tab, cursor: pageParam || undefined, include_viewed: includeViewed || undefined }).then(r => r.data),
    initialPageParam: '',
    getNextPageParam: (last: any) => last.next_cursor || undefined,
    // The list shifts under the viewer as they watch (watched videos drop
    // out), so never refetch it behind their back, and don't keep it once
    // they leave: coming back should start fresh from what is still unwatched.
    staleTime: Infinity,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })

  // Reels the viewer hid or blocked this session stay out of the list.
  const [removedIds, setRemovedIds] = useState<Set<string>>(new Set())
  const [removedAuthors, setRemovedAuthors] = useState<Set<string>>(new Set())
  const posts: Post[] = useMemo(() => {
    const seen = new Set<string>()
    return (q.data?.pages ?? []).flatMap((p: any) => p.posts ?? []).filter((p: Post) => {
      if (seen.has(p.id) || removedIds.has(p.id) || removedAuthors.has(p.author_id)) return false
      seen.add(p.id)
      return true
    })
  }, [q.data, removedIds, removedAuthors])

  const onRemove = useCallback((id: string, blockedAuthorId?: string) => {
    setRemovedIds(s => new Set(s).add(id))
    if (blockedAuthorId) setRemovedAuthors(s => new Set(s).add(blockedAuthorId))
  }, [])

  // ── Which reel is on screen ───────────────────────────────────────────────
  const scroller = useRef<HTMLDivElement>(null)
  const [active, setActive] = useState(0)
  const reduced = useMemo(prefersReducedMotion, [])

  useEffect(() => {
    const root = scroller.current
    if (!root) return
    const obs = new IntersectionObserver(entries => {
      for (const e of entries) {
        if (e.isIntersecting && e.intersectionRatio >= 0.6) {
          setActive(Number((e.target as HTMLElement).dataset.index))
        }
      }
    }, { root, threshold: [0.6] })
    root.querySelectorAll('[data-reel-slide]').forEach(el => obs.observe(el))
    return () => obs.disconnect()
  }, [posts.length, tab, includeViewed])

  const goTo = useCallback((i: number) => {
    const root = scroller.current
    if (!root) return
    const clamped = Math.max(0, Math.min(i, posts.length))
    root.scrollTo({ top: clamped * root.clientHeight, behavior: reduced ? 'auto' : 'smooth' })
  }, [posts.length, reduced])

  // A new reel is posted: say so, and go back to the top, where it will be.
  const onPosted = useCallback(() => {
    setCreating(false)
    setNotice('Your reel is posted')
    setTimeout(() => setNotice(''), 3000)
    setActive(0)
    scroller.current?.scrollTo({ top: 0 })
  }, [])

  // Start from the top whenever the list changes underneath (tab switch,
  // "watch older videos").
  useEffect(() => {
    setActive(0)
    scroller.current?.scrollTo({ top: 0 })
  }, [tab, includeViewed])

  // Page in before the viewer reaches the end.
  useEffect(() => {
    if (active >= posts.length - 3 && q.hasNextPage && !q.isFetchingNextPage && !q.isError) q.fetchNextPage()
  }, [active, posts.length, q.hasNextPage, q.isFetchingNextPage, q.isError])

  // ── Watched ───────────────────────────────────────────────────────────────
  const marked = useRef<Set<string>>(new Set())
  const onViewed = useCallback((id: string) => {
    if (marked.current.has(id)) return
    marked.current.add(id)
    reelsApi.viewed(id).catch(() => { marked.current.delete(id) })
  }, [])

  // ── Keyboard ──────────────────────────────────────────────────────────────
  const muted = useReelsStore(s => s.muted)
  const setMuted = useReelsStore(s => s.setMuted)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return
      if (document.querySelector('[data-reel-dialog]')) return
      switch (e.key) {
        case 'ArrowDown': case 'j': e.preventDefault(); goTo(active + 1); break
        case 'ArrowUp':   case 'k': e.preventDefault(); goTo(active - 1); break
        case 'm': setMuted(!muted); break
        case ' ': {
          e.preventDefault()
          const v = scroller.current?.querySelector<HTMLVideoElement>(`[data-index="${active}"] video`)
          if (v) { if (v.paused) v.play().catch(() => {}); else v.pause() }
          break
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [active, goTo, muted, setMuted])

  const tabBtn = (t: Tab, label: string) => (
    <button key={t} onClick={() => setTab(t)} aria-pressed={tab === t}
      className={`px-3 py-1 rounded-full text-sm font-semibold transition-colors ${
        tab === t ? 'bg-white text-black' : 'text-white/80 hover:text-white'}`}>
      {label}
    </button>
  )

  const empty = !q.isLoading && !q.isError && posts.length === 0
  const caughtUp = !q.isLoading && !q.isError && !q.hasNextPage && posts.length > 0

  return (
    <div className="reels-viewport relative bg-black text-white overflow-hidden md:rounded-xl">
      {/* Tabs float over the video */}
      <div className="absolute top-3 left-3 z-30 flex items-center gap-1 bg-black/40 backdrop-blur-sm rounded-full p-1">
        {tabBtn('following', 'Following')}
        {discoverOn && tabBtn('discover', 'Discover')}
      </div>
      <button onClick={() => setCreating(true)} aria-label="Make a reel" title="Make a reel"
        className="absolute top-3 right-3 z-30 w-11 h-11 rounded-full bg-black/40 backdrop-blur-sm flex items-center justify-center hover:bg-black/60 transition-colors">
        <Plus size={22} />
      </button>
      {notice && (
        <div role="status" className="absolute top-16 left-1/2 -translate-x-1/2 z-30 bg-black/70 text-sm px-3 py-1.5 rounded-full">{notice}</div>
      )}
      {creating && <CreateReelModal onClose={() => setCreating(false)} onPosted={onPosted} />}

      <div ref={scroller} key={`${tab}-${includeViewed}`}
        className="h-full overflow-y-scroll snap-y snap-mandatory no-scrollbar overscroll-contain">
        {posts.map((post, i) => (
          <div key={post.id} className="h-full snap-start snap-always">
            <ReelSlide
              post={post}
              index={i}
              active={i === active}
              near={Math.abs(i - active) <= 1}
              buffer={i === active + 1}
              autoplay={!reduced}
              onViewed={onViewed}
              onRemove={onRemove}
            />
          </div>
        ))}

        {q.isLoading && (
          <div className="h-full snap-start flex items-center justify-center text-white/70" role="status">Loading reels…</div>
        )}

        {q.isError && (
          <div className="h-full snap-start flex flex-col items-center justify-center gap-3 px-8 text-center">
            <p className="font-semibold">Couldn't load reels</p>
            <button onClick={() => q.refetch()} className="text-sm border border-white/40 rounded-lg px-4 py-2 hover:bg-white/10">Try again</button>
          </div>
        )}

        {empty && (
          <div className="h-full snap-start flex flex-col items-center justify-center gap-3 px-8 text-center">
            <Clapperboard size={36} className="text-white/60" />
            {tab === 'following' ? (
              <>
                <p className="font-semibold">No reels from the people you follow yet</p>
                <p className="text-sm text-white/70">Videos from your friends and the accounts you follow show up here.</p>
                {discoverOn && <button onClick={() => setTab('discover')} className="text-sm border border-white/40 rounded-lg px-4 py-2 hover:bg-white/10">See what's on Discover</button>}
                <button onClick={() => setCreating(true)} className="text-sm border border-white/40 rounded-lg px-4 py-2 hover:bg-white/10">Make a reel</button>
              </>
            ) : (
              <>
                <p className="font-semibold">No public reels to show right now</p>
                <p className="text-sm text-white/70">Public videos from this instance and its peers show up here.</p>
                <button onClick={() => setCreating(true)} className="text-sm border border-white/40 rounded-lg px-4 py-2 hover:bg-white/10">Make a reel</button>
              </>
            )}
            {!includeViewed && (
              <button onClick={() => setIncludeViewed(true)} className="text-xs text-white/60 underline">Show videos I've already watched</button>
            )}
          </div>
        )}

        {caughtUp && (
          <div className="h-full snap-start flex flex-col items-center justify-center gap-3 px-8 text-center">
            <Clapperboard size={36} className="text-white/60" />
            <p className="font-semibold">You're all caught up</p>
            <p className="text-sm text-white/70">That's every reel for now. Check back later.</p>
            {!includeViewed && (
              <button onClick={() => setIncludeViewed(true)} className="text-sm border border-white/40 rounded-lg px-4 py-2 hover:bg-white/10">Watch older videos</button>
            )}
            {tab === 'following' && discoverOn && (
              <Link to="/reels?tab=discover" className="text-sm underline">Look at Discover</Link>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
