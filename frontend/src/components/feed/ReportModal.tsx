import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { instanceApi, moderationApi } from '../../api'
import { X, Flag, AlertCircle, ChevronRight } from 'lucide-react'

const VIOLATION_TYPES = [
  { value: 'spam',            label: 'Spam',                     desc: 'Repetitive, irrelevant, or commercial content' },
  { value: 'hate_speech',     label: 'Hate speech',              desc: 'Content targeting people based on identity' },
  { value: 'harassment',      label: 'Harassment or bullying',   desc: 'Content intended to intimidate or harm' },
  { value: 'illegal_content', label: 'Illegal content',          desc: 'Content that may violate local or federal law' },
  { value: 'misinformation',  label: 'Misinformation',           desc: 'Demonstrably false or misleading information' },
  { value: 'sexual_content',  label: 'Explicit sexual content',  desc: 'Pornographic or sexually explicit material' },
  { value: 'violence',        label: 'Violence or threats',      desc: 'Threats of violence or graphic violent content' },
  { value: 'rule_violation',  label: 'Server rule violation',    desc: 'Violates a specific rule of this instance' },
  { value: 'other',           label: 'Something else',           desc: 'Doesn\'t fit the above categories' },
]

interface Props {
  postId?: string
  commentId?: string
  userId?: string
  onClose: () => void
}

export default function ReportModal({ postId, commentId, userId, onClose }: Props) {
  const [step, setStep] = useState<'type' | 'rule' | 'details'>('type')
  const [violationType, setViolationType] = useState('')
  const [selectedRuleId, setSelectedRuleId] = useState('')
  const [details, setDetails] = useState('')
  const [submitted, setSubmitted] = useState(false)

  const { data: rulesData } = useQuery({
    queryKey: ['instance-rules'],
    queryFn: () => instanceApi.getRules().then(r => r.data),
  })
  const rules: any[] = rulesData?.rules ?? []

  const report = useMutation({
    mutationFn: () => moderationApi.createReport({
      reported_post_id:    postId,
      reported_comment_id: commentId,
      reported_user_id:    userId,
      violation_type:      violationType,
      rule_id:             selectedRuleId || undefined,
      details,
    }),
    onSuccess: () => setSubmitted(true),
  })

  const targetLabel = commentId ? 'Comment' : postId ? 'Post' : 'User'

  if (submitted) return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-md p-6 text-center space-y-3" onClick={e => e.stopPropagation()}>
        <AlertCircle size={36} className="mx-auto text-green-500" />
        <p className="font-bold text-lg">Report submitted</p>
        <p className="text-sm text-agora-500">Thank you. Our moderators will review this shortly.</p>
        <button onClick={onClose} className="btn-primary mt-2">Done</button>
      </div>
    </div>
  )

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-md" onClick={e => e.stopPropagation()}>
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-agora-100 dark:border-agora-700">
          <div className="flex items-center gap-2">
            {step !== 'type' && (
              <button onClick={() => setStep(step === 'details' && violationType === 'rule_violation' ? 'rule' : 'type')}
                className="btn-ghost p-1 mr-1">←</button>
            )}
            <Flag size={16} className="text-red-500" />
            <h2 className="font-bold">Report {targetLabel}</h2>
          </div>
          <button onClick={onClose} className="btn-ghost p-1"><X size={18} /></button>
        </div>

        <div className="p-4 space-y-3">
          {/* Step 1: Violation type */}
          {step === 'type' && (
            <>
              <p className="text-sm text-agora-500 mb-3">What's the issue with this {targetLabel.toLowerCase()}?</p>
              <div className="space-y-1.5">
                {VIOLATION_TYPES.filter(v => v.value !== 'rule_violation' || rules.length > 0).map(v => (
                  <button key={v.value}
                    onClick={() => {
                      setViolationType(v.value)
                      if (v.value === 'rule_violation') setStep('rule')
                      else setStep('details')
                    }}
                    className="w-full text-left flex items-center justify-between gap-3 p-3 rounded-lg border border-agora-100 dark:border-agora-700 hover:border-agora-400 hover:bg-agora-50 dark:hover:bg-agora-700/50 transition-colors"
                  >
                    <div>
                      <p className="text-sm font-medium">{v.label}</p>
                      <p className="text-xs text-agora-400">{v.desc}</p>
                    </div>
                    <ChevronRight size={14} className="text-agora-300 flex-shrink-0" />
                  </button>
                ))}
              </div>
            </>
          )}

          {/* Step 2: Rule selection (only for rule_violation) */}
          {step === 'rule' && (
            <>
              <p className="text-sm text-agora-500 mb-3">Which rule was violated?</p>
              <div className="space-y-1.5">
                {rules.map((rule: any, i: number) => (
                  <label key={rule.id}
                    className={`flex items-start gap-3 p-3 rounded-lg border-2 cursor-pointer transition-colors ${
                      selectedRuleId === rule.id
                        ? 'border-agora-600 bg-agora-50 dark:bg-agora-700'
                        : 'border-agora-100 dark:border-agora-700 hover:border-agora-300'
                    }`}>
                    <input type="radio" name="rule" className="mt-0.5 flex-shrink-0"
                      checked={selectedRuleId === rule.id}
                      onChange={() => setSelectedRuleId(rule.id)} />
                    <span className="text-sm">
                      <span className="font-medium text-agora-500 mr-1.5">Rule {i + 1}.</span>
                      {rule.text}
                    </span>
                  </label>
                ))}
              </div>
              <button onClick={() => setStep('details')} disabled={!selectedRuleId}
                className="btn-primary w-full mt-2">Continue</button>
            </>
          )}

          {/* Step 3: Additional details */}
          {step === 'details' && (
            <>
              <div className="p-3 bg-agora-50 dark:bg-agora-700/50 rounded-lg">
                <p className="text-xs text-agora-500 font-medium uppercase tracking-wide">Reporting for</p>
                <p className="text-sm font-semibold mt-0.5">
                  {VIOLATION_TYPES.find(v => v.value === violationType)?.label}
                  {selectedRuleId && rules.find((r: any) => r.id === selectedRuleId) && (
                    <span className="text-agora-400 font-normal"> — {rules.find((r: any) => r.id === selectedRuleId)?.text}</span>
                  )}
                </p>
              </div>
              <div>
                <label className="label">Additional details <span className="text-agora-400 font-normal">(optional)</span></label>
                <textarea className="input w-full resize-none text-sm" rows={3} autoComplete="off"
                  placeholder="Provide any context that might help our moderators…"
                  value={details} onChange={e => setDetails(e.target.value)} />
              </div>
              {report.isError && <p className="text-sm text-red-500">Something went wrong. Please try again.</p>}
              <div className="flex gap-2 justify-end pt-1">
                <button onClick={onClose} className="btn-secondary">Cancel</button>
                <button onClick={() => report.mutate()} disabled={report.isPending} className="btn-danger">
                  {report.isPending ? 'Submitting…' : 'Submit report'}
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
