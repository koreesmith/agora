import { useState, useRef, useCallback } from 'react'
import { Camera, X, Check, Move } from 'lucide-react'

interface CoverPhotoProps {
  src: string
  position?: string       // CSS object-position, e.g. "50% 30%"
  height?: string         // Tailwind height class, e.g. "h-48"
  editable?: boolean      // Show reposition controls
  onUpload?: (e: React.ChangeEvent<HTMLInputElement>) => void
  onPositionSave?: (position: string) => void
  clickable?: boolean     // Click to enlarge
  fallback?: React.ReactNode
}

export default function CoverPhoto({
  src,
  position = '50% 50%',
  height = 'h-48',
  editable = false,
  onUpload,
  onPositionSave,
  clickable = true,
  fallback,
}: CoverPhotoProps) {
  const [lightbox, setLightbox] = useState(false)
  const [repositioning, setRepositioning] = useState(false)
  const [draftPosition, setDraftPosition] = useState(position)
  const [isDragging, setIsDragging] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const dragStart = useRef<{ y: number; posY: number } | null>(null)

  const parseY = (pos: string) => {
    const parts = pos.split(' ')
    return parseFloat(parts[1] ?? '50')
  }

  const startReposition = () => {
    setDraftPosition(position)
    setRepositioning(true)
  }

  const cancelReposition = () => {
    setDraftPosition(position)
    setRepositioning(false)
  }

  const saveReposition = () => {
    onPositionSave?.(draftPosition)
    setRepositioning(false)
  }

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    if (!repositioning) return
    e.preventDefault()
    dragStart.current = { y: e.clientY, posY: parseY(draftPosition) }
    setIsDragging(true)
  }, [repositioning, draftPosition])

  const onMouseMove = useCallback((e: React.MouseEvent) => {
    if (!isDragging || !dragStart.current || !containerRef.current) return
    const containerH = containerRef.current.getBoundingClientRect().height
    const deltaY = e.clientY - dragStart.current.y
    // Convert pixel delta to percentage (drag down = show more top = lower % value)
    const deltaPct = (deltaY / containerH) * 100
    const newY = Math.max(0, Math.min(100, dragStart.current.posY - deltaPct))
    setDraftPosition(`50% ${newY.toFixed(1)}%`)
  }, [isDragging])

  const onMouseUp = useCallback(() => {
    setIsDragging(false)
    dragStart.current = null
  }, [])

  // Touch support
  const onTouchStart = useCallback((e: React.TouchEvent) => {
    if (!repositioning) return
    const touch = e.touches[0]
    dragStart.current = { y: touch.clientY, posY: parseY(draftPosition) }
    setIsDragging(true)
  }, [repositioning, draftPosition])

  const onTouchMove = useCallback((e: React.TouchEvent) => {
    if (!isDragging || !dragStart.current || !containerRef.current) return
    e.preventDefault()
    const touch = e.touches[0]
    const containerH = containerRef.current.getBoundingClientRect().height
    const deltaY = touch.clientY - dragStart.current.y
    const deltaPct = (deltaY / containerH) * 100
    const newY = Math.max(0, Math.min(100, dragStart.current.posY - deltaPct))
    setDraftPosition(`50% ${newY.toFixed(1)}%`)
  }, [isDragging])

  const currentPosition = repositioning ? draftPosition : position

  return (
    <>
      <div
        ref={containerRef}
        className={`relative ${height} overflow-hidden select-none`}
        onMouseMove={onMouseMove}
        onMouseUp={onMouseUp}
        onMouseLeave={onMouseUp}
        onTouchMove={onTouchMove}
        onTouchEnd={onMouseUp}
      >
        {src ? (
          <img
            src={src}
            alt=""
            className={`w-full h-full object-cover transition-none ${repositioning ? (isDragging ? 'cursor-grabbing' : 'cursor-grab') : (clickable ? 'cursor-zoom-in' : '')}`}
            style={{ objectPosition: currentPosition }}
            onClick={() => { if (!repositioning && clickable) setLightbox(true) }}
            onMouseDown={onMouseDown}
            onTouchStart={onTouchStart}
            draggable={false}
          />
        ) : (
          <div className="w-full h-full bg-gradient-to-r from-agora-300 to-agora-500 dark:from-agora-700 dark:to-agora-900">
            {fallback}
          </div>
        )}

        {/* Reposition overlay hint */}
        {repositioning && (
          <div className="absolute inset-0 pointer-events-none">
            <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 bg-black/50 text-white text-xs px-3 py-1.5 rounded-full flex items-center gap-1.5">
              <Move size={12} /> Drag to reposition
            </div>
          </div>
        )}

        {/* Edit controls */}
        {editable && !repositioning && (
          <div className="absolute bottom-2 right-2 flex gap-1.5">
            {onUpload && (
              <label className="flex items-center gap-1.5 bg-black/50 hover:bg-black/70 text-white text-xs px-2.5 py-1.5 rounded-lg cursor-pointer transition-colors">
                <Camera size={12} /> Change
                <input type="file" accept="image/*" className="hidden" onChange={onUpload} />
              </label>
            )}
            {src && onPositionSave && (
              <button
                onClick={startReposition}
                className="flex items-center gap-1.5 bg-black/50 hover:bg-black/70 text-white text-xs px-2.5 py-1.5 rounded-lg transition-colors"
              >
                <Move size={12} /> Reposition
              </button>
            )}
          </div>
        )}

        {/* Reposition save/cancel */}
        {repositioning && (
          <div className="absolute bottom-2 right-2 flex gap-1.5">
            <button
              onClick={cancelReposition}
              className="flex items-center gap-1.5 bg-black/50 hover:bg-black/70 text-white text-xs px-2.5 py-1.5 rounded-lg transition-colors"
            >
              <X size={12} /> Cancel
            </button>
            <button
              onClick={saveReposition}
              className="flex items-center gap-1.5 bg-agora-600 hover:bg-agora-700 text-white text-xs px-2.5 py-1.5 rounded-lg transition-colors"
            >
              <Check size={12} /> Save position
            </button>
          </div>
        )}
      </div>

      {/* Lightbox */}
      {lightbox && src && (
        <div
          className="fixed inset-0 z-50 bg-black/90 flex items-center justify-center"
          onClick={() => setLightbox(false)}
        >
          <button
            onClick={() => setLightbox(false)}
            className="absolute top-4 right-4 bg-black/40 text-white rounded-full p-1.5 hover:bg-black/70"
          >
            <X size={20} />
          </button>
          <img
            src={src}
            alt=""
            className="max-w-[90vw] max-h-[90vh] object-contain rounded-lg shadow-2xl"
            onClick={e => e.stopPropagation()}
          />
        </div>
      )}
    </>
  )
}

// ── Avatar lightbox ───────────────────────────────────────────────────────────
// Wraps any avatar with click-to-enlarge behaviour.

interface AvatarProps {
  src?: string
  size?: string   // Tailwind size class e.g. "w-20 h-20"
  fallback?: React.ReactNode
  clickable?: boolean
}

export function Avatar({ src, size = 'w-10 h-10', fallback, clickable = true }: AvatarProps) {
  const [lightbox, setLightbox] = useState(false)

  return (
    <>
      <div
        className={`${size} rounded-full overflow-hidden flex-shrink-0 ${src && clickable ? 'cursor-zoom-in' : ''}`}
        onClick={() => { if (src && clickable) setLightbox(true) }}
      >
        {src
          ? <img src={src} alt="" className="w-full h-full object-cover" />
          : <div className="w-full h-full bg-agora-200 dark:bg-agora-700 flex items-center justify-center">{fallback}</div>}
      </div>

      {lightbox && src && (
        <div
          className="fixed inset-0 z-50 bg-black/90 flex items-center justify-center"
          onClick={() => setLightbox(false)}
        >
          <button
            onClick={() => setLightbox(false)}
            className="absolute top-4 right-4 bg-black/40 text-white rounded-full p-1.5 hover:bg-black/70"
          >
            <X size={20} />
          </button>
          <img
            src={src}
            alt=""
            className="max-w-[80vw] max-h-[80vh] object-contain rounded-full shadow-2xl"
            onClick={e => e.stopPropagation()}
          />
        </div>
      )}
    </>
  )
}
