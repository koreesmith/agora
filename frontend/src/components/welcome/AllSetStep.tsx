import { useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, BookOpen, Check, Compass, Home, MessageCircle, PenLine, Rss, Smartphone, Users, Users2 } from 'lucide-react'
import { customDomainApi, notificationsApi } from '../../api'
import type { StepId, StepProps } from './types'
import { Callout, SettingRow, StepHeader, Switch } from './WizardParts'

// AGORA-391: the last step. A recap of what the account has now (read back
// from /auth/me, not from what was clicked), where things live, and two last
// preferences. Both ways out complete onboarding.

const APP_STORE_URL = 'https://apps.apple.com/us/app/agora-social-network/id6760935134'

const DM_LABELS: Record<string, string> = {
  everyone: 'Anyone can message you',
  friends:  'Only friends can message you',
  nobody:   'Nobody can message you',
}

function count(n: number, one: string, many: string) {
  return `${n} ${n === 1 ? one : many}`
}

function Place({ icon, name, children }: { icon: ReactNode; name: string; children: ReactNode }) {
  return (
    <li className="flex-[1_1_220px] flex gap-3 items-start rounded-xl border border-agora-100 dark:border-agora-700 p-4">
      <span aria-hidden="true" className="w-9 h-9 rounded-lg bg-agora-100 dark:bg-agora-700 text-agora-800 dark:text-agora-100 flex items-center justify-center flex-shrink-0">{icon}</span>
      <div className="min-w-0">
        <p className="font-semibold text-sm text-agora-900 dark:text-agora-50">{name}</p>
        <p className="text-sm leading-relaxed text-agora-600 dark:text-agora-400">{children}</p>
      </div>
    </li>
  )
}

export default function AllSetStep({ steps, instance, me, notes, goTo, back, finish, finishing }: StepProps) {
  const qc = useQueryClient()
  const has = (id: StepId) => steps.some(s => s.id === id)
  const firstName = (me.display_name || '').trim().split(/\s+/)[0] || me.username

  const { data: domainData } = useQuery({
    queryKey: ['custom-domain'],
    queryFn: () => customDomainApi.get().then(r => r.data),
    enabled: has('bluesky'),
  })
  const blueskyHandle = `@${domainData?.current_handle || `${me.username}.${instance.domain}`}`

  const profileParts = [me.avatar_url && 'photo', me.display_name && 'name', me.bio && 'bio'].filter(Boolean) as string[]
  const profileSummary = profileParts.length
    ? `${profileParts.join(', ').replace(/, ([^,]*)$/, ' and $1').replace(/^./, c => c.toUpperCase())} added`
    : 'Nothing added yet'
  const peopleParts = [
    notes.requestsSent > 0 && count(notes.requestsSent, 'friend request sent', 'friend requests sent'),
    notes.follows > 0 && count(notes.follows, 'account followed', 'accounts followed'),
    notes.groupsJoined > 0 && count(notes.groupsJoined, 'group joined', 'groups joined'),
  ].filter(Boolean) as string[]

  const rows: { id: StepId; label: string; value: string }[] = [
    { id: 'profile', label: 'Profile', value: profileSummary },
    { id: 'privacy', label: 'Privacy', value: `${me.profile_private ? 'Private' : 'Public'} profile · ${DM_LABELS[me.dm_privacy] || DM_LABELS.everyone}` },
    ...(has('bluesky') ? [{ id: 'bluesky' as StepId, label: 'Bluesky', value: me.atproto_enabled ? `On, as ${blueskyHandle}` : 'Off' }] : []),
    ...(has('fediverse') ? [{ id: 'fediverse' as StepId, label: 'Fediverse', value: me.activitypub_enabled ? `On, as @${me.username}@${instance.domain}` : 'Off' }] : []),
    { id: 'people', label: 'People', value: peopleParts.join(' · ') || 'No one added yet' },
  ]

  // ── Preferences ──────────────────────────────────────────────────────────
  const { data: emailPrefs } = useQuery({
    queryKey: ['email-prefs'],
    queryFn: () => notificationsApi.getEmailPrefs().then(r => r.data),
  })
  const [emailOverride, setEmailOverride] = useState<boolean | null>(null)
  const emailOn = emailOverride ?? emailPrefs?.email_notifications_enabled ?? true
  const [dark, setDark] = useState(() => localStorage.getItem('theme') === 'dark')
  const [err, setErr] = useState('')

  const setEmail = async (v: boolean) => {
    setEmailOverride(v); setErr('')
    try {
      await notificationsApi.updateEmailPrefs(v)
      qc.invalidateQueries({ queryKey: ['email-prefs'] })
    } catch (e: any) {
      setEmailOverride(!v)
      setErr(e.response?.data?.error || 'Couldn\'t save that change. Try again.')
    }
  }
  const setTheme = (v: boolean) => {
    setDark(v)
    localStorage.setItem('theme', v ? 'dark' : 'light')
    document.documentElement.classList.toggle('dark', v)
  }

  return (
    <>
      <div className="space-y-3">
        <span aria-hidden="true" className="w-12 h-12 rounded-full bg-agora-700 dark:bg-agora-300 text-white dark:text-agora-900 flex items-center justify-center">
          <Check size={26} strokeWidth={2.5} />
        </span>
        <StepHeader title={`You're all set, ${firstName}`}>
          Here's a quick recap. Anything here can be changed later in Settings.
        </StepHeader>
      </div>

      <div className="space-y-3">
        <dl className="rounded-xl border border-agora-100 dark:border-agora-700 divide-y divide-agora-100 dark:divide-agora-700">
          {rows.map(r => (
            <div key={r.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-3">
              <dt className="basis-28 flex-shrink-0 text-sm font-semibold text-agora-600 dark:text-agora-400">{r.label}</dt>
              <dd className="flex-[1_1_220px] min-w-0 text-sm text-agora-900 dark:text-agora-100 break-words">{r.value}</dd>
              <dd>
                <button type="button" onClick={() => goTo(r.id)}
                  className="min-h-[36px] px-2 text-sm font-semibold text-agora-700 dark:text-agora-200 underline-offset-2 hover:underline">
                  Change<span className="sr-only"> {r.label}</span>
                </button>
              </dd>
            </div>
          ))}
        </dl>
        {notes.domainReminder && me.atproto_enabled && has('bluesky') && (
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 rounded-xl px-4 py-3 bg-blue-50 dark:bg-blue-900/30 text-sm text-agora-800 dark:text-agora-100">
            <p><strong className="font-semibold">Saved for later:</strong> use your own domain as your Bluesky handle.</p>
            <button type="button" onClick={() => finish('/settings?tab=bluesky')} disabled={finishing}
              className="min-h-[36px] font-semibold text-blue-700 dark:text-blue-300 hover:underline">
              Set it up in Settings
            </button>
          </div>
        )}
      </div>

      <div className="space-y-3.5">
        <h2 className="font-semibold text-agora-900 dark:text-agora-50">Where things live</h2>
        <ul className="flex flex-wrap gap-3">
          <Place icon={<Home size={18} />} name="Feed">Posts from your friends, groups, and pages.</Place>
          <Place icon={<Users size={18} />} name="Connections">
            Friends, requests{has('bluesky') || has('fediverse') ? ', and your Bluesky and Fediverse follows' : ', and friend lists'}.
          </Place>
          <Place icon={<MessageCircle size={18} />} name="Messages">Private, real-time conversations.</Place>
          <Place icon={<Users2 size={18} />} name="Groups">Communities around shared interests, public or private.</Place>
          <Place icon={<BookOpen size={18} />} name="Pages">Follow local spots, creators, and projects, or start your own.</Place>
          <Place icon={<Rss size={18} />} name="My Feeds">Build your own feeds from friends, lists, and follows.</Place>
        </ul>
      </div>

      {err && (
        <div role="alert" className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">
          {err}
        </div>
      )}

      <div className="flex flex-wrap gap-4">
        <div className="flex-[1_1_260px] rounded-xl border border-agora-100 dark:border-agora-700 p-5 space-y-2.5">
          <span aria-hidden="true" className="w-9 h-9 rounded-lg bg-agora-100 dark:bg-agora-700 text-agora-800 dark:text-agora-100 flex items-center justify-center"><Smartphone size={18} /></span>
          <h2 className="font-semibold text-agora-900 dark:text-agora-50">Take Agora with you</h2>
          <p className="text-sm leading-relaxed text-agora-600 dark:text-agora-400">The Agora app for iPhone and iPad is on the App Store. Android is coming soon.</p>
          <a href={APP_STORE_URL} target="_blank" rel="noopener noreferrer" className="btn-secondary dark:bg-transparent dark:ring-1 dark:ring-inset dark:ring-agora-500 dark:hover:bg-agora-700 min-h-[44px]">Get the iOS app</a>
        </div>
        <div className="flex-[1_1_260px] rounded-xl border border-agora-100 dark:border-agora-700 px-5 pt-4 pb-1">
          <h2 className="font-semibold text-agora-900 dark:text-agora-50">Two last preferences</h2>
          <SettingRow id="welcome-email-label" title="Email notifications"
            description="Likes, comments, and friend requests."
            control={<Switch checked={emailOn} onChange={setEmail} labelledBy="welcome-email-label" />} />
          <SettingRow id="welcome-dark-label" title="Dark mode" last
            description="Also at the bottom of the sidebar, under the moon icon."
            control={<Switch checked={dark} onChange={setTheme} labelledBy="welcome-dark-label" />} />
        </div>
      </div>

      <div className="space-y-4 pt-6 border-t border-agora-100 dark:border-agora-700">
        <div className="flex flex-wrap items-center gap-3">
          <button type="button" onClick={back} className="btn-ghost min-h-[48px]"><ArrowLeft size={18} /> Back</button>
          <span className="flex-1" />
          <button type="button" onClick={() => finish('/feed')} disabled={finishing} className="btn-secondary dark:bg-transparent dark:ring-1 dark:ring-inset dark:ring-agora-500 dark:hover:bg-agora-700 min-h-[48px] px-5 text-base">
            Go to my feed
          </button>
          <button type="button" onClick={() => finish('/feed?compose=1')} disabled={finishing} className="btn-primary min-h-[48px] px-5 text-base">
            <PenLine size={18} aria-hidden="true" /> {me.onboarding_returning ? 'Write a post' : 'Write your first post'}
          </button>
        </div>
        <p className="flex items-center gap-2 text-sm text-agora-600 dark:text-agora-400">
          <Compass size={16} aria-hidden="true" className="flex-shrink-0" />
          Want a refresher later? Replay this tour from the compass icon at the bottom of the sidebar.
        </p>
      </div>
    </>
  )
}
