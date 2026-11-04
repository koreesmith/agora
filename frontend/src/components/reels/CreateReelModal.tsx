import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Camera, Pause, Play, Upload, Volume2, VolumeX, X } from 'lucide-react'
import { authApi, feedApi, friendsApi, instanceApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import MentionTextarea from '../feed/MentionTextarea'
import RecordStep, { canRecord } from './RecordStep'

// AGORA-441: make a reel. Pick a video (or record one), trim it and choose its
// cover frame, then add a caption and post. The trim and cover are sent with
// the upload (AGORA-439), so the server cuts the clip during its one
// transcode. The result is an ordinary video post.

// The longest reel is the instance's own limit (AGORA-444), 2 minutes unless an
// admin lowered it; it comes from /instance and this is the fallback.
const DEFAULT_MAX_SECONDS = 120
const MIN_CLIP = 0.5

type Step = 'pick' | 'record' | 'edit' | 'post'
type Phase = 'idle' | 'uploading' | 'processing' | 'posting'

// "2 minutes", "90 seconds": a limit the way a person would say it.
const limitLabel = (sec: number) => sec >= 60 && sec % 60 === 0 ? `${sec / 60} minute${sec === 60 ? '' : 's'}` : `${sec} seconds`

const fmt = (t: number) => {
  const m = Math.floor(t / 60)
  const s = t - m * 60
  return `${m}:${s.toFixed(1).padStart(4, '0')}`
}

function NetworkChip({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} aria-pressed={active}
      className={`px-2 py-0.5 rounded-full text-xs font-medium border transition-colors ${
        active
          ? 'bg-agora-100 dark:bg-agora-700 border-agora-400 text-agora-700 dark:text-agora-200'
          : 'border-agora-200 dark:border-agora-600 text-agora-400 hover:border-agora-400'}`}>
      {label}
    </button>
  )
}

export default function CreateReelModal({ onClose, onPosted }: { onClose: () => void; onPosted: () => void }) {
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const dialogRef = useRef<HTMLDivElement>(null)

  const [step, setStep] = useState<Step>('pick')
  const [file, setFile] = useState<File | null>(null)
  const [src, setSrc] = useState('')
  const [pickError, setPickError] = useState('')

  // Edit
  const [duration, setDuration] = useState(0)
  const [canPreview, setCanPreview] = useState(true)
  const [start, setStart] = useState(0)
  const [end, setEnd] = useState(0)
  const [coverAt, setCoverAt] = useState(1)
  const [playing, setPlaying] = useState(true)
  const [soundOn, setSoundOn] = useState(false)
  const previewRef = useRef<HTMLVideoElement>(null)
  const coverRef = useRef<HTMLVideoElement>(null)

  // Post
  const [caption, setCaption] = useState('')
  const [twEnabled, setTwEnabled] = useState(false)
  const [twLabel, setTwLabel] = useState('')
  const [visibility, setVisibility] = useState('friends')
  const [friendListId, setFriendListId] = useState('')
  const [agoraEnabled, setAgoraEnabled] = useState(true)
  const [federateAP, setFederateAP] = useState(true)
  const [federateATProto, setFederateATProto] = useState(true)
  const [phase, setPhase] = useState<Phase>('idle')
  const [progress, setProgress] = useState(0)
  const [error, setError] = useState('')
  const abortRef = useRef<AbortController | null>(null)
  const cancelledRef = useRef(false)
  const [confirmClose, setConfirmClose] = useState(false)

  const { data: info } = useQuery({ queryKey: ['instance-info'], queryFn: () => instanceApi.getInfo().then(r => r.data), staleTime: 5 * 60_000 })
  const MAX_SECONDS: number = info?.video_max_seconds ?? DEFAULT_MAX_SECONDS
  const { data: listsData } = useQuery({ queryKey: ['friend-lists'], queryFn: () => friendsApi.listFriendLists().then(r => r.data) })
  const friendLists: any[] = listsData?.groups || []
  const { data: freshProfile } = useQuery({ queryKey: ['my-profile'], queryFn: () => authApi.me().then(r => r.data), staleTime: 60_000 })
  const apEnabled = freshProfile?.activitypub_enabled ?? user?.activitypub_enabled
  const atprotoEnabled = freshProfile?.atproto_enabled ?? user?.atproto_enabled

  const busy = phase !== 'idle'
  const dirty = !!file

  // ── Closing ───────────────────────────────────────────────────────────────
  const requestClose = () => { if (dirty || busy) setConfirmClose(true); else onClose() }
  const discard = () => {
    cancelledRef.current = true
    abortRef.current?.abort()
    onClose()
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); requestClose() } }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
  useEffect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    dialogRef.current?.focus()
    return () => { document.body.style.overflow = prev; cancelledRef.current = true; abortRef.current?.abort() }
  }, [])

  // ── Picking ───────────────────────────────────────────────────────────────
  useEffect(() => () => { if (src) URL.revokeObjectURL(src) }, [src])

  const chooseFile = (f: File | null | undefined) => {
    if (!f) return
    if (!f.type.startsWith('video/') && !/\.(mp4|mov|m4v|webm|mkv|avi)$/i.test(f.name)) {
      setPickError('That doesn\'t look like a video. Choose an MP4, MOV, WebM, MKV or AVI file.')
      return
    }
    setPickError('')
    setFile(f)
    setSrc(URL.createObjectURL(f))
    setDuration(0); setStart(0); setEnd(0); setCoverAt(1); setCanPreview(true); setPlaying(true)
    setStep('edit')
  }

  const backToPick = () => { setFile(null); setSrc(''); setStep('pick'); setError('') }

  // ── Editing ───────────────────────────────────────────────────────────────
  const onMetadata = () => {
    const v = previewRef.current
    if (!v) return
    const settle = (d: number) => {
      setDuration(d)
      setStart(0)
      setEnd(Math.min(d, MAX_SECONDS))
      setCoverAt(Math.min(1, Math.min(d, MAX_SECONDS) / 2))
      v.currentTime = 0
    }
    if (Number.isFinite(v.duration)) { settle(v.duration); return }
    // A recording made in the browser often reports an infinite length until
    // it has been played to the end; seeking far ahead makes it learn it.
    v.currentTime = 1e101
    v.ontimeupdate = () => {
      v.ontimeupdate = null
      if (Number.isFinite(v.duration)) settle(v.duration)
      else setCanPreview(false)
    }
  }

  const clipLen = Math.max(0, end - start)
  const onStart = (value: number) => {
    let s = Math.min(value, end - MIN_CLIP)
    let e = end
    if (e - s > MAX_SECONDS) e = s + MAX_SECONDS
    setStart(Math.max(0, s)); setEnd(e)
    setCoverAt(c => Math.min(c, Math.max(0, e - s)))
    if (previewRef.current) previewRef.current.currentTime = Math.max(0, s)
  }
  const onEnd = (value: number) => {
    let e = Math.max(value, start + MIN_CLIP)
    let s = start
    if (e - s > MAX_SECONDS) s = e - MAX_SECONDS
    setEnd(Math.min(duration, e)); setStart(s)
    setCoverAt(c => Math.min(c, Math.max(0, e - s)))
  }

  // Keep the preview inside the kept range.
  const onPreviewTime = () => {
    const v = previewRef.current
    if (!v || !canPreview || !duration) return
    if (v.currentTime >= end || v.currentTime < start - 0.25) v.currentTime = start
  }
  useEffect(() => {
    const v = previewRef.current
    if (!v) return
    v.muted = !soundOn
    if (playing) v.play().catch(() => {}); else v.pause()
  }, [playing, soundOn, src, duration])
  useEffect(() => {
    const c = coverRef.current
    if (c && duration) c.currentTime = Math.min(duration, start + coverAt)
  }, [coverAt, start, duration])

  const trimmed = duration > 0 && (start > 0.05 || end < duration - 0.25)

  // ── Posting ───────────────────────────────────────────────────────────────
  const validTargeting = visibility !== 'public' || agoraEnabled || federateAP || federateATProto
  const canPost = !!file && !busy && validTargeting && (visibility !== 'group' || !!friendListId) && (!canPreview || clipLen >= MIN_CLIP)

  const post = useMutation({
    mutationFn: async () => {
      if (!file) return
      setError(''); cancelledRef.current = false
      const ac = new AbortController(); abortRef.current = ac

      setPhase('uploading'); setProgress(0)
      const up = await feedApi.uploadVideo(file, {
        filename: file.name,
        // Only sent when they change something, so an untouched upload is
        // exactly what the server did before trimming existed.
        ...(canPreview && trimmed ? { trimStartMs: start * 1000, trimEndMs: end * 1000 } : {}),
        ...(canPreview && duration ? { coverMs: coverAt * 1000 } : {}),
      }, setProgress, ac.signal)
      const jobId: string = up.data.job_id

      setPhase('processing')
      const deadline = Date.now() + 16 * 60_000   // a little over the server's own limit
      let url = '', thumb = ''
      for (;;) {
        if (cancelledRef.current) return
        if (Date.now() > deadline) throw new Error('Processing is taking too long. Please try again.')
        await new Promise(r => setTimeout(r, 1500))
        if (cancelledRef.current) return
        const { data } = await feedApi.getVideoJob(jobId)
        if (data.status === 'done') { url = data.url; thumb = data.thumb_url || ''; break }
        if (data.status === 'failed') throw new Error(data.error || 'Video processing failed. Please try a different file.')
      }

      setPhase('posting')
      const usingDefaultTargeting = agoraEnabled && federateAP && federateATProto
      const targeting = visibility === 'public' && !usingDefaultTargeting
        ? { federate_activitypub: federateAP, federate_atproto: federateATProto, external_only: !agoraEnabled }
        : {}
      await feedApi.createPost({
        content: caption,
        video_url: url,
        video_thumb_url: thumb || undefined,
        visibility,
        group_id: visibility === 'group' ? friendListId : undefined,
        content_warning: twEnabled && twLabel.trim() ? twLabel.trim() : '',
        ...targeting,
      })
      return true
    },
    onSuccess: (ok) => {
      if (!ok) return
      for (const key of ['feed', 'reels', 'user-posts']) qc.invalidateQueries({ queryKey: [key] })
      onPosted()
    },
    onError: (e: any) => {
      if (cancelledRef.current || e?.code === 'ERR_CANCELED') return
      setPhase('idle')
      setError(e?.response?.data?.error || e?.message || 'Could not post your reel. Please try again.')
    },
  })

  const phaseLabel = useMemo(() => {
    if (phase === 'uploading') return `Uploading… ${Math.round(progress * 100)}%`
    if (phase === 'processing') return 'Processing your video… this can take a minute.'
    if (phase === 'posting') return 'Posting…'
    return ''
  }, [phase, progress])

  const inputCls = 'text-xs bg-transparent text-agora-600 dark:text-agora-300 border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1.5 focus:outline-none'
  const pct = (t: number) => (duration > 0 ? `${(t / duration) * 100}%` : '0%')

  return (
    <div data-reel-dialog className="fixed inset-0 z-50 flex items-end sm:items-center justify-center bg-black/60 sm:p-4" onClick={requestClose}>
      <div ref={dialogRef} tabIndex={-1} role="dialog" aria-modal="true" aria-label="Make a reel"
        className="relative w-full sm:max-w-lg max-h-[100dvh] sm:max-h-[92vh] overflow-y-auto bg-white dark:bg-agora-900 text-agora-800 dark:text-agora-100 sm:rounded-2xl shadow-xl outline-none"
        onClick={e => e.stopPropagation()}>
        <div className="sticky top-0 z-10 flex items-center justify-between px-4 py-3 bg-white dark:bg-agora-900 border-b border-agora-100 dark:border-agora-700">
          <h2 className="font-semibold">
            {step === 'pick' ? 'Make a reel' : step === 'record' ? 'Record' : step === 'edit' ? 'Trim and pick a cover' : 'Add details'}
          </h2>
          <button onClick={requestClose} aria-label="Close" className="text-agora-400 hover:text-agora-700"><X size={20} /></button>
        </div>

        <div className="p-4 space-y-4">
          {confirmClose && (
            <div role="alertdialog" aria-label="Discard this reel?" className="rounded-xl border border-amber-300 bg-amber-50 dark:bg-amber-900/20 dark:border-amber-700 p-3 space-y-2">
              <p className="text-sm font-medium">{busy ? 'Cancel posting this reel?' : 'Discard this reel?'}</p>
              <p className="text-xs text-agora-500">{busy ? 'Closing now stops the upload.' : 'Your video and the changes you made will be lost.'}</p>
              <div className="flex gap-2">
                <button onClick={discard} className="bg-red-600 hover:bg-red-700 text-white text-sm font-medium rounded-lg px-3 py-1.5">{busy ? 'Cancel upload' : 'Discard'}</button>
                <button onClick={() => setConfirmClose(false)} className="btn-ghost text-sm">Keep editing</button>
              </div>
            </div>
          )}

          {/* ── Pick ─────────────────────────────────────────────────────── */}
          {step === 'pick' && (
            <div className="space-y-3">
              <p className="text-sm text-agora-500">Reels are short videos, up to {limitLabel(MAX_SECONDS)}. You can trim a longer one on the next step.</p>
              <label className="flex items-center gap-3 rounded-xl border-2 border-dashed border-agora-300 dark:border-agora-600 hover:border-agora-500 p-5 cursor-pointer">
                <Upload size={22} className="text-agora-500 flex-shrink-0" />
                <span className="text-sm"><span className="font-medium">Choose a video</span><br /><span className="text-agora-400">MP4, MOV, WebM, MKV or AVI, up to 2 GB</span></span>
                <input type="file" accept="video/*" className="hidden" onChange={e => chooseFile(e.target.files?.[0])} />
              </label>
              {canRecord() && (
                <button type="button" onClick={() => setStep('record')}
                  className="w-full flex items-center gap-3 rounded-xl border border-agora-200 dark:border-agora-700 hover:border-agora-400 p-4 text-left">
                  <Camera size={22} className="text-agora-500 flex-shrink-0" />
                  <span className="text-sm"><span className="font-medium">Record with your camera</span><br /><span className="text-agora-400">Your browser will ask to use the camera and microphone</span></span>
                </button>
              )}
              {pickError && <p className="text-sm text-red-500" role="alert">{pickError}</p>}
            </div>
          )}

          {/* ── Record ───────────────────────────────────────────────────── */}
          {step === 'record' && (
            <RecordStep maxSeconds={MAX_SECONDS} onCancel={() => setStep('pick')} onRecorded={chooseFile} />
          )}

          {/* ── Edit ─────────────────────────────────────────────────────── */}
          {step === 'edit' && src && (
            <div className="space-y-4">
              <div className="relative bg-black rounded-xl overflow-hidden">
                <video ref={previewRef} src={src} muted playsInline loop={false}
                  onLoadedMetadata={onMetadata} onTimeUpdate={onPreviewTime}
                  onError={() => setCanPreview(false)}
                  className="w-full max-h-[46vh] object-contain mx-auto" />
                {canPreview && duration > 0 && (
                  <div className="absolute bottom-2 right-2 flex gap-2">
                    <button type="button" onClick={() => setPlaying(p => !p)} aria-label={playing ? 'Pause preview' : 'Play preview'}
                      className="w-9 h-9 rounded-full bg-black/55 text-white flex items-center justify-center">
                      {playing ? <Pause size={16} /> : <Play size={16} />}
                    </button>
                    <button type="button" onClick={() => setSoundOn(s => !s)} aria-label={soundOn ? 'Mute preview' : 'Unmute preview'}
                      className="w-9 h-9 rounded-full bg-black/55 text-white flex items-center justify-center">
                      {soundOn ? <Volume2 size={16} /> : <VolumeX size={16} />}
                    </button>
                  </div>
                )}
              </div>

              {!canPreview && (
                <div className="rounded-xl border border-amber-300 bg-amber-50 dark:bg-amber-900/20 dark:border-amber-700 p-3 text-sm flex gap-2">
                  <AlertTriangle size={16} className="text-amber-500 flex-shrink-0 mt-0.5" />
                  <span>Your browser can't preview this file, so you can't trim it here. You can still post it as it is, if it's within the {limitLabel(MAX_SECONDS)} limit.</span>
                </div>
              )}

              {canPreview && duration > 0 && (
                <>
                  <fieldset className="space-y-2">
                    <legend className="text-sm font-medium flex items-center justify-between w-full">
                      <span>Trim</span>
                      <span className="text-xs text-agora-500" aria-live="polite">Clip length {fmt(clipLen)}</span>
                    </legend>
                    <div className="relative h-2 rounded-full bg-agora-100 dark:bg-agora-700" aria-hidden>
                      <div className="absolute top-0 bottom-0 rounded-full bg-agora-600" style={{ left: pct(start), width: `${duration > 0 ? (clipLen / duration) * 100 : 0}%` }} />
                    </div>
                    <label className="flex items-center gap-3 text-xs">
                      <span className="w-10 text-agora-500">Start</span>
                      <input type="range" min={0} max={duration} step={0.1} value={start} onChange={e => onStart(Number(e.target.value))} className="flex-1" aria-label="Trim start" />
                      <span className="w-14 text-right tabular-nums">{fmt(start)}</span>
                    </label>
                    <label className="flex items-center gap-3 text-xs">
                      <span className="w-10 text-agora-500">End</span>
                      <input type="range" min={0} max={duration} step={0.1} value={end} onChange={e => onEnd(Number(e.target.value))} className="flex-1" aria-label="Trim end" />
                      <span className="w-14 text-right tabular-nums">{fmt(end)}</span>
                    </label>
                    {duration > MAX_SECONDS && (
                      <p className="text-xs text-agora-500">This video is {fmt(duration)} long. Reels are up to {limitLabel(MAX_SECONDS)}, so the first {limitLabel(MAX_SECONDS)} are selected. Move the handles to choose another part.</p>
                    )}
                  </fieldset>

                  <fieldset className="space-y-2">
                    <legend className="text-sm font-medium">Cover</legend>
                    <div className="flex items-center gap-3">
                      <div className="w-20 aspect-[9/16] bg-black rounded-lg overflow-hidden flex-shrink-0">
                        <video ref={coverRef} src={src} muted playsInline preload="auto" className="w-full h-full object-cover" aria-label="Cover preview" />
                      </div>
                      <label className="flex-1 text-xs space-y-1 block">
                        <span className="text-agora-500">Drag to choose the frame that represents your reel</span>
                        <input type="range" min={0} max={Math.max(0.1, clipLen)} step={0.1} value={Math.min(coverAt, clipLen)} onChange={e => setCoverAt(Number(e.target.value))} className="w-full" aria-label="Cover frame" />
                        <span className="tabular-nums text-agora-500">{fmt(Math.min(coverAt, clipLen))} into the clip</span>
                      </label>
                    </div>
                  </fieldset>
                </>
              )}

              <div className="flex items-center justify-between pt-1">
                <button onClick={backToPick} className="btn-ghost text-sm">Choose a different video</button>
                <button onClick={() => setStep('post')} disabled={canPreview && duration === 0} className="btn-primary text-sm disabled:opacity-50">Next</button>
              </div>
            </div>
          )}

          {/* ── Post ─────────────────────────────────────────────────────── */}
          {step === 'post' && (
            <div className="space-y-4">
              <MentionTextarea value={caption} onChange={setCaption} rows={3} autoFocus
                placeholder="Write a caption…  Use @ to mention someone and + to tag a group"
                className="input w-full resize-none text-sm" />

              <div className="space-y-2">
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={twEnabled} onChange={e => setTwEnabled(e.target.checked)} />
                  Add a content warning
                </label>
                {twEnabled && (
                  <input value={twLabel} onChange={e => setTwLabel(e.target.value)} maxLength={100}
                    placeholder="What should people know before watching?" className="input w-full text-sm" aria-label="Content warning" />
                )}
              </div>

              <div className="flex items-center gap-2 flex-wrap">
                <label className="text-xs text-agora-500" htmlFor="reel-audience">Who can see it</label>
                <select id="reel-audience" value={visibility} className={inputCls} onChange={e => {
                  const v = e.target.value
                  setVisibility(v)
                  // The network choice only applies to Public; reset it going
                  // into or out of that so an earlier choice can't linger.
                  if (v !== 'public') { setAgoraEnabled(true); setFederateAP(true); setFederateATProto(true) }
                }}>
                  <option value="public">Public</option>
                  <option value="friends">Friends</option>
                  <option value="group">Friend List</option>
                </select>
                {visibility === 'group' && (
                  friendLists.length > 0
                    ? <select value={friendListId} onChange={e => setFriendListId(e.target.value)} className={inputCls} aria-label="Friend list">
                        <option value="">Select list…</option>
                        {friendLists.map((g: any) => <option key={g.id} value={g.id}>{g.name}</option>)}
                      </select>
                    : <span className="text-xs text-agora-400">No lists yet. <Link to="/connections?tab=lists" className="underline">Create one</Link></span>
                )}
              </div>

              {visibility === 'public' && (apEnabled || atprotoEnabled) && (
                <div className="flex items-center gap-1.5 flex-wrap">
                  <span className="text-xs text-agora-400">Post to:</span>
                  <NetworkChip label="Agora" active={agoraEnabled} onClick={() => setAgoraEnabled(v => !v)} />
                  {apEnabled && <NetworkChip label="Fediverse" active={federateAP} onClick={() => setFederateAP(v => !v)} />}
                  {atprotoEnabled && <NetworkChip label="Bluesky" active={federateATProto} onClick={() => setFederateATProto(v => !v)} />}
                </div>
              )}
              {visibility === 'public' && !validTargeting && <p className="text-xs text-red-500">Pick at least one place for this reel to go.</p>}

              {error && <p className="text-sm text-red-500" role="alert">{error}</p>}

              {busy && (
                <div className="space-y-1.5" role="status">
                  <div className="h-1.5 rounded-full bg-agora-100 dark:bg-agora-700 overflow-hidden">
                    <div className={`h-full bg-agora-600 ${phase === 'uploading' ? '' : 'animate-pulse'}`} style={{ width: phase === 'uploading' ? `${Math.round(progress * 100)}%` : '100%' }} />
                  </div>
                  <p className="text-xs text-agora-500">{phaseLabel}</p>
                </div>
              )}

              <div className="flex items-center justify-between pt-1">
                <button onClick={() => setStep('edit')} disabled={busy} className="btn-ghost text-sm disabled:opacity-50">Back</button>
                <button onClick={() => post.mutate()} disabled={!canPost} className="btn-primary text-sm disabled:opacity-50">
                  {busy ? 'Posting…' : 'Post reel'}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
