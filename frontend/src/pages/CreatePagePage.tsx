import { useState } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { pagesApi, feedApi } from '../api'
import { ArrowLeft, ArrowRight, Check, Upload, X, CheckCircle } from 'lucide-react'

const PAGE_TYPES = [
  { value: 'band',         label: 'Band / Music',               emoji: '🎵', desc: 'For musicians, bands, and record labels' },
  { value: 'business',     label: 'Business',                   emoji: '🏢', desc: 'For companies and commercial enterprises' },
  { value: 'organization', label: 'Organization / Nonprofit',   emoji: '🤝', desc: 'For nonprofits, charities, and community orgs' },
  { value: 'creator',      label: 'Creator',                    emoji: '✨', desc: 'For influencers, artists, and content creators' },
]

const STEPS = ['Name', 'Bio', 'Type', 'Photos', 'Review']

function StepIndicator({ current }: { current: number }) {
  return (
    <div className="flex items-center gap-1 justify-center mb-6">
      {STEPS.map((label, i) => (
        <div key={i} className="flex items-center gap-1">
          <div className={`w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold transition-colors ${
            i < current
              ? 'bg-agora-600 text-white'
              : i === current
              ? 'bg-agora-100 dark:bg-agora-700 text-agora-700 dark:text-agora-200 ring-2 ring-agora-600'
              : 'bg-agora-100 dark:bg-agora-800 text-agora-400'
          }`}>
            {i < current ? <Check size={12} /> : i + 1}
          </div>
          {i < STEPS.length - 1 && (
            <div className={`w-6 h-0.5 transition-colors ${i < current ? 'bg-agora-600' : 'bg-agora-200 dark:bg-agora-700'}`} />
          )}
        </div>
      ))}
    </div>
  )
}

export default function CreatePagePage() {
  const navigate = useNavigate()
  const [step, setStep] = useState(0)

  // Form state
  const [displayName, setDisplayName] = useState('')
  const [bio, setBio] = useState('')
  const [pageType, setPageType] = useState('')
  const [avatarUrl, setAvatarUrl] = useState('')
  const [coverUrl, setCoverUrl] = useState('')
  const [uploadingAvatar, setUploadingAvatar] = useState(false)
  const [uploadingCover, setUploadingCover] = useState(false)
  const [createdSlug, setCreatedSlug] = useState('')

  const create = useMutation({
    mutationFn: () => pagesApi.create({
      display_name: displayName,
      bio,
      page_type: pageType || undefined,
      privacy: 'public',
      avatar_url: avatarUrl || undefined,
      cover_url: coverUrl || undefined,
    }),
    onSuccess: (res: any) => {
      setCreatedSlug(res.data.slug)
      setStep(5) // success screen
    },
  })

  const uploadAvatar = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setUploadingAvatar(true)
    try {
      const res = await feedApi.uploadMedia(file, 'avatar')
      setAvatarUrl((res as any).data.url)
    } finally { setUploadingAvatar(false); e.target.value = '' }
  }

  const uploadCover = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]; if (!file) return
    setUploadingCover(true)
    try {
      const res = await feedApi.uploadMedia(file, 'cover')
      setCoverUrl((res as any).data.url)
    } finally { setUploadingCover(false); e.target.value = '' }
  }

  const canAdvance = () => {
    if (step === 0) return displayName.trim().length >= 2
    return true
  }

  const next = () => {
    if (step === 4) { create.mutate(); return }
    setStep(s => s + 1)
  }
  const back = () => setStep(s => s - 1)

  // ── Success ───────────────────────────────────────────────────────────────────
  if (step === 5) return (
    <div className="max-w-md mx-auto">
      <div className="card p-8 text-center space-y-4">
        <CheckCircle size={48} className="mx-auto text-green-500" />
        <h2 className="text-xl font-bold">You're all set!</h2>
        <p className="text-sm text-agora-500">
          Your page <span className="font-semibold">@{createdSlug}</span> is live.
          Start publishing posts to let your subscribers know you're here.
        </p>
        <div className="flex gap-2 justify-center">
          <button onClick={() => navigate(`/pages/${createdSlug}`)} className="btn-primary">
            View my page
          </button>
          <button onClick={() => navigate(`/pages/${createdSlug}/settings`)} className="btn-secondary">
            Edit settings
          </button>
        </div>
      </div>
    </div>
  )

  return (
    <div className="max-w-md mx-auto space-y-4">
      {/* Back to pages */}
      <div className="flex items-center gap-2">
        <Link to="/pages" className="btn-ghost p-1.5 text-agora-500"><ArrowLeft size={18} /></Link>
        <h1 className="text-lg font-bold">Create a Page</h1>
      </div>

      <div className="card p-6 space-y-5">
        <StepIndicator current={step} />

        {/* Step 0: Name */}
        {step === 0 && (
          <div className="space-y-3">
            <div>
              <h2 className="font-semibold text-base">What's your page called?</h2>
              <p className="text-sm text-agora-500 mt-0.5">This is the public name of your page. You can change it later.</p>
            </div>
            <div>
              <label className="label">Page name *</label>
              <input
                value={displayName}
                onChange={e => setDisplayName(e.target.value)}
                maxLength={100}
                autoFocus
                className="input w-full mt-1"
                placeholder="e.g. Acme Records, Jazz Collective, The Daily Bean"
              />
              <p className="text-xs text-agora-400 mt-1">
                Your page URL will be <span className="font-mono">/pages/{displayName.trim().toLowerCase().replace(/\s+/g, '_').replace(/[^a-z0-9_]/g, '') || '…'}</span>
              </p>
            </div>
          </div>
        )}

        {/* Step 1: Bio */}
        {step === 1 && (
          <div className="space-y-3">
            <div>
              <h2 className="font-semibold text-base">Describe your page</h2>
              <p className="text-sm text-agora-500 mt-0.5">A short bio helps people understand who you are. You can skip this and fill it in later.</p>
            </div>
            <div>
              <label className="label">Bio <span className="text-agora-400">(optional)</span></label>
              <textarea
                value={bio}
                onChange={e => setBio(e.target.value)}
                maxLength={500}
                rows={4}
                autoFocus
                className="input w-full mt-1 resize-none"
                placeholder="Tell people what this page is about…"
              />
              <p className="text-xs text-agora-400 mt-1 text-right">{bio.length}/500</p>
            </div>
          </div>
        )}

        {/* Step 2: Type */}
        {step === 2 && (
          <div className="space-y-3">
            <div>
              <h2 className="font-semibold text-base">What kind of page is this?</h2>
              <p className="text-sm text-agora-500 mt-0.5">Helps people discover your page. You can skip this.</p>
            </div>
            <div className="space-y-2">
              {PAGE_TYPES.map(t => (
                <button
                  key={t.value}
                  onClick={() => setPageType(pt => pt === t.value ? '' : t.value)}
                  className={`w-full text-left p-3 rounded-xl border transition-colors flex items-start gap-3 ${
                    pageType === t.value
                      ? 'border-agora-600 bg-agora-50 dark:bg-agora-700/50'
                      : 'border-agora-200 dark:border-agora-600 hover:border-agora-400'
                  }`}>
                  <span className="text-xl">{t.emoji}</span>
                  <div>
                    <p className="font-medium text-sm">{t.label}</p>
                    <p className="text-xs text-agora-500">{t.desc}</p>
                  </div>
                  {pageType === t.value && <Check size={16} className="ml-auto text-agora-600 flex-shrink-0 mt-0.5" />}
                </button>
              ))}
            </div>
          </div>
        )}

        {/* Step 3: Photos */}
        {step === 3 && (
          <div className="space-y-4">
            <div>
              <h2 className="font-semibold text-base">Add a photo and cover</h2>
              <p className="text-sm text-agora-500 mt-0.5">Optional — you can add these later from page settings.</p>
            </div>

            {/* Cover */}
            <div>
              <label className="label mb-2">Cover photo</label>
              <div className="relative w-full h-24 rounded-xl overflow-hidden bg-gradient-to-r from-agora-600 to-agora-400 group">
                {coverUrl && <img src={coverUrl} alt="" className="w-full h-full object-cover" />}
                <div className="absolute inset-0 flex items-center justify-center gap-2">
                  <label className={`btn-secondary text-xs cursor-pointer flex items-center gap-1 ${uploadingCover ? 'opacity-50' : ''}`}>
                    <Upload size={12} /> {coverUrl ? 'Change' : 'Upload cover'}
                    <input type="file" accept="image/*" className="hidden" onChange={uploadCover} disabled={uploadingCover} />
                  </label>
                  {coverUrl && (
                    <button onClick={() => setCoverUrl('')} className="btn-secondary text-xs flex items-center gap-1">
                      <X size={12} /> Remove
                    </button>
                  )}
                </div>
              </div>
            </div>

            {/* Avatar */}
            <div>
              <label className="label mb-2">Page avatar</label>
              <div className="flex items-center gap-4">
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
                <div className="text-sm text-agora-500">
                  <p>Recommended: square image, at least 200×200px.</p>
                  {avatarUrl && (
                    <button onClick={() => setAvatarUrl('')} className="text-xs text-red-500 hover:underline mt-1">Remove</button>
                  )}
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Step 4: Review */}
        {step === 4 && (
          <div className="space-y-4">
            <div>
              <h2 className="font-semibold text-base">Review & publish</h2>
              <p className="text-sm text-agora-500 mt-0.5">Everything looks good? Hit publish to create your page.</p>
            </div>

            <div className="border border-agora-200 dark:border-agora-600 rounded-xl overflow-hidden">
              {/* Mini preview */}
              <div className="h-14 bg-gradient-to-r from-agora-600 to-agora-400 relative">
                {coverUrl && <img src={coverUrl} alt="" className="w-full h-full object-cover" />}
              </div>
              <div className="p-3 flex gap-3 items-start">
                <div className="w-10 h-10 rounded-lg bg-agora-200 dark:bg-agora-700 overflow-hidden flex-shrink-0 -mt-6 ring-2 ring-white dark:ring-agora-800">
                  {avatarUrl
                    ? <img src={avatarUrl} alt="" className="w-full h-full object-cover" />
                    : <span className="w-full h-full flex items-center justify-center font-bold text-agora-500 text-sm">
                        {displayName[0]}
                      </span>}
                </div>
                <div className="pt-1 min-w-0">
                  <p className="font-semibold text-sm">{displayName}</p>
                  {pageType && <p className="text-xs text-agora-400 capitalize">{pageType}</p>}
                  {bio && <p className="text-xs text-agora-500 mt-1 line-clamp-2">{bio}</p>}
                </div>
              </div>
            </div>

            {create.isError && (
              <p className="text-sm text-red-500">Something went wrong. Please try again.</p>
            )}
          </div>
        )}

        {/* Navigation */}
        <div className="flex gap-2 pt-2 border-t border-agora-100 dark:border-agora-700">
          {step > 0 && (
            <button onClick={back} className="btn-secondary flex items-center gap-1.5">
              <ArrowLeft size={14} /> Back
            </button>
          )}
          <div className="flex-1" />
          {step < 4 && (
            <button onClick={() => setStep(s => s + 1)} disabled={!canAdvance()}
              className="btn-secondary text-sm flex items-center gap-1.5">
              Skip <ArrowRight size={14} />
            </button>
          )}
          <button onClick={next} disabled={!canAdvance() || create.isPending}
            className="btn-primary flex items-center gap-1.5">
            {create.isPending ? 'Creating…' : step === 4 ? 'Publish page' : (
              <><span>Next</span> <ArrowRight size={14} /></>
            )}
          </button>
        </div>
      </div>
    </div>
  )
}
