import { useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AtSign, Check, Cloud, Copy, Lock } from 'lucide-react'
import { customDomainApi, usersApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import type { StepProps } from './types'
import { Callout, SettingRow, StepFooter, StepHeader, Switch } from './WizardParts'

// AGORA-390: the Bluesky and Fediverse steps. Same shape for both: the
// account's handle there, an on/off choice, what crosses over and what never
// does, and the per-network notification switch. Every change saves at once,
// the same as Settings → Bluesky / Settings → Fediverse.

type Network = 'bluesky' | 'fediverse'

interface NetworkCopy {
  name: string
  icon: ReactNode
  enabledKey: 'atproto_enabled' | 'activitypub_enabled'
  notifyKey: 'atproto_notifications_enabled' | 'fediverse_notifications_enabled'
  accent: { badge: string; handle: string; check: string; text: string }
  title: string
  lead: string
  handleNote: string
  yes: string
  yesDesc: string
  no: string
  goes: string[]
  goodToKnow: string
  notifyTitle: string
  notifyDesc: string
  off: string
}

const COPY: Record<Network, NetworkCopy> = {
  bluesky: {
    name: 'Bluesky',
    icon: <Cloud size={22} />,
    enabledKey: 'atproto_enabled',
    notifyKey: 'atproto_notifications_enabled',
    accent: {
      badge: 'bg-blue-50 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300',
      handle: 'bg-blue-50 dark:bg-blue-900/30',
      check: 'text-blue-700 dark:text-blue-300',
      text: 'text-blue-700 dark:text-blue-300',
    },
    title: 'You already have a Bluesky account',
    lead: 'Every Agora account is also a real Bluesky account, with no bridge and no second sign-up. Agora runs the same open technology Bluesky does (the AT Protocol), so your public posts can show up there directly.',
    handleNote: 'People on Bluesky can find you by searching for this.',
    yes: 'Yes, put me on Bluesky',
    yesDesc: 'People on Bluesky can follow you, see your public posts, and reply, like, and repost.',
    no: 'No, keep me off Bluesky',
    goes: ['Your public posts', 'Your replies, likes, reposts, and quotes of Bluesky posts', 'Your name, photo, and bio'],
    goodToKnow: 'Bluesky has a shorter length limit, so very long posts are trimmed there with a link back to the full post. Content warnings show up as a general warning label, without your wording.',
    notifyTitle: 'Bluesky post notifications',
    notifyDesc: "Lets you get notified when Bluesky accounts you follow post. You'll pick which accounts with the bell next to each one.",
    // AGORA-386: following a Bluesky account is a record in the account's
    // own repo, so it needs AT Proto on (followBlueskyActor 404s otherwise).
    off: "Your posts stay on Agora, and you won't be able to follow Bluesky accounts while it's off. Turn it on anytime in Settings → Bluesky.",
  },
  fediverse: {
    name: 'Fediverse',
    icon: <AtSign size={22} />,
    enabledKey: 'activitypub_enabled',
    notifyKey: 'fediverse_notifications_enabled',
    accent: {
      badge: 'bg-violet-50 text-violet-700 dark:bg-violet-900/40 dark:text-violet-300',
      handle: 'bg-violet-50 dark:bg-violet-900/30',
      check: 'text-violet-700 dark:text-violet-300',
      text: 'text-violet-700 dark:text-violet-300',
    },
    title: "You're on the Fediverse, too",
    lead: 'Agora speaks ActivityPub, the open protocol behind Mastodon, Pixelfed, and many other apps. That makes your Agora account a Fediverse account as well, so people there can follow you without leaving their app.',
    handleNote: 'Anyone on Mastodon or another Fediverse app can follow you with this.',
    yes: 'Yes, put me on the Fediverse',
    yesDesc: 'People on Mastodon and similar apps can follow you, see your public posts, and reply, boost, and quote.',
    no: 'No, keep me off the Fediverse',
    goes: ['Your public posts, including later edits and deletes', 'Your replies and quotes of Fediverse posts', 'Your name, photo, and bio'],
    goodToKnow: "Content warnings carry over with your own wording. You can also mention Fediverse people in a post, like @someone@mastodon.social, and they'll be notified on their own server.",
    notifyTitle: 'Fediverse post notifications',
    notifyDesc: "Lets you get notified when Fediverse accounts you follow post. You'll pick which accounts with the bell next to each one.",
    off: "Your posts stay on Agora, and people on Mastodon and similar apps won't see you. Turn it on anytime in Settings → Fediverse.",
  },
}

const STAYS = ['Friends-only, friend list, and only-me posts', 'Your friendships and friend lists', 'Your direct messages']

function NetworkStep({ network, steps, instance, me, notes, setNotes, next, back, goTo }: StepProps & { network: Network }) {
  const c = COPY[network]
  const { updateUser } = useAuthStore()
  const qc = useQueryClient()
  const [on, setOn] = useState<boolean>(!!me[c.enabledKey])
  const [notify, setNotify] = useState<boolean>(!!me[c.notifyKey])
  const [copied, setCopied] = useState(false)
  const [err, setErr] = useState('')

  // A live custom domain replaces the instance handle on Bluesky (AGORA-278).
  const { data: domainData } = useQuery({
    queryKey: ['custom-domain'],
    queryFn: () => customDomainApi.get().then(r => r.data),
    enabled: network === 'bluesky',
  })
  const handle = network === 'bluesky'
    ? `@${domainData?.current_handle || `${me.username}.${instance.domain}`}`
    : `@${me.username}@${instance.domain}`
  const hasCustomDomain = !!domainData && domainData.current_handle !== domainData.fallback_handle
  const fediverseNext = network === 'bluesky' && steps.some(s => s.id === 'fediverse')

  const save = async (patch: Record<string, boolean>, undo: () => void) => {
    setErr('')
    try {
      await usersApi.updateProfile(patch)
      updateUser(patch)
      qc.invalidateQueries({ queryKey: ['my-profile'] })
    } catch (e: any) {
      undo()
      setErr(e.response?.data?.error || 'Couldn\'t save that change. Try again.')
    }
  }
  const choose = (v: boolean) => {
    if (v === on) return
    setOn(v)
    save({ [c.enabledKey]: v }, () => setOn(!v))
  }
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(handle)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch { /* clipboard unavailable; the handle is still on screen */ }
  }

  const choiceClass = (selected: boolean) => `flex-[1_1_260px] flex gap-3 items-start rounded-xl px-4 py-4 cursor-pointer border-2 transition-colors ${selected
    ? 'border-agora-700 bg-agora-50 dark:border-agora-300 dark:bg-agora-900/40'
    : 'border-agora-100 hover:border-agora-200 dark:border-agora-700 dark:hover:border-agora-600'}`

  return (
    <>
      <StepHeader title={c.title} eyebrow={
        <div className="flex items-center gap-2.5">
          <span aria-hidden="true" className={`w-10 h-10 rounded-xl flex items-center justify-center ${c.accent.badge}`}>{c.icon}</span>
          <span className={`text-xs font-semibold uppercase tracking-wide ${c.accent.text}`}>{c.name}</span>
        </div>
      }>
        {c.lead}{fediverseNext && ' The Fediverse is a separate network; that\'s the next step.'}
      </StepHeader>

      {me.profile_private && (
        <Callout tone="warning">
          <strong className="font-semibold">Your profile is private, so {network === 'bluesky' ? 'Bluesky' : 'the Fediverse'} can't see you right now.</strong>{' '}
          Even with this on, nobody there can find or follow you until your profile is public.{' '}
          <button type="button" onClick={() => goTo('privacy')} className="font-semibold underline">Change it in Privacy</button>
        </Callout>
      )}

      {err && (
        <div role="alert" className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">
          {err}
        </div>
      )}

      <div className={`flex flex-wrap items-center justify-between gap-4 rounded-xl px-5 py-4 border border-agora-100 dark:border-agora-700 ${on ? c.accent.handle : 'bg-agora-50 dark:bg-agora-900/40'}`}>
        <div className="min-w-0 space-y-1">
          <p className="text-xs font-semibold text-agora-600 dark:text-agora-400">Your {c.name} handle</p>
          <p className={`text-xl font-semibold break-all ${on ? 'text-agora-900 dark:text-agora-50' : 'text-agora-500 dark:text-agora-400'}`}>{handle}</p>
          <p className="text-sm text-agora-600 dark:text-agora-400">{on ? c.handleNote : `Not in use while ${network === 'bluesky' ? 'Bluesky' : 'the Fediverse'} is off.`}</p>
        </div>
        <button type="button" onClick={copy}
          className="inline-flex items-center gap-2 min-h-[44px] px-4 rounded-lg border border-agora-200 dark:border-agora-600 bg-white dark:bg-agora-800 text-sm font-semibold text-agora-800 dark:text-agora-100">
          {copied ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>

      <fieldset className="min-w-0 space-y-3">
        <legend className="mb-3 font-semibold text-agora-900 dark:text-agora-50">
          Do you want your public posts on {network === 'bluesky' ? 'Bluesky' : 'the Fediverse'}?
        </legend>
        <div className="flex flex-wrap gap-3">
          <label className={choiceClass(on)}>
            <input type="radio" name={`welcome-${network}`} checked={on} onChange={() => choose(true)}
              className="w-5 h-5 mt-0.5 flex-shrink-0 accent-agora-700" />
            <span className="space-y-1">
              <span className="flex flex-wrap items-center gap-2">
                <span className="font-semibold text-agora-900 dark:text-agora-50">{c.yes}</span>
                <span className="text-xs font-semibold px-2 py-0.5 rounded-full bg-agora-100 dark:bg-agora-700 text-agora-800 dark:text-agora-100">Default</span>
              </span>
              <span className="block text-sm leading-relaxed text-agora-600 dark:text-agora-400">{c.yesDesc}</span>
            </span>
          </label>
          <label className={choiceClass(!on)}>
            <input type="radio" name={`welcome-${network}`} checked={!on} onChange={() => choose(false)}
              className="w-5 h-5 mt-0.5 flex-shrink-0 accent-agora-700" />
            <span className="space-y-1">
              <span className="block font-semibold text-agora-900 dark:text-agora-50">{c.no}</span>
              <span className="block text-sm leading-relaxed text-agora-600 dark:text-agora-400">
                Nothing you post reaches {network === 'bluesky' ? 'Bluesky' : 'the Fediverse'}. You can change your mind anytime in Settings.
              </span>
            </span>
          </label>
        </div>
      </fieldset>

      {on ? (
        <>
          <div className="flex flex-wrap gap-4">
            <div className="flex-[1_1_260px] rounded-xl border border-agora-100 dark:border-agora-700 px-5 py-4 space-y-2.5">
              <h2 className="font-semibold text-sm text-agora-900 dark:text-agora-50">Goes to {network === 'bluesky' ? 'Bluesky' : 'the Fediverse'}</h2>
              <ul className="space-y-2">
                {c.goes.map(g => (
                  <li key={g} className="flex gap-2.5 text-sm leading-relaxed text-agora-800 dark:text-agora-200">
                    <Check size={18} strokeWidth={2.5} className={`flex-shrink-0 mt-0.5 ${c.accent.check}`} aria-hidden="true" />{g}
                  </li>
                ))}
              </ul>
            </div>
            <div className="flex-[1_1_260px] rounded-xl border border-agora-100 dark:border-agora-700 px-5 py-4 space-y-2.5">
              <h2 className="font-semibold text-sm text-agora-900 dark:text-agora-50">Always stays on Agora</h2>
              <ul className="space-y-2">
                {STAYS.map(s => (
                  <li key={s} className="flex gap-2.5 text-sm leading-relaxed text-agora-800 dark:text-agora-200">
                    <Lock size={18} className="flex-shrink-0 mt-0.5 text-agora-600 dark:text-agora-400" aria-hidden="true" />{s}
                  </li>
                ))}
              </ul>
            </div>
          </div>

          <Callout tone="info">
            <strong className="font-semibold">Good to know:</strong> {c.goodToKnow}
          </Callout>

          <div>
            <h2 className="font-semibold text-agora-900 dark:text-agora-50 mb-1.5">Optional extras</h2>
            <SettingRow id={`welcome-${network}-notify-label`} title={c.notifyTitle} description={c.notifyDesc}
              last={network !== 'bluesky' || hasCustomDomain}
              control={<Switch checked={notify} labelledBy={`welcome-${network}-notify-label`}
                onChange={v => { setNotify(v); save({ [c.notifyKey]: v }, () => setNotify(!v)) }} />} />
            {network === 'bluesky' && !hasCustomDomain && (
              <SettingRow id="welcome-domain-label" title="Use your own domain as your handle" last
                description={`Be @${me.username}.example.com, or any domain you own, instead. You'll need to add one DNS record, so it's easiest to do later.`}
                control={notes.domainReminder
                  ? <span className="inline-flex items-center gap-2 min-h-[44px] px-4 text-sm font-semibold text-agora-700 dark:text-agora-200"><Check size={16} aria-hidden="true" /> Reminder saved</span>
                  : <button type="button" onClick={() => setNotes({ domainReminder: true })} className="btn-secondary dark:bg-transparent dark:ring-1 dark:ring-inset dark:ring-agora-500 dark:hover:bg-agora-700 min-h-[44px]">Remind me on the last step</button>} />
            )}
          </div>
        </>
      ) : (
        <Callout tone="info">
          <strong className="font-semibold">{network === 'bluesky' ? 'Bluesky is off.' : 'The Fediverse is off.'}</strong> {c.off}
        </Callout>
      )}

      <StepFooter onBack={back} onContinue={next} />
    </>
  )
}

export function BlueskyStep(props: StepProps) {
  return <NetworkStep {...props} network="bluesky" />
}

export function FediverseStep(props: StepProps) {
  return <NetworkStep {...props} network="fediverse" />
}
