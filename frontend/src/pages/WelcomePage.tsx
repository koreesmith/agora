import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { authApi, instanceApi, onboardingApi } from '../api'
import { useAuthStore } from '../store/auth'
import { markWhatsNewSeen } from '../components/common/WhatsNewModal'
import { markWelcomeShown } from '../components/welcome/wizardSession'
import type { InstanceInfo, StepDef, StepId, StepProps, WizardNotes } from '../components/welcome/types'
import PlaceholderStep from '../components/welcome/PlaceholderStep'

// AGORA-388: the getting started wizard (AGORA-386). New accounts land here
// from Layout after their first sign-in; anyone can come back through the
// sidebar's Getting started icon. The current step lives in ?step= so Back,
// reloads, and the recap's Change links all work.

const ALL_STEPS: StepDef[] = [
  { id: 'welcome',   label: 'Welcome',      Component: PlaceholderStep, bare: true },
  { id: 'profile',   label: 'Your profile', Component: PlaceholderStep },
  { id: 'privacy',   label: 'Privacy',      Component: PlaceholderStep },
  { id: 'bluesky',   label: 'Bluesky',      Component: PlaceholderStep },
  { id: 'fediverse', label: 'Fediverse',    Component: PlaceholderStep },
  { id: 'people',    label: 'Find people',  Component: PlaceholderStep },
  { id: 'done',      label: 'All set',      Component: PlaceholderStep },
]

const NOTES_KEY = 'agora_welcome_notes'
const EMPTY_NOTES: WizardNotes = { domainReminder: false, requestsSent: 0, follows: 0, groupsJoined: 0 }

function loadNotes(): WizardNotes {
  try { return { ...EMPTY_NOTES, ...JSON.parse(sessionStorage.getItem(NOTES_KEY) || '{}') } }
  catch { return EMPTY_NOTES }
}

export default function WelcomePage() {
  const { updateUser } = useAuthStore()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const [notes, setNotesState] = useState<WizardNotes>(loadNotes)
  const [finishing, setFinishing] = useState(false)

  useEffect(() => {
    markWelcomeShown()
    // This page sits outside Layout, which is what normally applies the
    // saved theme.
    document.documentElement.classList.toggle('dark', localStorage.getItem('theme') === 'dark')
  }, [])

  const { data: instance } = useQuery<InstanceInfo>({
    queryKey: ['instance-info'],
    queryFn: () => instanceApi.getInfo().then(r => r.data),
    staleTime: 5 * 60_000,
  })
  const { data: me } = useQuery({
    queryKey: ['my-profile'],
    queryFn: () => authApi.me().then(r => r.data),
  })

  // A network the admin has turned off for the whole instance has nothing
  // to set up, so its step isn't shown or counted.
  const steps = useMemo(() => ALL_STEPS.filter(s =>
    (s.id !== 'bluesky' || instance?.atproto_enabled) &&
    (s.id !== 'fediverse' || instance?.activitypub_enabled)
  ), [instance])

  const index = Math.max(0, steps.findIndex(s => s.id === params.get('step')))
  const step = steps[index]

  const goTo = (id: StepId) => {
    setParams({ step: id })
    window.scrollTo(0, 0)
  }
  const next = () => { if (index < steps.length - 1) goTo(steps[index + 1].id) }
  const back = () => { if (index > 0) goTo(steps[index - 1].id) }

  const setNotes: StepProps['setNotes'] = (update) => setNotesState(n => {
    const merged = { ...n, ...(typeof update === 'function' ? update(n) : update) }
    try { sessionStorage.setItem(NOTES_KEY, JSON.stringify(merged)) } catch { /* storage unavailable */ }
    return merged
  })

  const finish = async (to: string) => {
    setFinishing(true)
    // Leave even if this fails: the account then just sees the wizard again
    // in a later session, which beats trapping it here now.
    try { await onboardingApi.complete() } catch { /* see above */ }
    updateUser({ onboarding_completed: true })
    markWhatsNewSeen()
    try { sessionStorage.removeItem(NOTES_KEY) } catch { /* storage unavailable */ }
    navigate(to, { replace: true })
  }

  if (!instance || !me) return (
    <div className="min-h-screen flex items-center justify-center bg-agora-50 dark:bg-agora-950 text-agora-500">Loading…</div>
  )

  const instanceName = instance.instance_name || 'Agora'
  const stepProps: StepProps = {
    current: step.id,
    steps: steps.map(({ id, label }) => ({ id, label })),
    instance, me, notes, setNotes, next, back, goTo, finish, finishing,
  }
  const Step = step.Component

  return (
    <div className="min-h-screen bg-agora-50 dark:bg-agora-950 text-agora-900 dark:text-agora-100">
      <header className="bg-white dark:bg-agora-900 border-b border-agora-200 dark:border-agora-700">
        <div className="max-w-6xl mx-auto px-4 sm:px-6 h-16 flex items-center justify-between gap-4">
          <div className="flex items-center gap-3 min-w-0">
            <div className="w-9 h-9 rounded-xl bg-agora-700 flex items-center justify-center flex-shrink-0 overflow-hidden">
              {instance.logo_url
                ? <img src={instance.logo_url} alt="" className="w-full h-full object-cover" />
                : <span className="text-white font-bold text-base" aria-hidden="true">{instanceName[0]?.toUpperCase()}</span>}
            </div>
            <span className="font-bold text-lg text-agora-800 dark:text-agora-100 truncate">{instanceName}</span>
            <span aria-hidden="true" className="hidden sm:block w-px h-5 bg-agora-200 dark:bg-agora-700" />
            <span className="hidden sm:block text-sm text-agora-600 dark:text-agora-300">Getting started</span>
          </div>
          {step.id !== 'done' && (
            <button type="button" onClick={() => finish('/feed')} disabled={finishing}
              className="btn-ghost min-h-[44px] flex-shrink-0">
              Skip setup
            </button>
          )}
        </div>
      </header>

      {step.bare ? (
        <main className="max-w-4xl mx-auto px-4 sm:px-6 py-10 md:py-14">
          <Step {...stepProps} />
        </main>
      ) : (
        <main className="max-w-6xl mx-auto px-4 sm:px-6 py-8 md:py-10 flex flex-wrap gap-8 items-start">
          <nav aria-label="Setup steps" className="flex-[1_1_220px] md:max-w-[260px] space-y-4">
            <div className="space-y-2 px-3">
              <p className="text-xs font-semibold uppercase tracking-wide text-agora-600 dark:text-agora-400">
                Step {index + 1} of {steps.length}
              </p>
              <div aria-hidden="true" className="h-1.5 rounded-full bg-agora-200 dark:bg-agora-700 overflow-hidden">
                <div className="h-full rounded-full bg-agora-700 dark:bg-agora-300 transition-all"
                  style={{ width: `${Math.round((index / (steps.length - 1)) * 100)}%` }} />
              </div>
            </div>
            {/* On a phone the list would push the step itself below the fold,
                so only the count and bar show there. */}
            <ol className="hidden md:block space-y-0.5">
              {steps.map((s, i) => {
                const done = i < index
                const current = i === index
                return (
                  <li key={s.id}>
                    <button type="button" onClick={() => goTo(s.id)} aria-current={current ? 'step' : undefined}
                      className={`w-full flex items-center gap-3 min-h-[44px] px-3 rounded-lg text-sm text-left transition-colors ${current
                        ? 'bg-agora-100 dark:bg-agora-700 font-semibold text-agora-900 dark:text-agora-50'
                        : `font-medium hover:bg-white dark:hover:bg-agora-800 ${done ? 'text-agora-900 dark:text-agora-100' : 'text-agora-600 dark:text-agora-400'}`}`}>
                      <span aria-hidden="true"
                        className={`w-6 h-6 rounded-full flex items-center justify-center flex-shrink-0 text-xs font-semibold border-[1.5px] ${done
                          ? 'bg-agora-700 border-agora-700 text-white dark:bg-agora-300 dark:border-agora-300 dark:text-agora-900'
                          : current
                            ? 'bg-white border-agora-700 text-agora-900 dark:bg-agora-900 dark:border-agora-300 dark:text-agora-50'
                            : 'bg-white border-agora-300 text-agora-600 dark:bg-agora-900 dark:border-agora-600 dark:text-agora-400'}`}>
                        {done ? <Check size={13} strokeWidth={3} /> : i + 1}
                      </span>
                      {s.label}
                    </button>
                  </li>
                )
              })}
            </ol>
          </nav>

          <section aria-labelledby="step-title" className="card flex-[999_1_560px] min-w-0 p-6 md:p-9 space-y-7">
            <Step {...stepProps} />
          </section>
        </main>
      )}
    </div>
  )
}
