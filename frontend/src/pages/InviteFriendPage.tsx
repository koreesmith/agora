import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Mail, Send, CheckCircle2 } from 'lucide-react'
import { inviteApi } from '../api'

export default function InviteFriendPage() {
  const [email, setEmail] = useState('')
  const [sent, setSent] = useState<string[]>([])
  const [err, setErr] = useState('')

  const send = useMutation({
    mutationFn: () => inviteApi.send(email.trim()),
    onSuccess: () => {
      setSent(s => [...s, email.trim()])
      setEmail('')
      setErr('')
    },
    onError: (e: any) => setErr(e.response?.data?.error || 'Could not send invite'),
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!email.trim()) return
    setErr('')
    send.mutate()
  }

  return (
    <div className="max-w-lg mx-auto space-y-6">
      <div>
        <h1 className="text-xl font-bold">Invite a friend</h1>
        <p className="text-sm text-agora-500 dark:text-agora-400 mt-1">
          Know someone who'd love Agora? Send them an invitation.
        </p>
      </div>

      <div className="card p-6 space-y-4">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-agora-100 dark:bg-agora-700 flex items-center justify-center flex-shrink-0">
            <Mail size={20} className="text-agora-600 dark:text-agora-300" />
          </div>
          <div>
            <p className="font-semibold text-sm">Email invitation</p>
            <p className="text-xs text-agora-400">
              We'll send them a personalised invite with your name on it.
            </p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label className="label">Friend's email address</label>
            <input
              type="email"
              className="input w-full"
              placeholder="friend@example.com"
              value={email}
              onChange={e => { setEmail(e.target.value); setErr('') }}
              autoComplete="off"
              disabled={send.isPending}
            />
          </div>

          {err && (
            <p className="text-sm text-red-500 bg-red-50 dark:bg-red-900/20 rounded-lg px-3 py-2">{err}</p>
          )}

          <button
            type="submit"
            disabled={!email.trim() || send.isPending}
            className="btn-primary w-full flex items-center justify-center gap-2"
          >
            <Send size={15} />
            {send.isPending ? 'Sending…' : 'Send invitation'}
          </button>
        </form>
      </div>

      {/* Sent list */}
      {sent.length > 0 && (
        <div className="card p-4 space-y-2">
          <p className="text-sm font-semibold text-agora-600 dark:text-agora-300">
            Invitations sent ✓
          </p>
          {sent.map(e => (
            <div key={e} className="flex items-center gap-2 text-sm text-agora-600 dark:text-agora-300">
              <CheckCircle2 size={15} className="text-green-500 flex-shrink-0" />
              {e}
            </div>
          ))}
        </div>
      )}

      <div className="card p-4 bg-agora-50 dark:bg-agora-800/50">
        <p className="text-xs text-agora-500 dark:text-agora-400 leading-relaxed">
          Your friend will receive a personalised email letting them know you invited them. 
          They can choose to sign up or ignore the invitation — no account will be created without their action.
        </p>
      </div>
    </div>
  )
}
