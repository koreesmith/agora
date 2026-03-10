import { useEffect, useState } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { authApi } from '../api'

export default function VerifyEmailPage() {
  const [params] = useSearchParams()
  const [status, setStatus] = useState<'loading'|'ok'|'error'>('loading')

  useEffect(() => {
    const token = params.get('token')
    if (!token) { setStatus('error'); return }
    authApi.verifyEmail(token).then(() => setStatus('ok')).catch(() => setStatus('error'))
  }, [])

  return (
    <div className="min-h-screen flex items-center justify-center px-4">
      <div className="card p-8 max-w-sm w-full text-center space-y-4">
        {status === 'loading' && <p className="text-agora-500">Verifying…</p>}
        {status === 'ok' && <>
          <div className="text-4xl">✓</div>
          <h2 className="text-xl font-bold">Email verified!</h2>
          <Link to="/login" className="btn-primary inline-flex">Sign in</Link>
        </>}
        {status === 'error' && <>
          <div className="text-4xl">✗</div>
          <h2 className="text-xl font-bold">Verification failed</h2>
          <p className="text-sm text-agora-500">Token invalid or expired.</p>
          <Link to="/login" className="btn-secondary inline-flex">Back to sign in</Link>
        </>}
      </div>
    </div>
  )
}
