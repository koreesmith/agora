import type { ReactNode } from 'react'
import { ArrowRight, Eye, Globe, Users, Users2 } from 'lucide-react'
import type { StepProps } from './types'
import { instanceNetworks } from './networks'

// AGORA-389: the first step, shown on its own without the step rail: what
// Agora is, and what the rest of the wizard covers. AGORA-393: an account
// that predates the wizard gets it once as a re-introduction, so it's greeted
// as a returning member and told nothing changes unless it changes it.

function Pillar({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <div className="card flex-[1_1_240px] p-6 space-y-3">
      <span aria-hidden="true" className="w-11 h-11 rounded-xl bg-agora-100 dark:bg-agora-700 text-agora-800 dark:text-agora-100 flex items-center justify-center">
        {icon}
      </span>
      <h2 className="font-semibold text-agora-900 dark:text-agora-50">{title}</h2>
      <p className="text-sm leading-relaxed text-agora-600 dark:text-agora-300">{children}</p>
    </div>
  )
}

export default function WelcomeStep({ instance, me, steps, next, finish, finishing }: StepProps) {
  const instanceName = instance.instance_name || 'Agora'
  const firstName = (me.display_name || '').trim().split(/\s+/)[0] || me.username
  const ahead = steps.filter(s => s.id !== 'welcome' && s.id !== 'done')
  const networks = instanceNetworks(instance)
  const accountKinds = networks.map(n => n === 'Bluesky' ? 'a Bluesky account' : 'a Fediverse account').join(' and ')
  const reach = networks.map(n => n === 'Bluesky' ? 'Bluesky' : 'Mastodon').join(' and ')
  const returning = !!me.onboarding_returning

  return (
    <div className="space-y-10">
      <div className="space-y-4 max-w-2xl">
        {(returning || instance.domain) && (
          <p className="text-xs font-semibold uppercase tracking-wide text-agora-600 dark:text-agora-400">
            {returning ? 'A quick re-introduction' : `Welcome to ${instance.domain}`}
          </p>
        )}
        <h1 id="step-title" className="text-3xl md:text-4xl font-bold leading-tight text-agora-900 dark:text-agora-50">
          Hi {firstName}, welcome {returning && 'back '}to {instanceName}
        </h1>
        <p className="text-lg leading-relaxed text-agora-700 dark:text-agora-200">
          {instanceName} is a social network for the people you actually know.{' '}
          {instanceName === 'Agora'
            ? "It's open source and privacy-first"
            : "It runs on Agora, which is open source and privacy-first"}
          {instance.domain ? `, and it's run by the people behind ${instance.domain} rather than a big company` : ''}.
          {' '}{returning ? "Here's a refresher on what makes it different." : "Here's what makes it different."}
        </p>
      </div>

      <div className="flex flex-wrap gap-5">
        <Pillar icon={<Users size={22} />} title="Friends, not followers">
          Friendships here are mutual. You both say yes before either of you sees the other's friends-only posts.
        </Pillar>
        <Pillar icon={<Eye size={22} />} title="You pick who sees each post">
          Any post can be public, friends-only, for one of your friend lists, or just for you.
          {networks.length > 0 && ' Only public posts ever leave Agora.'}
        </Pillar>
        {networks.length > 0 ? (
          <Pillar icon={<Globe size={22} />} title={networks.length === 2 ? 'One account, three networks' : 'One account, two networks'}>
            Your account is also {accountKinds}, so your public posts can reach people on {reach} too.
            Whether they do is up to you.
          </Pillar>
        ) : (
          <Pillar icon={<Users2 size={22} />} title="Groups and Pages">
            Join communities around what you care about, and follow pages for local spots, creators, and projects.
          </Pillar>
        )}
      </div>

      <div className="card p-6 space-y-4">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 className="font-semibold text-agora-900 dark:text-agora-50">{returning ? "Here's what we'll go over" : "Here's what we'll set up"}</h2>
          <p className="text-sm text-agora-600 dark:text-agora-400">
            {returning
              ? 'About 3 minutes. Each step starts from your current settings, and nothing changes unless you change it.'
              : 'About 3 minutes. Everything can be changed later in Settings.'}
          </p>
        </div>
        <ol className="flex flex-wrap gap-2.5">
          {ahead.map((s, i) => (
            <li key={s.id} className="flex items-center gap-2 py-2 pl-2 pr-3.5 rounded-full bg-agora-50 dark:bg-agora-900 border border-agora-100 dark:border-agora-700 text-sm font-medium text-agora-800 dark:text-agora-100">
              <span aria-hidden="true" className="w-6 h-6 rounded-full bg-white dark:bg-agora-800 border-[1.5px] border-agora-300 dark:border-agora-600 flex items-center justify-center text-xs font-semibold text-agora-700 dark:text-agora-200">
                {i + 1}
              </span>
              {s.label}
            </li>
          ))}
        </ol>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <button type="button" onClick={next} className="btn-primary min-h-[48px] px-6 text-base">
          Let's get started <ArrowRight size={18} />
        </button>
        <button type="button" onClick={() => finish('/feed')} disabled={finishing} className="btn-ghost min-h-[48px]">
          Skip for now and go to my feed
        </button>
      </div>
    </div>
  )
}
