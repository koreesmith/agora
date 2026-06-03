import { useState, useEffect } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { pagesApi, feedApi } from '../api'
import { useAuthStore } from '../store/auth'
import { ArrowLeft, Upload, X } from 'lucide-react'

const PAGE_TYPES = [
  { value: '',             label: 'Select a type (optional)' },
  { value: 'band',         label: 'Band / Music' },
  { value: 'business',     label: 'Business' },
  { value: 'organization', label: 'Organization / Nonprofit' },
  { value: 'creator',      label: 'Creator / Influencer' },
]

export default function PageSettingsPage() {
  const { slug } = useParams<{ slug: string }>()!
  const { user } = useAuthStore()
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [displayName, setDisplayName] = useState('')
  const [bio, setBio] = useState('')
  const [pageType, setPageType] = useState('')
  const [privacy, setPrivacy] = useState('public')
  const [avatarUrl, setAvatarUrl] = useState('')
  const [coverUrl, setCoverUrl] = useState('')
  const [uploadingAvatar, setUploadingAvatar] = useState(false)
  const [uploadingCover, setUploadingCover] = useState(false)
  const [saved, setSaved] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['page', slug],
    queryFn: () => pagesApi.get(slug!).then(r => r.data),
  })
  const page = data?.page

  useEffect(() => {
    if (page) {
      setDisplayName(page.display_name)
      setBio(page.bio || '')
      setPageType(page.page_type || '')
      setPrivacy(page.privacy)
      setAvatarUrl(page.avatar_url || '')
      setCoverUrl(page.cover_url || '')
    }
  }, [page])

  const update = useMutation({
    mutationFn: () => pagesApi.update(slug!, {
      display_name: displayName,
      bio,
      page_type: pageType,
      privacy,
      avatar_url: avatarUrl,
      cover_url: coverUrl,
    }),
    onSuccess: () => {
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
      qc.invalidateQueries({ queryKey: ['page', slug] })
    },
  })

  const uploadAvatar = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploadingAvatar(true)
    try {
      const res = await feedApi.uploadMedia(file, 'avatar')
      setAvatarUrl((res as any).data.url)
    } finally {
      setUploadingAvatar(false)
      e.target.value = ''
    }
  }

  const uploadCover = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploadingCover(true)
    try {
      const res = await feedApi.uploadMedia(file, 'cover')
      setCoverUrl((res as any).data.url)
    } finally {
      setUploadingCover(false)
      e.target.value = ''
    }
  }

  if (isLoading) return <div className="text-center py-12 text-agora-400">Loading…</div>
  if (error || !page) return (
    <div className="card p-8 text-center space-y-2">
      <p className="font-semibold">Page not found</p>
      <Link to="/pages" className="text-sm text-agora-500 hover:underline">← Pages</Link>
    </div>
  )
  if (!page.is_owner) return (
    <div className="card p-8 text-center space-y-2">
      <p className="font-semibold">You don't have permission to edit this page.</p>
      <Link to={`/pages/${slug}`} className="text-sm text-agora-500 hover:underline">← Back to page</Link>
    </div>
  )

  return (
    <div className="max-w-xl mx-auto space-y-6">
      {/* Back */}
      <div className="flex items-center gap-2">
        <Link to={`/pages/${slug}`} className="btn-ghost p-1.5 text-agora-500">
          <ArrowLeft size={18} />
        </Link>
        <h1 className="text-lg font-bold">Page Settings</h1>
      </div>

      <div className="card p-5 space-y-5">
        {/* Cover photo */}
        <div>
          <label className="label mb-2">Cover photo</label>
          <div className="relative w-full h-28 rounded-xl overflow-hidden bg-agora-100 dark:bg-agora-700">
            {coverUrl
              ? <img src={coverUrl} alt="" className="w-full h-full object-cover" />
              : <div className="w-full h-full flex items-center justify-center text-agora-400">No cover photo</div>}
            <div className="absolute inset-0 flex items-center justify-center gap-2 bg-black/0 hover:bg-black/30 transition-colors group">
              <label className="btn-secondary text-xs cursor-pointer opacity-0 group-hover:opacity-100 transition-opacity flex items-center gap-1">
                <Upload size={12} /> Upload
                <input type="file" accept="image/*" className="hidden" onChange={uploadCover} disabled={uploadingCover} />
              </label>
              {coverUrl && (
                <button onClick={() => setCoverUrl('')}
                  className="btn-secondary text-xs opacity-0 group-hover:opacity-100 transition-opacity flex items-center gap-1">
                  <X size={12} /> Remove
                </button>
              )}
            </div>
          </div>
          {uploadingCover && <p className="text-xs text-agora-400 mt-1">Uploading…</p>}
        </div>

        {/* Avatar */}
        <div>
          <label className="label mb-2">Page avatar</label>
          <div className="flex items-center gap-3">
            <div className="w-16 h-16 rounded-xl bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0 relative group">
              {avatarUrl
                ? <img src={avatarUrl} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center text-xl font-bold text-agora-500">
                    {displayName[0] || '?'}
                  </span>}
              <label className="absolute inset-0 bg-black/0 hover:bg-black/40 transition-colors flex items-center justify-center cursor-pointer">
                <Upload size={16} className="text-white opacity-0 group-hover:opacity-100 transition-opacity" />
                <input type="file" accept="image/*" className="hidden" onChange={uploadAvatar} disabled={uploadingAvatar} />
              </label>
            </div>
            {avatarUrl && (
              <button onClick={() => setAvatarUrl('')} className="text-xs text-agora-400 hover:text-red-500">Remove avatar</button>
            )}
          </div>
          {uploadingAvatar && <p className="text-xs text-agora-400 mt-1">Uploading…</p>}
        </div>

        {/* Display name */}
        <div>
          <label className="label">Page name *</label>
          <input
            value={displayName}
            onChange={e => setDisplayName(e.target.value)}
            maxLength={100}
            className="input w-full mt-1"
            placeholder="e.g. Acme Records"
          />
          <p className="text-xs text-agora-400 mt-1">Slug: @{page.slug} (cannot be changed)</p>
        </div>

        {/* Bio */}
        <div>
          <label className="label">Bio</label>
          <textarea
            value={bio}
            onChange={e => setBio(e.target.value)}
            maxLength={500}
            rows={3}
            className="input w-full mt-1 resize-none"
            placeholder="Describe your page in a sentence or two…"
          />
          <p className="text-xs text-agora-400 mt-1">{bio.length}/500</p>
        </div>

        {/* Page type */}
        <div>
          <label className="label">Page type</label>
          <select value={pageType} onChange={e => setPageType(e.target.value)}
            className="input w-full mt-1">
            {PAGE_TYPES.map(t => (
              <option key={t.value} value={t.value}>{t.label}</option>
            ))}
          </select>
        </div>

        {/* Privacy */}
        <div>
          <label className="label">Visibility</label>
          <div className="flex gap-3 mt-1">
            {['public', 'private'].map(p => (
              <label key={p} className="flex items-center gap-2 cursor-pointer text-sm">
                <input type="radio" name="privacy" value={p} checked={privacy === p}
                  onChange={() => setPrivacy(p)} className="accent-agora-600" />
                <span className="capitalize">{p}</span>
              </label>
            ))}
          </div>
          <p className="text-xs text-agora-400 mt-1">
            {privacy === 'private' ? 'Only you can see this page.' : 'Anyone can find and subscribe to this page.'}
          </p>
        </div>

        {/* Save */}
        <div className="flex items-center gap-3 pt-2 border-t border-agora-100 dark:border-agora-700">
          <button
            onClick={() => update.mutate()}
            disabled={!displayName.trim() || update.isPending}
            className="btn-primary text-sm">
            {update.isPending ? 'Saving…' : saved ? '✓ Saved' : 'Save changes'}
          </button>
          <Link to={`/pages/${slug}`} className="btn-secondary text-sm">Cancel</Link>
        </div>
      </div>
    </div>
  )
}
