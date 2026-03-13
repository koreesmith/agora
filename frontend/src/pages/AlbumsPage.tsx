import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { albumsApi } from '../api'
import { useAuthStore } from '../store/auth'
import { Image as ImageIcon, Plus, Globe, Users, Lock, X } from 'lucide-react'

const visIcon: Record<string, React.ReactNode> = {
  public:  <Globe size={11} />,
  friends: <Users size={11} />,
  private: <Lock  size={11} />,
}

export default function AlbumsPage() {
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [form, setForm] = useState({ title: '', description: '', visibility: 'friends' })
  const [err, setErr] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['albums'],
    queryFn: () => albumsApi.list().then(r => r.data),
  })
  const albums: any[] = data?.albums ?? []

  const create = useMutation({
    mutationFn: () => albumsApi.create(form),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['albums'] })
      setShowCreate(false)
      setForm({ title: '', description: '', visibility: 'friends' })
    },
    onError: (e: any) => setErr(e.response?.data?.error || 'Could not create album'),
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold">Photo Albums</h1>
        <button onClick={() => setShowCreate(true)} className="btn-primary flex items-center gap-1.5 text-sm">
          <Plus size={15} /> New Album
        </button>
      </div>

      {/* Create modal */}
      {showCreate && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" onClick={() => setShowCreate(false)}>
          <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-md p-6 space-y-4" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-bold">Create Album</h2>
              <button onClick={() => setShowCreate(false)} className="btn-ghost p-1"><X size={18} /></button>
            </div>
            {err && <p className="text-sm text-red-500 bg-red-50 dark:bg-red-900/20 rounded-lg px-3 py-2">{err}</p>}
            <div>
              <label className="label">Album name <span className="text-red-500">*</span></label>
              <input className="input" placeholder="e.g. Summer 2025"
                value={form.title} onChange={e => setForm(f => ({ ...f, title: e.target.value }))} />
            </div>
            <div>
              <label className="label">Description</label>
              <textarea className="input resize-none" rows={2} placeholder="What's this album about?"
                value={form.description} onChange={e => setForm(f => ({ ...f, description: e.target.value }))} />
            </div>
            <div>
              <label className="label">Who can see this?</label>
              <div className="grid grid-cols-3 gap-2 mt-1">
                {([
                  ['public',  Globe,  'Public',  'Anyone'],
                  ['friends', Users,  'Friends', 'Friends only'],
                  ['private', Lock,   'Private', 'Just me'],
                ] as const).map(([val, Icon, label, desc]) => (
                  <button key={val} onClick={() => setForm(f => ({ ...f, visibility: val }))}
                    className={`flex flex-col items-center p-3 rounded-lg border-2 transition-colors text-center ${form.visibility === val ? 'border-agora-600 bg-agora-50 dark:bg-agora-700' : 'border-agora-200 dark:border-agora-600 hover:border-agora-300'}`}>
                    <Icon size={16} className="mb-1" />
                    <span className="text-xs font-medium">{label}</span>
                    <span className="text-xs text-agora-400">{desc}</span>
                  </button>
                ))}
              </div>
            </div>
            <div className="flex gap-2 justify-end pt-1">
              <button onClick={() => setShowCreate(false)} className="btn-secondary">Cancel</button>
              <button onClick={() => create.mutate()} disabled={!form.title.trim() || create.isPending} className="btn-primary">
                {create.isPending ? 'Creating…' : 'Create Album'}
              </button>
            </div>
          </div>
        </div>
      )}

      {isLoading && <div className="text-center py-8 text-agora-400">Loading…</div>}

      {!isLoading && albums.length === 0 && (
        <div className="card p-12 text-center text-agora-400 space-y-2">
          <ImageIcon size={32} className="mx-auto opacity-40" />
          <p className="font-medium">No albums yet</p>
          <p className="text-sm">Create an album to share your photos.</p>
          <button onClick={() => setShowCreate(true)} className="btn-primary text-sm mt-2">Create an album</button>
        </div>
      )}

      <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
        {albums.map((a: any) => <AlbumCard key={a.id} album={a} currentUserId={user?.id} />)}
      </div>
    </div>
  )
}

function AlbumCard({ album: a, currentUserId }: { album: any, currentUserId?: string }) {
  const isOwn = a.owner_id === currentUserId
  return (
    <Link to={`/albums/${a.id}`} className="card overflow-hidden group hover:shadow-md transition-shadow">
      <div className="aspect-square bg-agora-100 dark:bg-agora-800 overflow-hidden">
        {a.cover_url
          ? <img src={a.cover_url} alt="" className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300" />
          : <div className="w-full h-full flex items-center justify-center">
              <ImageIcon size={32} className="text-agora-300 dark:text-agora-600" />
            </div>}
      </div>
      <div className="p-3">
        <p className="font-semibold text-sm truncate">{a.title}</p>
        <div className="flex items-center gap-1.5 text-xs text-agora-400 mt-0.5">
          {visIcon[a.visibility]}
          <span>{a.photo_count} photo{a.photo_count !== 1 ? 's' : ''}</span>
          {!isOwn && <><span>·</span><span className="truncate">@{a.owner_username}</span></>}
        </div>
      </div>
    </Link>
  )
}
