import { useState, useEffect } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { pagesApi, feedApi } from '../api'
import { useAuthStore } from '../store/auth'
import { ArrowLeft, Upload, X, TrendingUp, Users, Heart, MessageCircle } from 'lucide-react'

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
  const [activityPubEnabled, setActivityPubEnabled] = useState(true)
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
      setActivityPubEnabled(page.activitypub_enabled ?? true)
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
      activitypub_enabled: activityPubEnabled,
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

        {/* Fediverse (AGORA-115) */}
        <div className="flex items-center justify-between py-2 border-t border-agora-100 dark:border-agora-700 pt-4">
          <div>
            <p className="font-medium text-sm">Fediverse (ActivityPub)</p>
            <p className="text-xs text-agora-400">
              Let Mastodon and other fediverse apps discover, follow, and see posts from this page.
            </p>
          </div>
          <button
            onClick={() => setActivityPubEnabled(v => !v)}
            className={`relative inline-flex h-6 w-11 rounded-full transition-colors flex-shrink-0 ml-4 ${activityPubEnabled ? 'bg-agora-700' : 'bg-agora-200'}`}>
            <span className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform m-0.5 ${activityPubEnabled ? 'translate-x-5' : 'translate-x-0'}`} />
          </button>
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

      {/* Analytics (AGORA-113) */}
      <AnalyticsSection slug={slug!} />
    </div>
  )
}

// ── Analytics Section ─────────────────────────────────────────────────────────

function AnalyticsSection({ slug }: { slug: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ['page-analytics', slug],
    queryFn: () => pagesApi.analytics(slug).then(r => r.data),
    staleTime: 60_000,
  })

  if (isLoading) return (
    <div className="card p-5 text-center text-agora-400 text-sm">Loading analytics…</div>
  )
  if (!data) return null

  const growth = data.subscriber_growth ?? {}
  const topPosts: any[] = data.top_posts ?? []

  return (
    <div className="card p-5 space-y-5">
      <div className="flex items-center gap-2">
        <TrendingUp size={16} className="text-agora-500" />
        <h3 className="font-semibold">Analytics</h3>
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-3 gap-3">
        <div className="bg-agora-50 dark:bg-agora-800 rounded-xl p-3 text-center">
          <Users size={16} className="mx-auto text-agora-400 mb-1" />
          <p className="text-xl font-bold">{data.total_subscribers ?? 0}</p>
          <p className="text-xs text-agora-500">Subscribers</p>
        </div>
        <div className="bg-agora-50 dark:bg-agora-800 rounded-xl p-3 text-center">
          <Heart size={16} className="mx-auto text-red-400 mb-1" />
          <p className="text-xl font-bold">{data.total_likes ?? 0}</p>
          <p className="text-xs text-agora-500">Total likes</p>
        </div>
        <div className="bg-agora-50 dark:bg-agora-800 rounded-xl p-3 text-center">
          <MessageCircle size={16} className="mx-auto text-agora-400 mb-1" />
          <p className="text-xl font-bold">{data.total_comments ?? 0}</p>
          <p className="text-xs text-agora-500">Total comments</p>
        </div>
      </div>

      {/* Subscriber growth */}
      <div>
        <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide mb-2">Subscriber growth</p>
        <div className="flex gap-3">
          {[['7 days', growth['7d']], ['30 days', growth['30d']], ['90 days', growth['90d']]].map(([label, val]) => (
            <div key={label as string} className="flex-1 bg-agora-50 dark:bg-agora-800 rounded-xl p-2.5 text-center">
              <p className={`text-lg font-bold ${(val as number) > 0 ? 'text-green-600' : (val as number) < 0 ? 'text-red-500' : ''}`}>
                {(val as number) > 0 ? '+' : ''}{val as number}
              </p>
              <p className="text-xs text-agora-400">{label as string}</p>
            </div>
          ))}
        </div>
      </div>

      {/* Top posts */}
      {topPosts.length > 0 && (
        <div>
          <p className="text-xs font-semibold text-agora-500 uppercase tracking-wide mb-2">Top posts by engagement</p>
          <div className="space-y-2">
            {topPosts.map((p: any) => (
              <div key={p.id} className="flex items-center gap-3 text-sm">
                <div className="flex-1 min-w-0">
                  <p className="truncate text-agora-700 dark:text-agora-300">{p.content || '(no text)'}</p>
                </div>
                <div className="flex gap-2 text-xs text-agora-400 flex-shrink-0">
                  <span className="flex items-center gap-0.5"><Heart size={11} /> {p.like_count}</span>
                  <span className="flex items-center gap-0.5"><MessageCircle size={11} /> {p.comment_count}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
