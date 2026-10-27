import { useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Globe, List, Lock, Users } from 'lucide-react'
import { usersApi } from '../../api'
import { useAuthStore } from '../../store/auth'
import type { StepProps } from './types'
import { Callout, SettingRow, StepFooter, StepHeader, Switch } from './WizardParts'
import { instanceNetworks } from './networks'

// AGORA-389: how post audiences work, then the profile-wide privacy settings.
// Each setting saves as soon as it changes, the same as Settings → Privacy.

type DmPrivacy = 'everyone' | 'friends' | 'nobody'

const DM_OPTIONS: { value: DmPrivacy; label: string }[] = [
  { value: 'everyone', label: 'Everyone' },
  { value: 'friends',  label: 'Friends only' },
  { value: 'nobody',   label: 'Nobody' },
]

function Audience({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <div className="flex-[1_1_170px] rounded-xl border border-agora-100 dark:border-agora-700 bg-agora-50 dark:bg-agora-900/40 p-4 space-y-2">
      <span aria-hidden="true" className="text-agora-700 dark:text-agora-300">{icon}</span>
      <p className="font-semibold text-sm text-agora-900 dark:text-agora-50">{title}</p>
      <p className="text-sm leading-relaxed text-agora-600 dark:text-agora-400">{children}</p>
    </div>
  )
}

export default function PrivacyStep({ instance, me, next, back }: StepProps) {
  const { updateUser } = useAuthStore()
  const qc = useQueryClient()
  const [priv, setPriv] = useState<boolean>(!!me.profile_private)
  const [wall, setWall] = useState<boolean>(!!me.wall_approval_required)
  const [dm, setDm] = useState<DmPrivacy>(me.dm_privacy || 'everyone')
  const [err, setErr] = useState('')
  const networks = instanceNetworks(instance)

  // Optimistic: flip the control now, put it back if the save fails.
  const save = async (patch: Record<string, unknown>, undo: () => void) => {
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

  return (
    <>
      <StepHeader title="Decide who sees what">
        You're in control at two levels: each post you write, and your profile as a whole.
      </StepHeader>

      <div className="space-y-3.5">
        <div className="space-y-1">
          <h2 className="font-semibold text-agora-900 dark:text-agora-50">Every post picks its own audience</h2>
          <p className="text-sm text-agora-600 dark:text-agora-400">You choose one each time you post, right in the composer.</p>
        </div>
        <div className="flex flex-wrap gap-3">
          <Audience icon={<Globe size={22} />} title="Public">
            Anyone{networks.length > 0 ? `, including ${networks.join(' and ')} if you're on them` : ' who visits'}.
          </Audience>
          <Audience icon={<Users size={22} />} title="Friends">
            Only people you're friends with.{networks.length > 0 && ' Never leaves Agora.'}
          </Audience>
          <Audience icon={<List size={22} />} title="A friend list">
            Just one list you've made, like Family or Book club.
          </Audience>
          <Audience icon={<Lock size={22} />} title="Only me">
            Nobody else. Handy for notes to yourself.
          </Audience>
        </div>
      </div>

      <div>
        <h2 className="font-semibold text-agora-900 dark:text-agora-50 mb-1.5">Your profile</h2>

        {err && (
          <div role="alert" className="my-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">
            {err}
          </div>
        )}

        <SettingRow id="welcome-private-label" title="Private profile"
          description="Only friends can see your profile page and timeline."
          control={<Switch checked={priv} labelledBy="welcome-private-label"
            onChange={v => { setPriv(v); save({ profile_private: v }, () => setPriv(!v)) }} />}>
          {priv && networks.length > 0 && (
            <Callout tone="warning">
              <strong className="font-semibold">This also hides you from {networks.join(' and ')}.</strong>{' '}
              People there won't be able to find or follow you, even if you turn {networks.length > 1 ? 'them' : 'it'} on
              in the next {networks.length > 1 ? 'two steps' : 'step'}.
            </Callout>
          )}
        </SettingRow>

        <fieldset className="py-3.5 border-b border-agora-100 dark:border-agora-700 flex flex-wrap items-center justify-between gap-x-4 gap-y-3 min-w-0">
          <legend className="sr-only">Who can message you</legend>
          <div className="min-w-0 flex-1 basis-64" aria-hidden="true">
            <p className="font-medium text-sm text-agora-900 dark:text-agora-100">Who can message you</p>
            <p className="text-sm text-agora-600 dark:text-agora-400 leading-relaxed">Direct messages are always private between the people in them.</p>
          </div>
          <div className="flex gap-1 p-1 rounded-xl bg-agora-50 dark:bg-agora-900 border border-agora-100 dark:border-agora-700">
            {DM_OPTIONS.map(o => (
              <button key={o.value} type="button" aria-pressed={dm === o.value}
                onClick={() => {
                  if (dm === o.value) return
                  const prev = dm
                  setDm(o.value)
                  save({ dm_privacy: o.value }, () => setDm(prev))
                }}
                className={`min-h-[40px] px-3.5 rounded-lg text-sm font-semibold transition-colors ${dm === o.value
                  ? 'bg-white dark:bg-agora-700 text-agora-900 dark:text-agora-50 shadow-sm'
                  : 'text-agora-600 dark:text-agora-400 hover:text-agora-900 dark:hover:text-agora-100'}`}>
                {o.label}
              </button>
            ))}
          </div>
        </fieldset>

        <SettingRow id="welcome-wall-label" title="Approve wall posts" last
          description="Review what friends post on your wall before it appears."
          control={<Switch checked={wall} labelledBy="welcome-wall-label"
            onChange={v => { setWall(v); save({ wall_approval_required: v }, () => setWall(!v)) }} />} />

        <p className="mt-2 text-sm text-agora-600 dark:text-agora-400">
          More options, like hiding your timeline or blocking people, live in{' '}
          <a href="/settings?tab=privacy" target="_blank" rel="noopener noreferrer" className="font-semibold text-agora-700 dark:text-agora-200 underline">
            Settings → Privacy
          </a>.
        </p>
      </div>

      <StepFooter onBack={back} onContinue={next} />
    </>
  )
}
