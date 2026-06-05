import { useState, useEffect } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { pagesApi, feedApi, pageMembersApi } from '../api'
import { useAuthStore } from '../store/auth'
import { ArrowLeft, Upload, X, UserPlus, Trash2 } from 'lucide-react'

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

      {/* Team management (AGORA-112) */}
      <TeamSection slug={slug!} isOwner={page.is_owner} qc={qc} />
    </div>
  )
}

// ── Team Section ──────────────────────────────────────────────────────────────

function TeamSection({ slug, isOwner, qc }: { slug: string, isOwner: boolean, qc: any }) {
  const [inviteUsername, setInviteUsername] = useState('')
  const [inviteRole, setInviteRole] = useState<'admin'|'editor'>('editor')
  const [inviteError, setInviteError] = useState('')

  const { data, refetch } = useQuery({
    queryKey: ['page-members', slug],
    queryFn: () => pageMembersApi.list(slug).then(r => r.data),
  })
  const members: any[] = data?.members ?? []

  const invite = useMutation({
    mutationFn: () => pageMembersApi.invite(slug, inviteUsername, inviteRole),
    onSuccess: () => { setInviteUsername(''); setInviteError(''); refetch() },
    onError: (e: any) => setInviteError(e?.response?.data?.error ?? 'Could not invite user'),
  })

  const remove = useMutation({
    mutationFn: (userId: string) => pageMembersApi.remove(slug, userId),
    onSuccess: () => refetch(),
  })

  const setRole = useMutation({
    mutationFn: ({ userId, role }: { userId: string, role: string }) => pageMembersApi.setRole(slug, userId, role),
    onSuccess: () => refetch(),
  })

  return (
    <div className="card p-5 space-y-4">
      <h3 className="font-semibold">Team</h3>
      <p className="text-sm text-agora-500">Admins can post and edit page settings. Editors can post only. Invited users must accept before gaining access.</p>

      {/* Current members */}
      <div className="divide-y divide-agora-100 dark:divide-agora-700">
        {members.map((m: any) => (
          <div key={m.user_id} className="flex items-center gap-3 py-2.5">
            <div className="w-8 h-8 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0">
              {m.avatar_url
                ? <img src={m.avatar_url} alt="" className="w-full h-full object-cover" />
                : <span className="w-full h-full flex items-center justify-center text-xs font-bold text-agora-500">{(m.display_name || m.username)[0]?.toUpperCase()}</span>}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium truncate">{m.display_name || m.username}</p>
              <p className="text-xs text-agora-400">@{m.username}{!m.accepted && <span className="ml-1 text-amber-500">· pending</span>}</p>
            </div>
            <div className="flex items-center gap-2 flex-shrink-0">
              {m.role !== 'owner' && isOwner ? (
                <select
                  value={m.role}
                  onChange={e => setRole.mutate({ userId: m.user_id, role: e.target.value })}
                  className="text-xs border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1 bg-white dark:bg-agora-800">
                  <option value="admin">Admin</option>
                  <option value="editor">Editor</option>
                </select>
              ) : (
                <span className="text-xs text-agora-500 capitalize">{m.role}</span>
              )}
              {m.role !== 'owner' && isOwner && (
                <button onClick={() => remove.mutate(m.user_id)} className="text-agora-400 hover:text-red-500 transition-colors">
                  <Trash2 size={14} />
                </button>
              )}
            </div>
          </div>
        ))}
      </div>

      {/* Invite form (owner only) */}
      {isOwner && (
        <div className="space-y-2 pt-2 border-t border-agora-100 dark:border-agora-700">
          <label className="label">Invite a team member</label>
          <div className="flex gap-2">
            <input
              value={inviteUsername}
              onChange={e => setInviteUsername(e.target.value)}
              placeholder="@username"
              className="input flex-1 text-sm"
              onKeyDown={e => e.key === 'Enter' && invite.mutate()}
            />
            <select value={inviteRole} onChange={e => setInviteRole(e.target.value as any)}
              className="text-sm border border-agora-200 dark:border-agora-600 rounded-lg px-2 py-1.5 bg-white dark:bg-agora-800">
              <option value="editor">Editor</option>
              <option value="admin">Admin</option>
            </select>
            <button onClick={() => invite.mutate()} disabled={!inviteUsername.trim() || invite.isPending}
              className="btn-primary text-sm flex items-center gap-1.5">
              <UserPlus size={14} /> Invite
            </button>
          </div>
          {inviteError && <p className="text-xs text-red-500">{inviteError}</p>}
        </div>
      )}
    </div>
  )
}
