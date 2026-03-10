import { useState } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { authApi } from '../api'

export default function ResetPasswordPage() {
  const [params] = useSearchParams()
  const [password, setPassword] = useState('')
  const [done, setDone] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setLoading(true)
    try {
      await authApi.resetPassword({ token: params.get('token'), new_password: password })
      setDone(true)
    } catch (err:any) { setError(err.response?.data?.error||'Reset failed') }
    finally { setLoading(false) }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        {done ? (
          <div className="card p-8 text-center space-y-4">
            <div className="text-3xl">✓</div>
            <h2 className="text-xl font-bold">Password reset!</h2>
            <Link to="/login" className="btn-primary inline-flex">Sign in</Link>
          </div>
        ) : (
          <form onSubmit={submit} className="card p-6 space-y-4">
            <h2 className="text-xl font-bold">Set new password</h2>
            {error && <div className="bg-red-50 border border-red-200 rounded-lg px-3 py-2 text-sm text-red-700">{error}</div>}
            <div><label className="label">New password</label>
              <input type="password" className="input" required minLength={8} value={password} onChange={e=>setPassword(e.target.value)} /></div>
            <button type="submit" disabled={loading} className="btn-primary w-full">{loading?'Saving…':'Reset password'}</button>
          </form>
        )}
      </div>
    </div>
  )
}
