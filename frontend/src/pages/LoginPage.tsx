import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { authApi } from '../api'
import { useAuthStore } from '../store/auth'

export default function LoginPage() {
  const [form, setForm] = useState({ username_or_email: '', password: '' })
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [forgotMode, setForgotMode] = useState(false)
  const [forgotSent, setForgotSent] = useState(false)
  const [forgotEmail, setForgotEmail] = useState('')
  const { setAuth } = useAuthStore()
  const navigate = useNavigate()

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setError(''); setLoading(true)
    try {
      const res = await authApi.login(form)
      const { token, ...userData } = res.data
      setAuth(userData, token)
    } catch (err: any) {
      setError(err.response?.data?.error || 'Login failed')
    } finally { setLoading(false) }
  }

  const submitForgot = async (e: React.FormEvent) => {
    e.preventDefault(); setLoading(true)
    try {
      await authApi.forgotPassword(forgotEmail)
      setForgotSent(true)
    } finally { setLoading(false) }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-agora-50 dark:bg-agora-950 px-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <div className="w-12 h-12 rounded-xl bg-agora-700 flex items-center justify-center mx-auto mb-3">
            <span className="text-white font-bold text-xl">A</span>
          </div>
          <h1 className="text-2xl font-bold text-agora-900 dark:text-agora-100">
            {forgotMode ? 'Reset password' : 'Welcome back'}
          </h1>
          <p className="text-sm text-agora-500 mt-1">
            {forgotMode ? 'Enter your email to get a reset link' : 'Sign in to Agora'}
          </p>
        </div>

        {forgotMode ? (
          forgotSent ? (
            <div className="card p-6 text-center space-y-3">
              <p className="text-sm text-agora-600 dark:text-agora-300">
                If that email is registered, a reset link has been sent.
              </p>
              <button onClick={() => { setForgotMode(false); setForgotSent(false) }} className="btn-secondary w-full">
                Back to sign in
              </button>
            </div>
          ) : (
            <form onSubmit={submitForgot} className="card p-6 space-y-4">
              <div><label className="label">Email address</label>
                <input type="email" className="input" required autoComplete="email" value={forgotEmail}
                  onChange={e => setForgotEmail(e.target.value)} /></div>
              <button type="submit" disabled={loading} className="btn-primary w-full">
                {loading ? 'Sending…' : 'Send reset link'}
              </button>
              <button type="button" onClick={() => setForgotMode(false)} className="btn-ghost w-full text-sm">
                Back to sign in
              </button>
            </form>
          )
        ) : (
          <form onSubmit={submit} className="card p-6 space-y-4">
            {error && <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">{error}</div>}
            <div><label className="label">Username or email</label>
              <input className="input" required autoComplete="username email" value={form.username_or_email}
                onChange={e => setForm(f => ({ ...f, username_or_email: e.target.value }))} /></div>
            <div><label className="label">Password</label>
              <input type="password" className="input" required autoComplete="current-password" value={form.password}
                onChange={e => setForm(f => ({ ...f, password: e.target.value }))} /></div>
            <button type="submit" disabled={loading} className="btn-primary w-full">
              {loading ? 'Signing in…' : 'Sign in'}
            </button>
            <div className="text-center text-sm">
              <button type="button" onClick={() => setForgotMode(true)} className="text-agora-600 hover:underline">
                Forgot password?
              </button>
            </div>
          </form>
        )}

        <p className="text-center text-sm text-agora-500 mt-4">
          Don't have an account?{' '}
          <Link to="/register" className="text-agora-600 hover:underline font-medium">Create one</Link>
        </p>
      </div>
    </div>
  )
}
