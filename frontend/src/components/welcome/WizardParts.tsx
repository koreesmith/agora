import type { ReactNode } from 'react'
import { AlertTriangle, ArrowLeft, ArrowRight, Info } from 'lucide-react'

// AGORA-388: building blocks shared by the getting started wizard's steps.

export function StepHeader({ eyebrow, title, children }: { eyebrow?: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="space-y-2.5">
      {eyebrow}
      <h1 id="step-title" className="text-2xl md:text-3xl font-bold leading-tight text-agora-900 dark:text-agora-50">{title}</h1>
      {children && <p className="text-agora-600 dark:text-agora-300 leading-relaxed max-w-2xl">{children}</p>}
    </div>
  )
}

export function StepFooter({ onBack, onSkip, onContinue, continueLabel = 'Continue', busy }: {
  onBack?: () => void
  onSkip?: () => void
  onContinue: () => void
  continueLabel?: string
  busy?: boolean
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 pt-6 border-t border-agora-100 dark:border-agora-700">
      {onBack
        ? <button type="button" onClick={onBack} className="btn-ghost min-h-[44px]"><ArrowLeft size={18} /> Back</button>
        : <span />}
      <div className="flex flex-wrap items-center gap-2">
        {onSkip && <button type="button" onClick={onSkip} className="btn-ghost min-h-[44px]">Skip this step</button>}
        <button type="button" onClick={onContinue} disabled={busy} className="btn-primary min-h-[44px] px-5">
          {busy ? 'Saving…' : <>{continueLabel} <ArrowRight size={18} /></>}
        </button>
      </div>
    </div>
  )
}

// Same look as the toggles in Settings, with a 44px tap target.
export function Switch({ checked, onChange, labelledBy, disabled }: {
  checked: boolean
  onChange: (next: boolean) => void
  labelledBy: string
  disabled?: boolean
}) {
  return (
    <button type="button" role="switch" aria-checked={checked} aria-labelledby={labelledBy} disabled={disabled}
      onClick={() => onChange(!checked)}
      className="flex-shrink-0 w-14 h-11 flex items-center justify-center rounded-lg disabled:opacity-50 disabled:cursor-not-allowed">
      <span className={`relative inline-flex h-7 w-12 rounded-full transition-colors ${checked ? 'bg-agora-700' : 'bg-agora-200 dark:bg-agora-600'}`}>
        <span className={`absolute top-1 left-1 h-5 w-5 rounded-full bg-white shadow transition-transform ${checked ? 'translate-x-5' : ''}`} />
      </span>
    </button>
  )
}

// A titled setting with its control on the right, like a row in Settings.
export function SettingRow({ id, title, description, control, last, children }: {
  id: string
  title: string
  description: ReactNode
  control: ReactNode
  last?: boolean
  children?: ReactNode
}) {
  return (
    <div className={`py-3.5 space-y-3 ${last ? '' : 'border-b border-agora-100 dark:border-agora-700'}`}>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <div className="min-w-0 flex-1 basis-64">
          <p id={id} className="font-medium text-sm text-agora-900 dark:text-agora-100">{title}</p>
          <p className="text-sm text-agora-600 dark:text-agora-400 leading-relaxed">{description}</p>
        </div>
        {control}
      </div>
      {children}
    </div>
  )
}

export function Callout({ tone, children }: { tone: 'warning' | 'info'; children: ReactNode }) {
  const warning = tone === 'warning'
  return (
    <div role={warning ? 'status' : undefined}
      className={`flex gap-3 items-start rounded-xl px-4 py-3.5 text-sm leading-relaxed border ${warning
        ? 'bg-amber-50 border-amber-300 text-amber-900 dark:bg-amber-900/20 dark:border-amber-700 dark:text-amber-200'
        : 'bg-agora-50 border-agora-100 text-agora-700 dark:bg-agora-900/40 dark:border-agora-700 dark:text-agora-200'}`}>
      {warning
        ? <AlertTriangle size={18} className="flex-shrink-0 mt-0.5" aria-hidden="true" />
        : <Info size={18} className="flex-shrink-0 mt-0.5" aria-hidden="true" />}
      <div className="min-w-0">{children}</div>
    </div>
  )
}
