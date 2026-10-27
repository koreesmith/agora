import type { ComponentType } from 'react'

// AGORA-388: shared shapes for the getting started wizard (AGORA-386).

export type StepId = 'welcome' | 'profile' | 'privacy' | 'bluesky' | 'fediverse' | 'people' | 'done'

// The parts of GET /api/instance the wizard uses (AGORA-387).
export interface InstanceInfo {
  instance_name?: string
  logo_url?: string
  user_invites_enabled?: string
  domain?: string
  activitypub_enabled?: boolean
  atproto_enabled?: boolean
}

// What one step did that a later one reports back. Kept for the browser
// session so a reload mid-wizard doesn't lose it.
export interface WizardNotes {
  domainReminder: boolean
  requestsSent: number
  follows: number
  groupsJoined: number
}

export interface StepProps {
  current: StepId
  // The steps this instance shows, in order: a network the admin turned off
  // has no step.
  steps: { id: StepId; label: string }[]
  instance: InstanceInfo
  // Fresh GET /auth/me (the shared 'my-profile' query). Steps that change a
  // setting invalidate it, so the recap at the end reads what was saved.
  me: any
  notes: WizardNotes
  setNotes: (update: Partial<WizardNotes> | ((n: WizardNotes) => Partial<WizardNotes>)) => void
  next: () => void
  back: () => void
  goTo: (id: StepId) => void
  // Marks onboarding complete and leaves for `to`. Used by finishing and by
  // every kind of skip.
  finish: (to: string) => void
  finishing: boolean
}

export interface StepDef {
  id: StepId
  label: string
  Component: ComponentType<StepProps>
  // Rendered on its own, without the step rail and card (the Welcome step).
  bare?: boolean
}
