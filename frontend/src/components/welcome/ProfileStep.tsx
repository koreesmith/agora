import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Camera, Globe } from 'lucide-react'
import { usersApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import type { StepProps } from './types'
import { StepFooter, StepHeader } from './WizardParts'
import { instanceNetworks } from './networks'

// AGORA-389: photo, display name, pronouns, and bio, with a live preview.
// Photos save as soon as they're picked, like Settings; the text fields save
// on Continue, and "Skip this step" leaves them as they were.

function initialsOf(name: string, fallback: string) {
  const words = name.trim().split(/\s+/).filter(Boolean)
  return (words.slice(0, 2).map(w => w[0]).join('') || fallback[0] || '?').toUpperCase()
}

export default function ProfileStep({ instance, me, next, back }: StepProps) {
  const { updateUser } = useAuthStore()
  const qc = useQueryClient()
  const [name, setName] = useState<string>(me.display_name || '')
  const [pronouns, setPronouns] = useState<string>(me.pronouns || '')
  const [bio, setBio] = useState<string>(me.bio || '')
  const [avatar, setAvatar] = useState<string>(me.avatar_url || '')
  const [cover, setCover] = useState<string>(me.cover_url || '')
  const [uploading, setUploading] = useState<'' | 'avatar' | 'cover'>('')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const upload = async (kind: 'avatar' | 'cover', file?: File) => {
    if (!file) return
    setUploading(kind); setErr('')
    try {
      if (kind === 'avatar') {
        const { data } = await usersApi.uploadAvatar(file)
        setAvatar(data.avatar_url); updateUser({ avatar_url: data.avatar_url })
      } else {
        const { data } = await usersApi.uploadCover(file)
        setCover(data.cover_url); updateUser({ cover_url: data.cover_url })
      }
      qc.invalidateQueries({ queryKey: ['my-profile'] })
    } catch (e: any) {
      setErr(e.response?.data?.error || 'That upload didn\'t work. Try a different image.')
    } finally {
      setUploading('')
    }
  }

  const save = async () => {
    const changes = { display_name: name.trim(), pronouns: pronouns.trim(), bio: bio.trim() }
    if (changes.display_name === (me.display_name || '') && changes.pronouns === (me.pronouns || '') && changes.bio === (me.bio || '')) {
      next(); return
    }
    setSaving(true); setErr('')
    try {
      await usersApi.updateProfile(changes)
      updateUser(changes)
      qc.invalidateQueries({ queryKey: ['my-profile'] })
      next()
    } catch (e: any) {
      setErr(e.response?.data?.error || 'Couldn\'t save your profile. Try again.')
    } finally {
      setSaving(false)
    }
  }

  const initials = initialsOf(name, me.username)
  const handleLine = `@${me.username}${pronouns.trim() ? ` · ${pronouns.trim()}` : ''}`
  const networks = instanceNetworks(instance)

  return (
    <>
      <StepHeader title="Put a face to your name">
        This is what people see when they find you. All of it is optional, and you can change it anytime from your profile.
      </StepHeader>

      {err && (
        <div role="alert" className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">
          {err}
        </div>
      )}

      <div className="flex flex-wrap gap-8 items-start">
        <div className="flex-[1_1_320px] min-w-0 space-y-5">
          <div className="flex items-center gap-4">
            <span aria-hidden="true" className="w-20 h-20 rounded-full bg-agora-700 text-white flex items-center justify-center text-2xl font-semibold flex-shrink-0 overflow-hidden">
              {avatar ? <img src={avatar} alt="" className="w-full h-full object-cover" /> : initials}
            </span>
            <div className="space-y-1.5">
              <label className={`btn-secondary min-h-[44px] cursor-pointer ${uploading === 'avatar' ? 'opacity-50 pointer-events-none' : ''}`}>
                <Camera size={18} aria-hidden="true" />
                {uploading === 'avatar' ? 'Uploading…' : avatar ? 'Change photo' : 'Upload a photo'}
                <input type="file" accept="image/*" className="sr-only"
                  onChange={e => { upload('avatar', e.target.files?.[0]); e.target.value = '' }} />
              </label>
              <p className="text-xs text-agora-600 dark:text-agora-400">A square image works best.</p>
            </div>
          </div>

          <div>
            <label htmlFor="welcome-display-name" className="label">Display name</label>
            <input id="welcome-display-name" className="input min-h-[44px]" value={name}
              onChange={e => setName(e.target.value)} autoComplete="name" />
          </div>
          <div>
            <label htmlFor="welcome-pronouns" className="label">
              Pronouns <span className="font-normal text-agora-500 dark:text-agora-400">(optional)</span>
            </label>
            <input id="welcome-pronouns" className="input min-h-[44px]" value={pronouns} maxLength={50}
              placeholder="e.g. she/her, he/him, they/them" autoComplete="off"
              onChange={e => setPronouns(e.target.value)} />
          </div>
          <div>
            <label htmlFor="welcome-bio" className="label">
              Bio <span className="font-normal text-agora-500 dark:text-agora-400">(optional)</span>
            </label>
            <textarea id="welcome-bio" className="input resize-y" rows={3} value={bio}
              placeholder="A line or two about you" onChange={e => setBio(e.target.value)} />
          </div>

          <p className="text-sm text-agora-600 dark:text-agora-400">
            Your username, <span className="font-semibold text-agora-800 dark:text-agora-100">@{me.username}</span>, is permanent. Everything else here can change.
          </p>
        </div>

        <div className="flex-[1_1_280px] min-w-0 space-y-3">
          <p className="text-xs font-semibold uppercase tracking-wide text-agora-600 dark:text-agora-400">Preview</p>
          <div className="rounded-xl border border-agora-200 dark:border-agora-700 overflow-hidden bg-white dark:bg-agora-800">
            <div className="h-28 bg-agora-100 dark:bg-agora-700 relative">
              {cover && <img src={cover} alt="" className="w-full h-full object-cover" />}
              <label className={`absolute right-2.5 top-2.5 inline-flex items-center gap-1.5 min-h-[36px] px-3 rounded-lg bg-white/90 dark:bg-agora-900/90 text-agora-800 dark:text-agora-100 text-xs font-semibold cursor-pointer ${uploading === 'cover' ? 'opacity-50 pointer-events-none' : ''}`}>
                <Camera size={15} aria-hidden="true" />
                {uploading === 'cover' ? 'Uploading…' : 'Cover photo'}
                <input type="file" accept="image/*" className="sr-only"
                  onChange={e => { upload('cover', e.target.files?.[0]); e.target.value = '' }} />
              </label>
            </div>
            <div className="px-5 pb-5 space-y-1">
              <span aria-hidden="true" className="-mt-9 w-[72px] h-[72px] rounded-full bg-agora-700 text-white border-4 border-white dark:border-agora-800 flex items-center justify-center text-xl font-semibold overflow-hidden">
                {avatar ? <img src={avatar} alt="" className="w-full h-full object-cover" /> : initials}
              </span>
              <p className="pt-1 text-lg font-bold text-agora-900 dark:text-agora-50 break-words">{name.trim() || me.username}</p>
              <p className="text-sm text-agora-600 dark:text-agora-400">{handleLine}</p>
              {bio.trim()
                ? <p className="pt-1.5 text-sm leading-relaxed text-agora-800 dark:text-agora-200 whitespace-pre-line break-words">{bio.trim()}</p>
                : <p className="pt-1.5 text-sm italic text-agora-500 dark:text-agora-400">Your bio will show here.</p>}
            </div>
          </div>
          {networks.length > 0 && (
            <div className="flex gap-2.5 items-start rounded-xl bg-agora-50 dark:bg-agora-900/40 px-3.5 py-3">
              <Globe size={18} className="flex-shrink-0 mt-0.5 text-agora-600 dark:text-agora-400" aria-hidden="true" />
              <p className="text-sm leading-relaxed text-agora-600 dark:text-agora-300">
                Your name, photo, and bio are public. If you're on {networks.join(' or ')} (coming up next), people there see them too.
              </p>
            </div>
          )}
        </div>
      </div>

      <StepFooter onBack={back} onSkip={next} onContinue={save} busy={saving} />
    </>
  )
}
