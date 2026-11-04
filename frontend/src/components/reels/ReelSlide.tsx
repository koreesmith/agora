import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, Bookmark, EyeOff, Flag, MessageCircle, MoreHorizontal, Pause, Play, RotateCcw, RotateCw, Repeat2, Share, ThumbsUp, Volume2, VolumeX, X, Ban } from 'lucide-react'
import { useMutation } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { feedApi, bookmarksApi, hiddenPostsApi, blocksApi } from '../../api'
import { useReelsStore } from '../../store/reels'
import { useAuthStore } from '../../store/auth'
import { renderContent, renderName } from '../feed/CommentsSection'
import ReportModal from '../feed/ReportModal'
import { isShareable, shareUrl } from '../feed/ShareMenu'
import { handle } from '../../utils/handle'
import { REACTION_MAP } from '../../utils/reactions'
import type { Post } from '../feed/PostCard'
import ReelVideo from './ReelVideo'

// AGORA-440: one reel. Only the slide on screen plays; the ones either side
// keep a real <video> so the next swipe is instant, and every other slide is
// just its poster, which is what keeps memory flat through a long session.

interface Props {
  post: Post
  index: number
  active: boolean
  near: boolean          // within one slide of the visible one: mount a <video>
  buffer: boolean        // the slide after the visible one: preload it
  autoplay: boolean      // false when the person asked for reduced motion
  onViewed: (id: string) => void
  onRemove: (id: string, blockedAuthorId?: string) => void
}

const VIEWED_AFTER_MS = 3000

function networkLabel(post: Post): string {
  if (!post.is_remote) return ''
  return post.remote_instance === 'bsky.app' ? 'Bluesky' : (post.remote_instance || 'Fediverse')
}

export default function ReelSlide({ post, index, active, near, buffer, autoplay, onViewed, onRemove }: Props) {
  const muted = useReelsStore((s) => s.muted)
  const setMuted = useReelsStore((s) => s.setMuted)
  const videoRef = useRef<HTMLVideoElement | null>(null)

  const [loaded, setLoaded] = useState(false)
  const [ready, setReady] = useState(false)     // frames are actually showing
  const [paused, setPaused] = useState(true)
  const [failed, setFailed] = useState(false)
  const [progress, setProgress] = useState(0)
  // AGORA-450: dragging the bar. The video pauses while it is held and plays on
  // from the new place when it lets go, if it was playing.
  const barRef = useRef<HTMLDivElement | null>(null)
  const [scrubRatio, setScrubRatio] = useState<number | null>(null)
  const wasPlaying = useRef(false)
  // AGORA-451: a click brings up the controls instead of pausing. While the
  // video plays they hide again after a few seconds; `controlsTick` restarts
  // that wait whenever one of them is used.
  const [controlsShown, setControlsShown] = useState(false)
  const [controlsTick, setControlsTick] = useState(0)
  const [cwShown, setCwShown] = useState(false)
  const [captionOpen, setCaptionOpen] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const [showReport, setShowReport] = useState(false)
  const [showRepost, setShowRepost] = useState(false)
  const [toast, setToast] = useState('')

  const cwOk = !post.content_warning || cwShown
  // Hide, report and block make no sense on your own reel.
  const isOwn = useAuthStore((s) => s.user?.id) === post.author_id

  const flash = (msg: string) => { setToast(msg); setTimeout(() => setToast(''), 2200) }

  // Play the slide on screen, pause and rewind the rest.
  useEffect(() => {
    const v = videoRef.current
    if (!v) return
    if (active && cwOk && autoplay && !failed) {
      v.play().catch(() => {
        // Autoplay with sound is refused until the person has interacted with
        // the page: fall back to muted rather than showing a dead frame.
        if (!v.muted) { setMuted(true); v.muted = true; v.play().catch(() => {}) }
      })
    } else {
      v.pause()
      if (!active) { try { v.currentTime = 0 } catch { /* not seekable yet */ } setProgress(0) }
    }
  }, [active, cwOk, autoplay, failed, near, loaded, setMuted])

  // A slide that scrolls out of range drops its <video>; forget what that
  // video had shown so the poster covers the gap when it comes back.
  useEffect(() => {
    if (!near) { setReady(false); setLoaded(false); setPaused(true) }
  }, [near])

  // Count it as watched after a few seconds of actual playback.
  useEffect(() => {
    if (!active || paused || failed || !ready) return
    const t = setTimeout(() => onViewed(post.id), VIEWED_AFTER_MS)
    return () => clearTimeout(t)
  }, [active, paused, failed, ready, post.id, onViewed])

  const skip = (seconds: number) => {
    const v = videoRef.current
    if (!v || !(v.duration > 0)) return
    v.currentTime = Math.max(0, Math.min(v.duration, v.currentTime + seconds))
  }

  const ratioFrom = (clientX: number) => {
    const r = barRef.current?.getBoundingClientRect()
    if (!r || r.width <= 0) return 0
    return Math.max(0, Math.min(1, (clientX - r.left) / r.width))
  }
  const seekTo = (ratio: number) => {
    const v = videoRef.current
    setScrubRatio(ratio)
    if (v && v.duration > 0) v.currentTime = ratio * v.duration
  }
  const onBarDown = (e: React.PointerEvent<HTMLDivElement>) => {
    e.stopPropagation()
    const v = videoRef.current
    if (!v || failed) return
    e.currentTarget.setPointerCapture(e.pointerId)
    wasPlaying.current = !v.paused
    v.pause()
    seekTo(ratioFrom(e.clientX))
  }
  const onBarMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (scrubRatio === null) return
    seekTo(ratioFrom(e.clientX))
  }
  const onBarUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (scrubRatio === null) return
    e.stopPropagation()
    setScrubRatio(null)
    if (wasPlaying.current) videoRef.current?.play().catch(() => {})
  }
  const clock = (sec: number) => `${Math.floor(sec / 60)}:${String(Math.floor(sec % 60)).padStart(2, '0')}`

  useEffect(() => {
    if (!controlsShown || paused) return
    const t = setTimeout(() => setControlsShown(false), 3000)
    return () => clearTimeout(t)
  }, [controlsShown, paused, controlsTick])
  useEffect(() => { if (!active) setControlsShown(false) }, [active])

  // A click on the picture: play it if paused, otherwise show or hide the controls.
  const onSurfaceClick = () => {
    const v = videoRef.current
    if (!v || failed) return
    if (v.paused) { v.play().catch(() => {}); setControlsShown(false) }
    else setControlsShown((c) => !c)
  }

  const togglePlay = () => {
    const v = videoRef.current
    if (!v || failed) return
    if (v.paused) v.play().catch(() => {}); else v.pause()
  }

  // ── Actions, the same endpoints PostCard uses ───────────────────────────
  const [myReaction, setMyReaction] = useState(post.my_reaction || (post.liked ? 'like' : ''))
  const [reactionCount, setReactionCount] = useState(post.reaction_count || post.like_count || 0)
  const react = useMutation({
    mutationFn: (type: string | null) => type ? feedApi.reactPost(post.id, type) : feedApi.unreactPost(post.id),
  })
  const toggleLike = () => {
    if (myReaction) { setMyReaction(''); setReactionCount((n) => Math.max(0, n - 1)); react.mutate(null) }
    else { setMyReaction('like'); setReactionCount((n) => n + 1); react.mutate('like') }
  }

  const [bookmarked, setBookmarked] = useState(!!post.bookmarked)
  const toggleBookmark = useMutation({
    mutationFn: (save: boolean) => save ? bookmarksApi.add(post.id) : bookmarksApi.remove(post.id),
    onMutate: (save: boolean) => setBookmarked(save),
    onError: (_e, save) => setBookmarked(!save),
  })

  const hide = useMutation({
    mutationFn: () => hiddenPostsApi.hide(post.id),
    onSuccess: () => onRemove(post.id),
  })
  const block = useMutation({
    mutationFn: () => blocksApi.block(post.author_username),
    onSuccess: () => onRemove(post.id, post.author_id),
  })

  const [repostText, setRepostText] = useState('')
  const [repostVisibility, setRepostVisibility] = useState<'public' | 'friends'>('friends')
  const repost = useMutation({
    mutationFn: () => feedApi.repost(post.id, { content: repostText, visibility: repostVisibility }),
    onSuccess: () => { setShowRepost(false); setRepostText(''); flash('Reposted') },
    onError: (e: any) => flash(e.response?.data?.error || 'Could not repost'),
  })

  const share = async () => {
    const url = shareUrl(post)
    try {
      if (navigator.share) { await navigator.share({ url }); return }
      await navigator.clipboard.writeText(url)
      flash('Link copied')
    } catch { /* dismissed */ }
  }

  const name = post.author_display_name || post.author_username
  const network = networkLabel(post)
  const shareable = isShareable(post)

  const action = 'flex flex-col items-center gap-1 text-white drop-shadow pointer-events-auto'
  const round = 'w-11 h-11 rounded-full bg-black/40 backdrop-blur-sm flex items-center justify-center hover:bg-black/60 transition-colors'

  return (
    <section
      data-reel-slide
      data-index={index}
      aria-label={`Reel by ${name}`}
      className="relative h-full snap-start snap-always bg-black text-white overflow-hidden select-none"
    >
      {/* Picture */}
      <div className="absolute inset-0" onClick={onSurfaceClick} role="button" tabIndex={-1}
        aria-label={paused ? 'Play' : 'Show controls'}>
        {post.video_thumb_url && !ready && (
          <img src={post.video_thumb_url} alt="" draggable={false}
            className="absolute inset-0 w-full h-full object-contain" />
        )}
        {near && cwOk && !failed && post.video_url && (
          <ReelVideo
            ref={videoRef}
            src={post.video_url}
            poster={post.video_thumb_url || undefined}
            muted={muted}
            preload={active || buffer ? 'auto' : 'metadata'}
            className="absolute inset-0 w-full h-full object-contain"
            onError={() => setFailed(true)}
            onPlay={() => setPaused(false)}
            onPause={() => setPaused(true)}
            onReady={() => setReady(true)}
            onLoaded={() => setLoaded(true)}
            onTime={(t, d) => { if (d > 0) setProgress(t / d) }}
          />
        )}
      </div>

      {/* Controls (paused, or just clicked): play or pause, with 10 seconds back and forward either side. Outside
          the click-to-play surface so the buttons never toggle it. */}
      {active && (paused || controlsShown) && ready && !failed && cwOk && scrubRatio === null && (
        <div className="absolute inset-0 z-10 flex items-center justify-center gap-6 pointer-events-none">
          <button onClick={() => { skip(-10); setControlsTick((n) => n + 1) }} aria-label="Back 10 seconds" title="Back 10 seconds"
            className="pointer-events-auto relative w-12 h-12 rounded-full bg-black/50 hover:bg-black/70 flex items-center justify-center">
            <RotateCcw size={22} />
            <span className="absolute text-[10px] font-bold mt-0.5">10</span>
          </button>
          <button onClick={() => { togglePlay(); setControlsShown(paused); setControlsTick((n) => n + 1) }}
            aria-label={paused ? 'Play' : 'Pause'}
            className="pointer-events-auto w-16 h-16 rounded-full bg-black/50 hover:bg-black/70 flex items-center justify-center">
            {paused ? <Play size={30} fill="currentColor" /> : <Pause size={30} fill="currentColor" />}
          </button>
          <button onClick={() => { skip(10); setControlsTick((n) => n + 1) }} aria-label="Forward 10 seconds" title="Forward 10 seconds"
            className="pointer-events-auto relative w-12 h-12 rounded-full bg-black/50 hover:bg-black/70 flex items-center justify-center">
            <RotateCw size={22} />
            <span className="absolute text-[10px] font-bold mt-0.5">10</span>
          </button>
        </div>
      )}

      {/* Content warning gate */}
      {post.content_warning && !cwShown && (
        <div className="absolute inset-0 z-10 bg-black flex flex-col items-center justify-center gap-3 px-8 text-center">
          <AlertTriangle size={28} className="text-amber-400" />
          <p className="font-semibold">Trigger Warning</p>
          <p className="text-sm text-white/70">{post.content_warning}</p>
          <button onClick={() => setCwShown(true)}
            className="mt-1 text-sm font-medium border border-white/40 rounded-lg px-4 py-2 hover:bg-white/10">
            Show reel
          </button>
        </div>
      )}

      {failed && (
        <div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-3 px-8 text-center bg-black/80">
          <p className="font-semibold">Couldn't play this video</p>
          <p className="text-sm text-white/70">It may have been removed, or the server it lives on isn't reachable.</p>
          <Link to={`/post/${post.id}`} className="text-sm underline">Open the post</Link>
        </div>
      )}

      {/* Shading so the text stays readable over any video */}
      <div className="absolute inset-x-0 bottom-0 h-2/5 bg-gradient-to-t from-black/70 to-transparent pointer-events-none" />

      {/* Top right: sound and more */}
      <div className="absolute top-16 right-3 z-20 flex items-center gap-2">
        <button onClick={() => setMuted(!muted)} className={round}
          aria-label={muted ? 'Turn sound on' : 'Turn sound off'} title={muted ? 'Sound off' : 'Sound on'}>
          {muted ? <VolumeX size={18} /> : <Volume2 size={18} />}
        </button>
        {!isOwn && <div className="relative">
          <button onClick={() => setMenuOpen((o) => !o)} className={round} aria-label="More" aria-haspopup="menu" aria-expanded={menuOpen}>
            <MoreHorizontal size={18} />
          </button>
          {menuOpen && (
            <div role="menu" className="absolute right-0 top-12 min-w-[170px] bg-white dark:bg-agora-800 text-agora-700 dark:text-agora-200 rounded-lg shadow-lg border border-agora-200 dark:border-agora-700 py-1">
              <button role="menuitem" onClick={() => { setMenuOpen(false); hide.mutate() }}
                className="flex items-center gap-2 w-full px-3 py-2 text-sm hover:bg-agora-50 dark:hover:bg-agora-700">
                <EyeOff size={14} /> Hide post
              </button>
              <button role="menuitem" onClick={() => { setMenuOpen(false); setShowReport(true) }}
                className="flex items-center gap-2 w-full px-3 py-2 text-sm hover:bg-agora-50 dark:hover:bg-agora-700">
                <Flag size={14} /> Report
              </button>
              <button role="menuitem"
                onClick={() => { setMenuOpen(false); if (confirm(`Block ${handle(post.author_username, post.is_remote, post.remote_instance)}? You won't see each other's posts.`)) block.mutate() }}
                className="flex items-center gap-2 w-full px-3 py-2 text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20">
                <Ban size={14} /> Block
              </button>
            </div>
          )}
        </div>}
      </div>

      {/* Right rail: the actions */}
      <div className="absolute right-3 bottom-6 z-20 flex flex-col items-center gap-4 pointer-events-none">
        <button onClick={toggleLike} className={action} aria-pressed={!!myReaction}
          aria-label={myReaction ? 'Remove reaction' : 'Like'}>
          <span className={round}>
            {myReaction ? <span className="text-lg leading-none">{REACTION_MAP[myReaction]?.emoji}</span> : <ThumbsUp size={20} />}
          </span>
          <span className="text-xs">{reactionCount || ''}</span>
        </button>
        <Link to={`/post/${post.id}`} className={action} aria-label={`Comments, ${post.comment_count}`}>
          <span className={round}><MessageCircle size={20} /></span>
          <span className="text-xs">{post.comment_count || ''}</span>
        </Link>
        <button onClick={() => post.visibility === 'public' && setShowRepost(true)}
          disabled={post.visibility !== 'public'} className={`${action} disabled:opacity-40`}
          aria-label="Repost" title={post.visibility !== 'public' ? 'Friends-only posts cannot be reposted' : 'Repost'}>
          <span className={round}><Repeat2 size={20} /></span>
          <span className="text-xs">{post.repost_count || ''}</span>
        </button>
        <button onClick={() => toggleBookmark.mutate(!bookmarked)} className={action}
          aria-pressed={bookmarked} aria-label={bookmarked ? 'Remove bookmark' : 'Bookmark'}>
          <span className={round}><Bookmark size={20} fill={bookmarked ? 'currentColor' : 'none'} /></span>
        </button>
        {shareable && (
          <button onClick={share} className={action} aria-label="Share">
            <span className={round}><Share size={20} /></span>
          </button>
        )}
      </div>

      {/* Bottom left: who and what */}
      <div className="absolute left-3 right-20 bottom-5 z-20 pointer-events-none">
        <Link to={`/profile/${post.author_username}`} className="inline-flex items-center gap-2 pointer-events-auto max-w-full">
          {post.author_avatar_url
            ? <img src={post.author_avatar_url} alt="" className="w-9 h-9 rounded-full object-cover flex-shrink-0" />
            : <span className="w-9 h-9 rounded-full bg-agora-700 flex items-center justify-center font-semibold flex-shrink-0">{name[0]?.toUpperCase()}</span>}
          <span className="min-w-0">
            <span className="block text-sm font-semibold truncate">{renderName(name, post.author_emojis)}</span>
            <span className="block text-xs text-white/70 truncate">
              {handle(post.author_username, post.is_remote, post.remote_instance)}
              {' · '}{formatDistanceToNow(new Date(post.published_at || post.created_at), { addSuffix: true })}
            </span>
          </span>
        </Link>
        {network && (
          <span className="ml-2 align-middle text-[10px] font-medium px-1.5 py-0.5 rounded-full bg-white/20">{network}</span>
        )}
        {post.content && (
          <div className="mt-2 pointer-events-auto">
            <p onClick={() => setCaptionOpen((o) => !o)}
              className={`text-sm whitespace-pre-wrap break-words cursor-pointer ${captionOpen ? 'max-h-48 overflow-y-auto' : 'line-clamp-3'}`}>
              {renderContent(post.content, 'underline break-all', post.content_emojis, post.mention_domains)}
            </p>
          </div>
        )}
      </div>

      {/* Progress */}
      {scrubRatio !== null && (videoRef.current?.duration ?? 0) > 0 && (
        <div className="absolute inset-x-0 bottom-14 z-20 flex justify-center pointer-events-none">
          <span className="bg-black/60 text-sm font-semibold px-2.5 py-1 rounded-lg">
            {clock(scrubRatio * (videoRef.current?.duration ?? 0))} / {clock(videoRef.current?.duration ?? 0)}
          </span>
        </div>
      )}
      {near && cwOk && !failed && post.video_url && (
        <div ref={barRef} role="slider" aria-label="Seek" aria-valuemin={0} aria-valuemax={100}
          aria-valuenow={Math.round((scrubRatio ?? progress) * 100)} tabIndex={-1}
          onPointerDown={onBarDown} onPointerMove={onBarMove} onPointerUp={onBarUp} onPointerCancel={onBarUp}
          onClick={(e) => e.stopPropagation()}
          className="group absolute inset-x-0 bottom-3 h-6 z-20 cursor-pointer touch-none flex items-end">
          <div className={`relative w-full bg-white/20 transition-[height] ${scrubRatio !== null ? 'h-1.5' : 'h-0.5 group-hover:h-1.5'}`}>
            <div className="h-full bg-white/85" style={{ width: `${Math.min(100, (scrubRatio ?? progress) * 100)}%` }} />
            <div className={`absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-3.5 h-3.5 rounded-full bg-white shadow ${scrubRatio !== null ? '' : 'opacity-0 group-hover:opacity-100'}`}
              style={{ left: `${Math.min(100, (scrubRatio ?? progress) * 100)}%` }} />
          </div>
        </div>
      )}

      {toast && (
        <div role="status" className="absolute top-28 left-1/2 -translate-x-1/2 z-30 bg-black/70 text-sm px-3 py-1.5 rounded-full">{toast}</div>
      )}

      {showReport && (
        <div data-reel-dialog><ReportModal postId={post.id} onClose={() => setShowReport(false)} /></div>
      )}

      {showRepost && (
        <div data-reel-dialog className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60"
          onClick={() => setShowRepost(false)}>
          <div className="w-full max-w-sm bg-white dark:bg-agora-800 text-agora-800 dark:text-agora-100 rounded-xl p-4 space-y-3"
            role="dialog" aria-label="Repost" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between">
              <h2 className="font-semibold">Repost</h2>
              <button onClick={() => setShowRepost(false)} aria-label="Close"><X size={18} /></button>
            </div>
            <textarea value={repostText} onChange={(e) => setRepostText(e.target.value)} rows={3}
              placeholder="Add a comment (optional)" className="input w-full resize-none text-sm" />
            <label className="flex items-center gap-2 text-sm">
              Who can see it
              <select value={repostVisibility} onChange={(e) => setRepostVisibility(e.target.value as 'public' | 'friends')}
                className="input text-sm py-1">
                <option value="friends">Friends</option>
                <option value="public">Public</option>
              </select>
            </label>
            <button onClick={() => repost.mutate()} disabled={repost.isPending} className="btn-primary w-full">
              {repost.isPending ? 'Reposting…' : 'Repost'}
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
