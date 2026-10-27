import type { StepProps } from './types'
import { StepFooter, StepHeader } from './WizardParts'

// AGORA-388: stands in for a step until its own story lands (AGORA-389 to
// AGORA-391), so the shell can be exercised end to end on its own.
export default function PlaceholderStep({ current, steps, next, back, finish, finishing }: StepProps) {
  const index = steps.findIndex(s => s.id === current)
  const last = index === steps.length - 1
  return (
    <>
      <StepHeader title={steps[index]?.label ?? ''} />
      <StepFooter
        onBack={index > 0 ? back : undefined}
        onContinue={last ? () => finish('/feed') : next}
        continueLabel={last ? 'Go to my feed' : 'Continue'}
        busy={finishing}
      />
    </>
  )
}
