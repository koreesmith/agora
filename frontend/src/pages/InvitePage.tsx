import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { groupsApi } from '../api'
import { Users, CheckCircle, XCircle } from 'lucide-react'

export default function InvitePage() {
  const { token } = useParams<{ token: string }>()
  const navigate = useNavigate()
  const [status, setStatus] = useState<'loading'|'success'|'error'|'already'>('loading')
  const [slug, setSlug] = useState('')
  const [errorMsg, setErrorMsg] = useState('')

  useEffect(() => {
    if (!token) return
    groupsApi.joinByInvite(token)
      .then(res => {
        setSlug(res.data.slug)
        if (res.data.message === 'you are already a member') {
          setStatus('already')
        } else {
          setStatus('success')
        }
      })
      .catch(err => {
        const msg = err.response?.data?.error || 'Invalid or expired invite link'
        if (msg.includes('already')) {
          // get slug from response if available
          setStatus('already')
        } else {
          setErrorMsg(msg)
          setStatus('error')
        }
      })
  }, [token])

  if (status === 'loading') return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="card p-10 text-center space-y-3 max-w-sm w-full">
        <Users size={36} className="mx-auto text-agora-400 animate-pulse" />
        <p className="text-agora-500">Joining group…</p>
      </div>
    </div>
  )

  if (status === 'success') return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="card p-10 text-center space-y-4 max-w-sm w-full">
        <CheckCircle size={40} className="mx-auto text-green-500" />
        <div>
          <h2 className="text-lg font-bold">You're in!</h2>
          <p className="text-sm text-agora-500 mt-1">You've successfully joined the group.</p>
        </div>
        <button onClick={() => navigate(`/groups/${slug}`)} className="btn-primary w-full">
          Go to Group
        </button>
      </div>
    </div>
  )

  if (status === 'already') return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="card p-10 text-center space-y-4 max-w-sm w-full">
        <CheckCircle size={40} className="mx-auto text-agora-400" />
        <div>
          <h2 className="text-lg font-bold">Already a member</h2>
          <p className="text-sm text-agora-500 mt-1">You're already in this group.</p>
        </div>
        {slug
          ? <button onClick={() => navigate(`/groups/${slug}`)} className="btn-primary w-full">Go to Group</button>
          : <Link to="/groups" className="btn-secondary w-full block text-center">Browse Groups</Link>
        }
      </div>
    </div>
  )

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="card p-10 text-center space-y-4 max-w-sm w-full">
        <XCircle size={40} className="mx-auto text-red-400" />
        <div>
          <h2 className="text-lg font-bold">Invite not valid</h2>
          <p className="text-sm text-agora-500 mt-1">{errorMsg}</p>
        </div>
        <Link to="/groups" className="btn-secondary block text-center">Browse Groups</Link>
      </div>
    </div>
  )
}
