import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { authApi } from '../api'

export default function RegisterPage() {
  const [params] = useSearchParams()
  const [form, setForm] = useState({ username:'', email:'', password:'', display_name:'', invite_code: params.get('invite')||'' })
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)
  const [waitlisted, setWaitlisted] = useState(false)
  const [loading, setLoading] = useState(false)

  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement>) => setForm(f=>({...f,[k]:e.target.value}))

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setError(''); setLoading(true)
    try {
      const res = await authApi.register(form)
      if (res.data?.message === 'waitlist') {
        setWaitlisted(true)
      } else {
        setSuccess(true)
      }
    }
    catch (err:any) { setError(err.response?.data?.error||'Registration failed') }
    finally { setLoading(false) }
  }

  if (waitlisted) return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="card p-8 max-w-sm w-full text-center space-y-4">
        <div className="w-12 h-12 rounded-full bg-amber-100 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400 flex items-center justify-center mx-auto text-2xl">⏳</div>
        <h2 className="text-xl font-bold">You're on the waitlist!</h2>
        <p className="text-agora-500 text-sm">Thanks for signing up, <strong>{form.display_name || form.username}</strong>. We're reviewing new accounts — you'll receive an invite link by email once you're approved.</p>
        <p className="text-agora-400 text-xs">Also check your inbox for an email verification link — you'll need to verify your email too.</p>
      </div>
    </div>
  )

  if (success) return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="card p-8 max-w-sm w-full text-center space-y-4">
        <div className="w-12 h-12 rounded-full bg-green-100 text-green-600 flex items-center justify-center mx-auto text-2xl">✓</div>
        <h2 className="text-xl font-bold">Check your email</h2>
        <p className="text-agora-500 text-sm">We sent a verification link to <strong>{form.email}</strong>.</p>
        <Link to="/login" className="btn-primary inline-flex">Go to sign in</Link>
      </div>
    </div>
  )

  return (
    <div className="min-h-screen flex items-center justify-center bg-agora-50 dark:bg-agora-950 px-4 py-8">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <div className="w-12 h-12 rounded-xl bg-agora-700 flex items-center justify-center mx-auto mb-3">
            <span className="text-white font-bold text-xl">A</span>
          </div>
          <h1 className="text-2xl font-bold text-agora-900 dark:text-agora-100">Join Agora</h1>
          <p className="text-sm text-agora-500 mt-1">Your community, your rules</p>
        </div>
        <form onSubmit={submit} className="card p-6 space-y-4">
          {error && <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">{error}</div>}
          <div><label className="label">Display name</label><input className="input" autoComplete="name" value={form.display_name} onChange={set('display_name')} /></div>
          <div>
            <label className="label">Username *</label>
            <input className="input" required minLength={3} maxLength={50} autoComplete="username" value={form.username} onChange={set('username')} />
            <p className="text-xs text-agora-400 mt-1">Letters, numbers, underscores, hyphens only</p>
          </div>
          <div><label className="label">Email *</label><input type="email" className="input" required autoComplete="email" value={form.email} onChange={set('email')} /></div>
          <div><label className="label">Password *</label><input type="password" className="input" required minLength={8} autoComplete="new-password" value={form.password} onChange={set('password')} /></div>
          <div><label className="label">Invite code</label><input className="input" autoComplete="off" value={form.invite_code} onChange={set('invite_code')} placeholder="Required if registration is invite-only" /></div>
          <button type="submit" disabled={loading} className="btn-primary w-full">{loading ? 'Creating account…' : 'Create account'}</button>
        </form>
        <p className="text-center text-sm text-agora-500 mt-4">Already have an account? <Link to="/login" className="text-agora-600 hover:underline font-medium">Sign in</Link></p>
      </div>
    </div>
  )
}
