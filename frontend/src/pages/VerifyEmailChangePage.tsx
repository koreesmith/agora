import { useEffect, useState } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { authApi } from '../api'
import { useAuthStore } from '../store/auth'

export default function VerifyEmailChangePage() {
  const [params] = useSearchParams()
  const [status, setStatus] = useState<'loading'|'ok'|'error'>('loading')
  const { updateUser } = useAuthStore()

  useEffect(() => {
    const token = params.get('token')
    if (!token) { setStatus('error'); return }
    authApi.verifyEmailChange(token)
      .then(res => {
        // Refresh user data so the displayed email updates
        authApi.me().then(r => updateUser(r.data))
        setStatus('ok')
      })
      .catch(() => setStatus('error'))
  }, [])

  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="card p-8 max-w-sm w-full text-center space-y-4">
        {status === 'loading' && <p className="text-agora-500">Verifying…</p>}
        {status === 'ok' && <>
          <div className="text-4xl">✓</div>
          <h2 className="text-xl font-bold">Email address updated!</h2>
          <p className="text-sm text-agora-500">Your email address has been changed successfully.</p>
          <Link to="/settings" className="btn-primary inline-flex">Go to settings</Link>
        </>}
        {status === 'error' && <>
          <div className="text-4xl">✗</div>
          <h2 className="text-xl font-bold">Verification failed</h2>
          <p className="text-sm text-agora-500">Token invalid or expired. Please request a new email change from settings.</p>
          <Link to="/settings" className="btn-secondary inline-flex">Back to settings</Link>
        </>}
      </div>
    </div>
  )
}
