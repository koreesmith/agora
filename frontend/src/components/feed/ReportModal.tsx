import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { instanceApi, moderationApi } from '../../api'
import { X, Flag, AlertCircle } from 'lucide-react'

interface ReportModalProps {
  postId?: string
  userId?: string
  onClose: () => void
}

export default function ReportModal({ postId, userId, onClose }: ReportModalProps) {
  const [selectedRule, setSelectedRule] = useState<string>('') // rule text or 'other'
  const [details, setDetails] = useState('')
  const [submitted, setSubmitted] = useState(false)

  const { data: rulesData } = useQuery({
    queryKey: ['instance-rules'],
    queryFn: () => instanceApi.getRules().then(r => r.data),
  })
  const rules: any[] = rulesData?.rules ?? []

  const report = useMutation({
    mutationFn: () => moderationApi.createReport({
      reported_post_id: postId,
      reported_user_id: userId,
      reason: selectedRule === 'other' ? 'other' : selectedRule,
      details,
    }),
    onSuccess: () => setSubmitted(true),
  })

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-white dark:bg-agora-800 rounded-xl shadow-xl w-full max-w-md p-6 space-y-4"
        onClick={e => e.stopPropagation()}>

        <div className="flex items-center justify-between">
          <h2 className="text-lg font-bold flex items-center gap-2">
            <Flag size={18} className="text-red-500" />
            Report {postId ? 'Post' : 'User'}
          </h2>
          <button onClick={onClose} className="btn-ghost p-1"><X size={18} /></button>
        </div>

        {submitted ? (
          <div className="text-center py-4 space-y-2">
            <AlertCircle size={32} className="mx-auto text-green-500" />
            <p className="font-medium">Report submitted</p>
            <p className="text-sm text-agora-500">Thank you. Our moderators will review this shortly.</p>
            <button onClick={onClose} className="btn-primary mt-2">Done</button>
          </div>
        ) : (
          <>
            <div className="space-y-2">
              <label className="label">Why are you reporting this?</label>

              {rules.length > 0 ? (
                <div className="space-y-2">
                  {rules.map((rule: any, i: number) => (
                    <label key={rule.id}
                      className={`flex items-start gap-3 p-3 rounded-lg border-2 cursor-pointer transition-colors ${
                        selectedRule === rule.text
                          ? 'border-agora-600 bg-agora-50 dark:bg-agora-700'
                          : 'border-agora-100 dark:border-agora-700 hover:border-agora-300'
                      }`}>
                      <input type="radio" name="rule" className="mt-0.5 flex-shrink-0"
                        checked={selectedRule === rule.text}
                        onChange={() => setSelectedRule(rule.text)} />
                      <span className="text-sm">
                        <span className="font-medium text-agora-500 mr-1.5">Rule {i + 1}.</span>
                        {rule.text}
                      </span>
                    </label>
                  ))}
                  <label className={`flex items-center gap-3 p-3 rounded-lg border-2 cursor-pointer transition-colors ${
                    selectedRule === 'other'
                      ? 'border-agora-600 bg-agora-50 dark:bg-agora-700'
                      : 'border-agora-100 dark:border-agora-700 hover:border-agora-300'
                  }`}>
                    <input type="radio" name="rule" className="flex-shrink-0"
                      checked={selectedRule === 'other'}
                      onChange={() => setSelectedRule('other')} />
                    <span className="text-sm font-medium">Something else</span>
                  </label>
                </div>
              ) : (
                // No rules configured — show free-text category options
                <div className="space-y-2">
                  {['Spam', 'Harassment or hate speech', 'Misinformation', 'Illegal content', 'Other'].map(opt => (
                    <label key={opt} className={`flex items-center gap-3 p-3 rounded-lg border-2 cursor-pointer transition-colors ${
                      selectedRule === opt
                        ? 'border-agora-600 bg-agora-50 dark:bg-agora-700'
                        : 'border-agora-100 dark:border-agora-700 hover:border-agora-300'
                    }`}>
                      <input type="radio" name="rule" className="flex-shrink-0"
                        checked={selectedRule === opt}
                        onChange={() => setSelectedRule(opt)} />
                      <span className="text-sm">{opt}</span>
                    </label>
                  ))}
                </div>
              )}
            </div>

            <div>
              <label className="label">Additional details <span className="text-agora-400 font-normal">(optional)</span></label>
              <textarea
                className="input w-full resize-none text-sm"
                rows={3}
                placeholder="Provide any context that might help our moderators…"
                value={details}
                onChange={e => setDetails(e.target.value)}
              />
            </div>

            {report.isError && (
              <p className="text-sm text-red-500">Something went wrong. Please try again.</p>
            )}

            <div className="flex gap-2 justify-end pt-1">
              <button onClick={onClose} className="btn-secondary">Cancel</button>
              <button
                onClick={() => report.mutate()}
                disabled={!selectedRule || report.isPending}
                className="btn-danger"
              >
                {report.isPending ? 'Submitting…' : 'Submit Report'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
