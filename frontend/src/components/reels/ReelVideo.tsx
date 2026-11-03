import { forwardRef, useEffect, useRef } from 'react'

// AGORA-440: the <video> behind a reel. Agora's own uploads and most Fediverse
// videos are plain files, but a Bluesky video arrives as an HLS playlist
// (.m3u8). Safari plays HLS natively; every other browser needs hls.js, which
// is loaded only when a playlist actually shows up, so it never weighs on the
// main bundle. Tearing the player down on unmount (src cleared, hls.js
// destroyed) is what keeps memory flat through a long session.

export const isHlsUrl = (url: string) => /\.m3u8(\?|#|$)/i.test(url)

interface Props {
  src: string
  poster?: string
  muted: boolean
  preload: 'auto' | 'metadata' | 'none'
  className?: string
  onError: () => void
  onPlay: () => void
  onPause: () => void
  onTime: (current: number, duration: number) => void
  onReady: () => void
  onLoaded: () => void
}

const ReelVideo = forwardRef<HTMLVideoElement, Props>(function ReelVideo(
  { src, poster, muted, preload, className, onError, onPlay, onPause, onTime, onReady, onLoaded }, outerRef,
) {
  const inner = useRef<HTMLVideoElement | null>(null)
  const setRef = (el: HTMLVideoElement | null) => {
    inner.current = el
    if (typeof outerRef === 'function') outerRef(el)
    else if (outerRef) outerRef.current = el
  }
  // Latest callbacks without re-running the source effect when they change.
  const onErrorRef = useRef(onError)
  onErrorRef.current = onError

  useEffect(() => {
    const video = inner.current
    if (!video) return
    let cancelled = false
    let hls: { destroy: () => void } | null = null

    if (isHlsUrl(src) && !video.canPlayType('application/vnd.apple.mpegurl')) {
      import('hls.js').then(({ default: Hls }) => {
        if (cancelled) return
        if (!Hls.isSupported()) { onErrorRef.current(); return }
        const player = new Hls({ maxBufferLength: 8, maxMaxBufferLength: 15 })
        hls = player
        player.on(Hls.Events.ERROR, (_e, data) => { if (data.fatal) onErrorRef.current() })
        player.loadSource(src)
        player.attachMedia(video)
      }).catch(() => { if (!cancelled) onErrorRef.current() })
    } else {
      video.src = src
    }

    return () => {
      cancelled = true
      hls?.destroy()
      video.removeAttribute('src')
      video.load()
    }
  }, [src])

  useEffect(() => { if (inner.current) inner.current.muted = muted }, [muted])

  return (
    <video
      ref={setRef}
      poster={poster}
      preload={preload}
      muted={muted}
      loop
      playsInline
      className={className}
      onError={() => onError()}
      onPlay={onPlay}
      onPause={onPause}
      onPlaying={onReady}
      onLoadedData={onLoaded}
      onTimeUpdate={(e) => onTime(e.currentTarget.currentTime, e.currentTarget.duration)}
    />
  )
})

export default ReelVideo
