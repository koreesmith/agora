import { useState } from 'react'

/**
 * An "ALT" badge for an image that has a description (AGORA-400). The
 * description is already the image's alt text for screen readers; the badge
 * is for everyone else, the way Mastodon and Bluesky show it. Clicking it
 * shows the text below the image without opening the lightbox underneath.
 *
 * AGORA-414: the description is laid out in normal flow under the image, never
 * over it, so opening it hides none of the photo. The badge therefore sits in
 * the image's top-left corner, where it stays put as the panel grows below.
 *
 * Renders nothing without a description. Place it inside a positioned
 * (relative) container that wraps the image, as a sibling of any button that
 * opens the image, since a button can't contain another button. The panel is
 * the container's last in-flow child, so nothing else should follow the image
 * inside it.
 */
export default function AltBadge({ alt }: { alt?: string }) {
  const [open, setOpen] = useState(false)
  if (!alt) return null
  return (
    <>
      <button
        type="button"
        onClick={e => { e.stopPropagation(); setOpen(o => !o) }}
        aria-expanded={open}
        aria-label={open ? 'Hide image description' : 'Show image description'}
        className="absolute top-1.5 left-1.5 z-10 rounded px-1.5 py-0.5 text-[10px] font-bold tracking-wide bg-black/70 text-white hover:bg-black/85"
      >
        ALT
      </button>
      {open && (
        <div
          onClick={e => e.stopPropagation()}
          className="mt-1 max-h-40 overflow-y-auto rounded-lg bg-agora-100 dark:bg-agora-800 p-2.5 text-xs leading-relaxed text-agora-800 dark:text-agora-100 whitespace-pre-wrap text-left"
        >
          {alt}
        </div>
      )}
    </>
  )
}
