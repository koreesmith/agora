import { useEffect, useRef, useState } from 'react'
import { Circle, Square, Camera } from 'lucide-react'

// AGORA-441: record a reel with the camera. Offered only where the browser can
// (getUserMedia and MediaRecorder); elsewhere the picker just uploads a file.
// Stops by itself at the length limit.

export const canRecord = () =>
  typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof MediaRecorder !== 'undefined'

const MAX_SECONDS = 120

function pickMimeType(): string {
  // Safari records MP4; Chrome and Firefox record WebM.
  for (const t of ['video/mp4', 'video/webm;codecs=vp9,opus', 'video/webm;codecs=vp8,opus', 'video/webm']) {
    if (MediaRecorder.isTypeSupported(t)) return t
  }
  return ''
}

export function recordingFileName(mime: string): string {
  return mime.includes('mp4') ? 'reel.mp4' : 'reel.webm'
}

export default function RecordStep({ onRecorded, onCancel }: { onRecorded: (file: File) => void; onCancel: () => void }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const recorderRef = useRef<MediaRecorder | null>(null)
  const chunksRef = useRef<Blob[]>([])
  const tickRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const [ready, setReady] = useState(false)
  const [recording, setRecording] = useState(false)
  const [seconds, setSeconds] = useState(0)
  const [error, setError] = useState('')

  const stopStream = () => {
    streamRef.current?.getTracks().forEach(t => t.stop())
    streamRef.current = null
  }

  useEffect(() => {
    let cancelled = false
    navigator.mediaDevices.getUserMedia({ video: { facingMode: 'user', width: { ideal: 720 }, height: { ideal: 1280 } }, audio: true })
      .then(stream => {
        if (cancelled) { stream.getTracks().forEach(t => t.stop()); return }
        streamRef.current = stream
        if (videoRef.current) videoRef.current.srcObject = stream
        setReady(true)
      })
      .catch((e: any) => {
        setError(e?.name === 'NotAllowedError'
          ? 'Camera access was blocked. Allow it in your browser\'s site settings to record, or upload a file instead.'
          : 'Couldn\'t start the camera. You can upload a file instead.')
      })
    return () => {
      cancelled = true
      if (tickRef.current) clearInterval(tickRef.current)
      if (recorderRef.current && recorderRef.current.state !== 'inactive') {
        recorderRef.current.onstop = null
        recorderRef.current.stop()
      }
      stopStream()
    }
  }, [])

  const stop = () => {
    if (tickRef.current) { clearInterval(tickRef.current); tickRef.current = null }
    if (recorderRef.current && recorderRef.current.state !== 'inactive') recorderRef.current.stop()
  }

  const start = () => {
    if (!streamRef.current) return
    const mime = pickMimeType()
    const rec = new MediaRecorder(streamRef.current, mime ? { mimeType: mime } : undefined)
    chunksRef.current = []
    rec.ondataavailable = e => { if (e.data.size > 0) chunksRef.current.push(e.data) }
    rec.onstop = () => {
      setRecording(false)
      const type = rec.mimeType || mime || 'video/webm'
      const blob = new Blob(chunksRef.current, { type })
      stopStream()
      if (blob.size > 0) onRecorded(new File([blob], recordingFileName(type), { type }))
      else setError('Nothing was recorded. Try again.')
    }
    rec.start(1000)
    recorderRef.current = rec
    setSeconds(0)
    setRecording(true)
    const began = Date.now()
    tickRef.current = setInterval(() => {
      const s = Math.floor((Date.now() - began) / 1000)
      setSeconds(s)
      if (s >= MAX_SECONDS) stop()
    }, 250)
  }

  const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`

  return (
    <div className="space-y-3">
      <div className="relative bg-black rounded-xl overflow-hidden aspect-[9/16] max-h-[60vh] mx-auto">
        <video ref={videoRef} autoPlay muted playsInline className="w-full h-full object-cover -scale-x-100" />
        {!ready && !error && (
          <div className="absolute inset-0 flex items-center justify-center text-white/70 text-sm" role="status">Starting camera…</div>
        )}
        {recording && (
          <div className="absolute top-3 left-3 flex items-center gap-1.5 bg-black/60 text-white text-xs rounded-full px-2.5 py-1" role="timer" aria-label="Recording time">
            <Circle size={10} className="text-red-500" fill="currentColor" /> {mmss(seconds)} / {mmss(MAX_SECONDS)}
          </div>
        )}
      </div>
      {error && <p className="text-sm text-red-500" role="alert">{error}</p>}
      <div className="flex items-center justify-center gap-3">
        {!recording ? (
          <button type="button" onClick={start} disabled={!ready}
            className="btn-primary text-sm flex items-center gap-1.5 disabled:opacity-50">
            <Camera size={16} /> Start recording
          </button>
        ) : (
          <button type="button" onClick={stop}
            className="bg-red-600 hover:bg-red-700 text-white text-sm font-medium rounded-lg px-4 py-2 flex items-center gap-1.5">
            <Square size={14} fill="currentColor" /> Stop
          </button>
        )}
        <button type="button" onClick={onCancel} disabled={recording} className="btn-ghost text-sm disabled:opacity-50">Back</button>
      </div>
    </div>
  )
}
