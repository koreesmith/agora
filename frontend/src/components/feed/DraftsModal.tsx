import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { Image as ImageIcon, Video, BarChart2, Trash2, X } from 'lucide-react'
import { draftsApi } from '../../api'

// AGORA-431: the drafts someone has saved, to pick one up where they left it or
// throw it away. They live on the server, so a draft saved on a phone is here on
// the web too.

export interface Draft {
  id: string
  content: string
  data: Record<string, any>
  updated_at: string
}

export default function DraftsModal({ onPick, onClose }: { onPick: (d: Draft) => void; onClose: () => void }) {
  const qc = useQueryClient()
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['drafts'],
    queryFn: () => draftsApi.list().then(r => r.data),
  })
  const drafts: Draft[] = data?.drafts ?? []
  const remove = useMutation({
    mutationFn: (id: string) => draftsApi.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['drafts'] }),
  })

  return (
    <div data-reel-dialog className="fixed inset-0 z-50 flex items-end sm:items-center justify-center bg-black/50 sm:p-4" onClick={onClose}>
      <div role="dialog" aria-modal="true" aria-label="Drafts"
        className="w-full sm:max-w-md max-h-[85vh] overflow-y-auto bg-white dark:bg-agora-900 text-agora-800 dark:text-agora-100 sm:rounded-2xl shadow-xl"
        onClick={e => e.stopPropagation()}>
        <div className="sticky top-0 flex items-center justify-between px-4 py-3 bg-white dark:bg-agora-900 border-b border-agora-100 dark:border-agora-700">
          <h2 className="font-semibold">Drafts</h2>
          <button onClick={onClose} aria-label="Close" className="text-agora-400 hover:text-agora-700"><X size={20} /></button>
        </div>
        <div className="p-2">
          {isLoading && <p className="p-4 text-sm text-agora-400" role="status">Loading drafts…</p>}
          {isError && (
            <p className="p-4 text-sm text-red-500" role="alert">Couldn't load your drafts. <button className="underline" onClick={() => refetch()}>Try again</button></p>
          )}
          {!isLoading && !isError && drafts.length === 0 && (
            <p className="p-4 text-sm text-agora-400">No drafts. Use "Save draft" while writing a post to keep it for later.</p>
          )}
          <ul>
            {drafts.map(d => {
              const photos = (d.data.image_urls ?? []).length
              return (
                <li key={d.id} className="flex items-start gap-2 rounded-lg hover:bg-agora-50 dark:hover:bg-agora-800">
                  <button onClick={() => onPick(d)} className="flex-1 min-w-0 text-left p-3">
                    <p className="text-sm line-clamp-2 break-words">{d.content.trim() || <span className="text-agora-400 italic">No text</span>}</p>
                    <p className="text-xs text-agora-400 mt-1 flex items-center gap-2">
                      <span>{formatDistanceToNow(new Date(d.updated_at), { addSuffix: true })}</span>
                      {photos > 0 && <span className="flex items-center gap-0.5"><ImageIcon size={12} /> {photos}</span>}
                      {d.data.video_url && <span className="flex items-center gap-0.5"><Video size={12} /> video</span>}
                      {(d.data.poll_options ?? []).some((o: string) => o?.trim()) && <span className="flex items-center gap-0.5"><BarChart2 size={12} /> poll</span>}
                    </p>
                  </button>
                  <button onClick={() => { if (confirm('Delete this draft?')) remove.mutate(d.id) }}
                    aria-label="Delete draft" className="p-3 text-agora-400 hover:text-red-500"><Trash2 size={16} /></button>
                </li>
              )
            })}
          </ul>
        </div>
      </div>
    </div>
  )
}
