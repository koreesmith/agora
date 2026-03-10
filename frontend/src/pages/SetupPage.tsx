import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuthStore } from '../store/auth'
import api from '../api'

export default function SetupPage() {
  const { setAuth } = useAuthStore()
  const navigate = useNavigate()
  const [form, setForm] = useState({ username:'', email:'', password:'', display_name:'', instance_name:'Agora' })
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement>) => setForm(f=>({...f,[k]:e.target.value}))

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setError(''); setLoading(true)
    try {
      const res = await api.post('/setup', form)
      const { token, ...userData } = res.data
      setAuth(userData, token)
      navigate('/')
    } catch (err: any) {
      setError(err.response?.data?.error || 'Setup failed')
    } finally { setLoading(false) }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-agora-50 dark:bg-agora-950 px-4 py-8">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <div className="w-12 h-12 rounded-xl bg-agora-700 flex items-center justify-center mx-auto mb-3">
            <span className="text-white font-bold text-xl">A</span>
          </div>
          <h1 className="text-2xl font-bold text-agora-900 dark:text-agora-100">Welcome to Agora</h1>
          <p className="text-sm text-agora-500 mt-1">Let's set up your instance</p>
        </div>
        <form onSubmit={submit} className="card p-6 space-y-4">
          {error && <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">{error}</div>}
          <div className="pb-2 border-b border-agora-100 dark:border-agora-700">
            <p className="text-xs font-semibold uppercase tracking-wide text-agora-400 mb-3">Instance</p>
            <div><label className="label">Instance name</label>
              <input className="input" value={form.instance_name} onChange={set('instance_name')} /></div>
          </div>
          <div>
            <p className="text-xs font-semibold uppercase tracking-wide text-agora-400 mb-3">Admin account</p>
            <div className="space-y-3">
              <div><label className="label">Display name</label>
                <input className="input" value={form.display_name} onChange={set('display_name')} /></div>
              <div><label className="label">Username *</label>
                <input className="input" required minLength={3} value={form.username} onChange={set('username')} /></div>
              <div><label className="label">Email *</label>
                <input type="email" className="input" required value={form.email} onChange={set('email')} /></div>
              <div><label className="label">Password *</label>
                <input type="password" className="input" required minLength={8} value={form.password} onChange={set('password')} /></div>
            </div>
          </div>
          <button type="submit" disabled={loading} className="btn-primary w-full">
            {loading ? 'Creating…' : 'Create admin account'}
          </button>
        </form>
      </div>
    </div>
  )
}
