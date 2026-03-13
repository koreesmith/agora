import { useState, useRef } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { albumsApi } from '../api'
import { useAuthStore } from '../store/auth'
import { formatDistanceToNow } from 'date-fns'
import {
  ArrowLeft, Plus, Trash2, Pencil, X, Globe, Users, Lock,
  ChevronLeft, ChevronRight, Image as ImageIcon, Check
} from 'lucide-react'

const visIcons: Record<string, React.ReactNode> = {
  public:  <><Globe size={13} /> Public</>,
  friends: <><Users size={13} /> Friends</>,
  private: <><Lock  size={13} /> Private</>,
}

export default function AlbumPage() {
  const { id } = useParams<{ id: string }>()
  const { user } = useAuthStore()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const fileRef = useRef<HTMLInputElement>(null)

  const [lightbox, setLightbox] = useState<number | null>(null)
  const [editingCaption, setEditingCaption] = useState<string | null>(null)
  const [captionText, setCaptionText] = useState('')
  const [editingAlbum, setEditingAlbum] = useState(false)
  const [albumForm, setAlbumForm] = useState({ title: '', description: '', visibility: 'friends' })
  const [uploading, setUploading] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['album', id],
    queryFn: () => albumsApi.get(id!).then(r => r.data),
    enabled: !!id,
  })
  const album = data?.album
  const photos: any[] = album?.photos ?? []
  const isOwner = album?.owner_id === user?.id

  const deleteAlbum = useMutation({
    mutationFn: () => albumsApi.delete(id!),
    onSuccess: () => navigate(-1),
  })
  const updateAlbum = useMutation({
    mutationFn: () => albumsApi.update(id!, albumForm),
    onSuccess: () => { setEditingAlbum(false); qc.invalidateQueries({ queryKey: ['album', id] }) },
  })
  const deletePhoto = useMutation({
    mutationFn: (photoId: string) => albumsApi.deletePhoto(id!, photoId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['album', id] }),
  })
  const saveCaption = useMutation({
    mutationFn: (photoId: string) => albumsApi.updatePhoto(id!, photoId, { caption: captionText }),
    onSuccess: () => { setEditingCaption(null); qc.invalidateQueries({ queryKey: ['album', id] }) },
  })
  const setCover = useMutation({
    mutationFn: (url: string) => albumsApi.update(id!, { cover_url: url }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['album', id] }),
  })

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || [])
    if (!files.length) return
    setUploading(true)
    try {
      for (const file of files) {
        await albumsApi.uploadPhoto(id!, file)
      }
      qc.invalidateQueries({ queryKey: ['album', id] })
    } catch (err: any) {
      const msg = err?.response?.data?.error || 'Upload failed. Please try a JPEG or PNG file.'
      alert(msg)
    }
    finally { setUploading(false); if (fileRef.current) fileRef.current.value = '' }
  }

  const openEdit = () => {
    setAlbumForm({ title: album.title, description: album.description, visibility: album.visibility })
    setEditingAlbum(true)
  }

  // Lightbox navigation
  const prev = () => setLightbox(l => l !== null ? Math.max(0, l - 1) : null)
  const next = () => setLightbox(l => l !== null ? Math.min(photos.length - 1, l + 1) : null)

  if (isLoading) return <div className="text-center py-12 text-agora-400">Loading…</div>
  if ((error as any)?.response?.status === 403) return (
    <div className="card p-10 text-center space-y-3">
      <Lock size={32} className="mx-auto text-agora-400" />
      <p className="font-semibold">This album is private</p>
      <Link to="/" className="btn-secondary text-sm inline-flex items-center gap-1.5"><ArrowLeft size={14} /> Back</Link>
    </div>
  )
  if (!album) return (
    <div className="card p-10 text-center space-y-3">
      <p className="font-semibold text-agora-600">Album not found</p>
      <Link to="/" className="btn-secondary text-sm inline-flex items-center gap-1.5"><ArrowLeft size={14} /> Back</Link>
    </div>
  )

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center gap-3">
        <button onClick={() => navigate(-1)} className="btn-ghost p-2"><ArrowLeft size={18} /></button>
        <div className="flex-1 min-w-0">
          <h1 className="text-xl font-bold truncate">{album.title}</h1>
          <div className="flex items-center gap-2 text-xs text-agora-400 mt-0.5">
            <Link to={`/profile/${album.owner_username}`} className="hover:underline font-medium">{album.owner_username}</Link>
            <span>·</span>
            <span className="flex items-center gap-1">{visIcons[album.visibility]}</span>
            <span>·</span>
            <span>{album.photo_count} photo{album.photo_count !== 1 ? 's' : ''}</span>
          </div>
        </div>
        {isOwner && (
          <div className="flex gap-1.5">
            <button onClick={openEdit} className="btn-secondary text-sm flex items-center gap-1"><Pencil size={13} /> Edit</button>
            <button
              onClick={() => { if (confirm(`Delete "${album.title}"? All photos will be removed.`)) deleteAlbum.mutate() }}
              className="btn-ghost p-2 text-red-400 hover:text-red-600"><Trash2 size={16} /></button>
          </div>
        )}
      </div>

      {album.description && (
        <p className="text-sm text-agora-500">{album.description}</p>
      )}

      {/* Edit album modal */}
      {editingAlbum && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" onClick={() => setEditingAlbum(false)}>
          <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-md p-6 space-y-4" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between">
              <h2 className="text-lg font-bold">Edit Album</h2>
              <button onClick={() => setEditingAlbum(false)} className="btn-ghost p-1"><X size={18} /></button>
            </div>
            <div><label className="label">Title</label>
              <input className="input" value={albumForm.title} onChange={e => setAlbumForm(f => ({ ...f, title: e.target.value }))} /></div>
            <div><label className="label">Description</label>
              <textarea className="input resize-none" rows={2} value={albumForm.description}
                onChange={e => setAlbumForm(f => ({ ...f, description: e.target.value }))} /></div>
            <div>
              <label className="label">Visibility</label>
              <div className="grid grid-cols-3 gap-2 mt-1">
                {(['public', 'friends', 'private'] as const).map(v => (
                  <button key={v} onClick={() => setAlbumForm(f => ({ ...f, visibility: v }))}
                    className={`p-2 rounded-lg border-2 text-sm font-medium flex items-center justify-center gap-1 ${albumForm.visibility === v ? 'border-agora-600 bg-agora-50 dark:bg-agora-700' : 'border-agora-200 dark:border-agora-600'}`}>
                    {v === 'public' ? <Globe size={13}/> : v === 'friends' ? <Users size={13}/> : <Lock size={13}/>}
                    <span className="capitalize">{v}</span>
                  </button>
                ))}
              </div>
            </div>
            <div className="flex gap-2 justify-end">
              <button onClick={() => setEditingAlbum(false)} className="btn-secondary">Cancel</button>
              <button onClick={() => updateAlbum.mutate()} disabled={!albumForm.title.trim() || updateAlbum.isPending} className="btn-primary">
                {updateAlbum.isPending ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Photo grid */}
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
        {photos.map((photo, idx) => (
          <div key={photo.id} className="relative group aspect-square rounded-lg overflow-hidden bg-agora-100 dark:bg-agora-800">
            <button className="w-full h-full" onClick={() => setLightbox(idx)}>
              <img src={photo.url} alt={photo.caption} className="w-full h-full object-cover hover:opacity-90 transition-opacity" />
            </button>
            {isOwner && (
              <div className="absolute top-1.5 right-1.5 flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                <button
                  onClick={e => { e.stopPropagation(); setCover.mutate(photo.url) }}
                  className="bg-black/60 text-white rounded-full p-1 hover:bg-black/80 text-xs"
                  title="Set as cover">
                  <Check size={12} />
                </button>
                <button
                  onClick={e => { e.stopPropagation(); if (confirm('Remove this photo?')) deletePhoto.mutate(photo.id) }}
                  className="bg-black/60 text-white rounded-full p-1 hover:bg-red-600">
                  <X size={12} />
                </button>
              </div>
            )}
            {photo.caption && (
              <div className="absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/60 p-2">
                <p className="text-white text-xs line-clamp-1">{photo.caption}</p>
              </div>
            )}
          </div>
        ))}

        {/* Upload tile */}
        {isOwner && (
          <label className={`aspect-square rounded-lg border-2 border-dashed border-agora-200 dark:border-agora-600 flex flex-col items-center justify-center gap-2 cursor-pointer hover:border-agora-400 transition-colors ${uploading ? 'opacity-50 pointer-events-none' : ''}`}>
            <input ref={fileRef} type="file" accept="image/*" multiple className="hidden" onChange={handleUpload} disabled={uploading} />
            {uploading ? (
              <span className="text-sm text-agora-400 animate-pulse">Uploading…</span>
            ) : (
              <>
                <Plus size={24} className="text-agora-400" />
                <span className="text-xs text-agora-400">Add photos</span>
              </>
            )}
          </label>
        )}
      </div>

      {photos.length === 0 && !isOwner && (
        <div className="card p-10 text-center text-agora-400 space-y-2">
          <ImageIcon size={32} className="mx-auto opacity-40" />
          <p>No photos yet.</p>
        </div>
      )}

      {/* Lightbox */}
      {lightbox !== null && photos[lightbox] && (
        <div className="fixed inset-0 z-50 bg-black/90 flex items-center justify-center"
          onClick={() => setLightbox(null)}>
          {/* Prev */}
          {lightbox > 0 && (
            <button onClick={e => { e.stopPropagation(); prev() }}
              className="absolute left-4 top-1/2 -translate-y-1/2 bg-black/40 text-white rounded-full p-2 hover:bg-black/70 z-10">
              <ChevronLeft size={24} />
            </button>
          )}

          {/* Image */}
          <div className="max-w-[90vw] max-h-[90vh] flex flex-col items-center gap-3" onClick={e => e.stopPropagation()}>
            <img
              src={photos[lightbox].url}
              alt={photos[lightbox].caption}
              className="max-w-full max-h-[80vh] w-auto h-auto object-contain rounded-lg shadow-2xl"
            />
            {/* Caption */}
            <div className="w-full max-w-lg text-center">
              {editingCaption === photos[lightbox].id ? (
                <div className="flex gap-2" onClick={e => e.stopPropagation()}>
                  <input className="input flex-1 text-sm"
                    value={captionText}
                    onChange={e => setCaptionText(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter') saveCaption.mutate(photos[lightbox].id) }}
                    autoFocus />
                  <button onClick={() => saveCaption.mutate(photos[lightbox].id)} className="btn-primary text-sm px-3">Save</button>
                  <button onClick={() => setEditingCaption(null)} className="btn-secondary text-sm px-3">Cancel</button>
                </div>
              ) : (
                <div className="flex items-center justify-center gap-2">
                  {photos[lightbox].caption
                    ? <p className="text-white/80 text-sm">{photos[lightbox].caption}</p>
                    : isOwner && <span className="text-white/40 text-sm italic">Add a caption…</span>}
                  {isOwner && (
                    <button onClick={() => { setEditingCaption(photos[lightbox].id); setCaptionText(photos[lightbox].caption) }}
                      className="text-white/50 hover:text-white/90 transition-colors">
                      <Pencil size={14} />
                    </button>
                  )}
                </div>
              )}
            </div>
            {/* Counter */}
            <p className="text-white/40 text-xs">{lightbox + 1} / {photos.length}</p>
          </div>

          {/* Next */}
          {lightbox < photos.length - 1 && (
            <button onClick={e => { e.stopPropagation(); next() }}
              className="absolute right-4 top-1/2 -translate-y-1/2 bg-black/40 text-white rounded-full p-2 hover:bg-black/70 z-10">
              <ChevronRight size={24} />
            </button>
          )}

          {/* Close */}
          <button onClick={() => setLightbox(null)}
            className="absolute top-4 right-4 bg-black/40 text-white rounded-full p-1.5 hover:bg-black/70">
            <X size={20} />
          </button>
        </div>
      )}
    </div>
  )
}
