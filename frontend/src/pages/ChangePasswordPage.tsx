import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { authApi } from '../api'
import { useAuthStore } from '../store/auth'

export default function ChangePasswordPage() {
  const { logout } = useAuthStore()
  const navigate = useNavigate()
  const [form, setForm] = useState({ current_password: '', new_password: '' })
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setError(''); setLoading(true)
    try {
      await authApi.changePassword(form)
      navigate('/')
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed')
    } finally { setLoading(false) }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <form onSubmit={submit} className="card p-6 space-y-4">
          <h2 className="text-xl font-bold">Change password</h2>
          {error && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">
              {error}
            </div>
          )}
          <div>
            <label className="label">Current password</label>
            <input type="password" className="input" required
              value={form.current_password}
              onChange={e => setForm(f => ({ ...f, current_password: e.target.value }))} />
          </div>
          <div>
            <label className="label">New password</label>
            <input type="password" className="input" required minLength={8}
              value={form.new_password}
              onChange={e => setForm(f => ({ ...f, new_password: e.target.value }))} />
          </div>
          <button type="submit" disabled={loading} className="btn-primary w-full">
            {loading ? 'Saving…' : 'Change password'}
          </button>
          <button type="button" onClick={() => navigate(-1)} className="btn-secondary w-full">
            Cancel
          </button>
          <button type="button" onClick={() => { logout(); navigate('/login') }}
            className="btn-ghost w-full text-sm text-agora-400">
            Sign out instead
          </button>
        </form>
      </div>
    </div>
  )
}
