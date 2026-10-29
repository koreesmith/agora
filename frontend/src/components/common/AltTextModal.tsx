import { useState } from 'react'
import { X } from 'lucide-react'

// Mastodon's limit, the same one the server caps descriptions at
// (alttext.MaxLen), so whatever is written here federates whole.
export const MAX_ALT_LEN = 1500

/**
 * Write or change one image's description (AGORA-400). Used by the composers
 * and by the post editor.
 */
export default function AltTextModal({ imageUrl, initial, onSave, onClose }: {
  imageUrl: string
  initial: string
  onSave: (alt: string) => void
  onClose: () => void
}) {
  const [text, setText] = useState(initial)
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60" onClick={onClose}>
      <div
        role="dialog"
        aria-labelledby="alt-text-title"
        className="bg-white dark:bg-agora-800 rounded-2xl shadow-2xl w-full max-w-md"
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 pt-4 pb-3 border-b border-agora-100 dark:border-agora-700">
          <div>
            <h3 id="alt-text-title" className="font-semibold text-agora-900 dark:text-agora-100">Describe this image</h3>
            <p className="text-xs text-agora-400 mt-0.5">For people who use screen readers or can't see the image</p>
          </div>
          <button onClick={onClose} aria-label="Close" className="text-agora-400 hover:text-agora-600 transition-colors">
            <X size={18} />
          </button>
        </div>
        <div className="p-4 space-y-3">
          <img src={imageUrl} alt="" className="w-full max-h-48 object-contain rounded-lg bg-agora-50 dark:bg-agora-900" />
          <textarea
            className="input w-full text-sm min-h-[6rem]"
            value={text}
            onChange={e => setText(e.target.value.slice(0, MAX_ALT_LEN))}
            placeholder="What's in the image? Mention anything that matters to the post, like text in it."
            autoFocus
            aria-label="Image description"
          />
          <p className="text-xs text-agora-400 text-right">{text.length}/{MAX_ALT_LEN}</p>
        </div>
        <div className="flex gap-2 px-4 pb-4">
          <button onClick={onClose} className="btn-secondary flex-1 text-sm">Cancel</button>
          <button onClick={() => { onSave(text.trim()); onClose() }} className="btn-primary flex-1 text-sm">Save</button>
        </div>
      </div>
    </div>
  )
}
