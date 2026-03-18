import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import api from '../api'

interface InstanceInfo {
  instance_name: string
  instance_description: string
  registration_mode: string
}

const FEATURES = [
  { icon: '🔒', title: 'Private by default', desc: 'Posts are visible to friends only. You control who sees what.' },
  { icon: '🌐', title: 'Federated', desc: 'Connect with friends on other Agora instances.' },
  { icon: '👥', title: 'Groups', desc: 'Create and join groups around shared interests.' },
  { icon: '💬', title: 'Real conversations', desc: 'Threaded comments, reactions, and direct messages.' },
  { icon: '📱', title: 'Mobile apps', desc: 'Native iOS and Android apps. Coming soon!' },
  { icon: '🚫', title: 'No ads. Ever.', desc: 'No advertisers, no tracking, no manipulation.' },
]

export default function LandingPage() {
  const [instance, setInstance] = useState<InstanceInfo | null>(null)
  const [userCount, setUserCount] = useState(0)
  const [postCount, setPostCount] = useState(0)
  const [rules, setRules] = useState<{ id: string; text: string }[]>([])

  useEffect(() => {
    api.get('/instance').then(r => setInstance(r.data)).catch(() => {})
    api.get('/instance/rules').then(r => setRules(r.data.rules || [])).catch(() => {})
    api.get('/stats').then(r => { setUserCount(r.data.user_count || 0); setPostCount(r.data.post_count || 0) }).catch(() => {})
  }, [])

  const name = instance?.instance_name || 'Agora'
  const desc = instance?.instance_description || 'A private, federated social network for the people you actually know.'
  const canRegister = instance?.registration_mode !== 'closed'
  const isWaitlist = instance?.registration_mode === 'waitlist'

  return (
    <div style={{ minHeight: '100vh', fontFamily: 'Inter, system-ui, sans-serif', backgroundColor: '#f0f4f8' }}>

      {/* Nav */}
      <nav style={{ backgroundColor: '#fff', borderBottom: '1px solid #d9e2ec', position: 'sticky', top: 0, zIndex: 20 }}>
        <div style={{ maxWidth: 960, margin: '0 auto', padding: '12px 24px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <svg width="32" height="32" viewBox="0 0 96 96" fill="none">
              <rect width="96" height="96" rx="20" fill="#486581"/>
              <path d="M48 16L24 72" stroke="white" strokeWidth="6" strokeLinecap="round"/>
              <path d="M48 16L72 72" stroke="white" strokeWidth="6" strokeLinecap="round"/>
              <line x1="34" y1="50" x2="62" y2="50" stroke="white" strokeWidth="5.5" strokeLinecap="round"/>
              <circle cx="28" cy="50" r="4" fill="#9fb3c8"/>
              <circle cx="68" cy="50" r="4" fill="#9fb3c8"/>
            </svg>
            <span style={{ fontWeight: 700, fontSize: 18, color: '#102a43' }}>{name}</span>
          </div>
          <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
            <Link to="/login" style={{ fontSize: 14, fontWeight: 500, color: '#486581', textDecoration: 'none', padding: '6px 12px', borderRadius: 8 }}>
              Sign in
            </Link>
            {canRegister && (
              <Link to="/register" style={{ fontSize: 14, fontWeight: 600, backgroundColor: '#486581', color: '#fff', textDecoration: 'none', padding: '7px 16px', borderRadius: 8 }}>
                Create account
              </Link>
            )}
          </div>
        </div>
      </nav>

      {/* Hero */}
      <div style={{ backgroundColor: '#fff', borderBottom: '1px solid #d9e2ec', padding: '64px 24px' }}>
        <div style={{ maxWidth: 960, margin: '0 auto', display: 'flex', alignItems: 'center', gap: 64, flexWrap: 'wrap' }}>
          <div style={{ flex: 1, minWidth: 280 }}>
            <div style={{ display: 'inline-block', backgroundColor: '#f0f4f8', color: '#486581', fontSize: 11, fontWeight: 600, padding: '4px 12px', borderRadius: 20, marginBottom: 20, textTransform: 'uppercase' as const, letterSpacing: '0.06em' }}>
              Federated · Private · Open
            </div>
            <h1 style={{ fontSize: 48, fontWeight: 800, color: '#102a43', lineHeight: 1.1, margin: '0 0 16px' }}>
              {name}
            </h1>
            <p style={{ fontSize: 18, color: '#627d98', lineHeight: 1.7, margin: '0 0 32px', maxWidth: 480 }}>
              {desc}
            </p>
            <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' as const }}>
              {canRegister ? (
                <Link to="/register" style={{ backgroundColor: '#486581', color: '#fff', fontWeight: 600, padding: '12px 24px', borderRadius: 12, textDecoration: 'none', fontSize: 16 }}>
                  {isWaitlist ? 'Join the waitlist' : 'Get started — free'}
                </Link>
              ) : (
                <div style={{ backgroundColor: '#f0f4f8', color: '#829ab1', fontWeight: 500, padding: '12px 24px', borderRadius: 12, fontSize: 14 }}>
                  Registrations currently closed
                </div>
              )}
              <Link to="/login" style={{ border: '1px solid #d9e2ec', color: '#334e68', fontWeight: 600, padding: '12px 24px', borderRadius: 12, textDecoration: 'none', fontSize: 16 }}>
                Sign in
              </Link>
            </div>
            {isWaitlist && (
              <p style={{ fontSize: 13, color: '#829ab1', marginTop: 12 }}>
                ℹ️ This instance is currently in waitlist mode — accounts are reviewed before approval.
              </p>
            )}
            {(userCount > 0 || postCount > 0) && (
              <div style={{ display: 'flex', gap: 32, marginTop: 40 }}>
                {userCount > 0 && (
                  <div>
                    <div style={{ fontSize: 28, fontWeight: 800, color: '#102a43' }}>{userCount.toLocaleString()}</div>
                    <div style={{ fontSize: 11, color: '#829ab1', textTransform: 'uppercase' as const, letterSpacing: '0.06em' }}>Members</div>
                  </div>
                )}
                {postCount > 0 && (
                  <div>
                    <div style={{ fontSize: 28, fontWeight: 800, color: '#102a43' }}>{postCount.toLocaleString()}</div>
                    <div style={{ fontSize: 11, color: '#829ab1', textTransform: 'uppercase' as const, letterSpacing: '0.06em' }}>Posts</div>
                  </div>
                )}
              </div>
            )}
          </div>
          <div style={{ flexShrink: 0 }}>
            <div style={{ width: 200, height: 200, borderRadius: 36, backgroundColor: '#486581', display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: '0 20px 40px rgba(72,101,129,0.3)' }}>
              <svg width="120" height="120" viewBox="0 0 96 96" fill="none">
                <path d="M48 8L16 80" stroke="white" strokeWidth="6.5" strokeLinecap="round"/>
                <path d="M48 8L80 80" stroke="white" strokeWidth="6.5" strokeLinecap="round"/>
                <line x1="30" y1="52" x2="66" y2="52" stroke="white" strokeWidth="6" strokeLinecap="round"/>
                <circle cx="24" cy="52" r="5" fill="#9fb3c8"/>
                <circle cx="72" cy="52" r="5" fill="#9fb3c8"/>
                <circle cx="16" cy="52" r="3.5" fill="#9fb3c8" opacity="0.55"/>
                <circle cx="80" cy="52" r="3.5" fill="#9fb3c8" opacity="0.55"/>
              </svg>
            </div>
          </div>
        </div>
      </div>

      {/* Features */}
      <div style={{ maxWidth: 960, margin: '0 auto', padding: '64px 24px' }}>
        <h2 style={{ textAlign: 'center' as const, fontSize: 26, fontWeight: 700, color: '#243b53', marginBottom: 8 }}>
          Social media, the way it should be
        </h2>
        <p style={{ textAlign: 'center' as const, color: '#829ab1', marginBottom: 48, fontSize: 16 }}>
          Built for real people, not engagement metrics.
        </p>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 20 }}>
          {FEATURES.map(f => (
            <div key={f.title} style={{ backgroundColor: '#fff', borderRadius: 16, padding: 24, border: '1px solid #d9e2ec' }}>
              <div style={{ fontSize: 32, marginBottom: 12 }}>{f.icon}</div>
              <div style={{ fontWeight: 600, color: '#243b53', marginBottom: 6, fontSize: 15 }}>{f.title}</div>
              <div style={{ fontSize: 13, color: '#829ab1', lineHeight: 1.6 }}>{f.desc}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Rules */}
      {rules.length > 0 && (
        <div style={{ maxWidth: 960, margin: '0 auto', padding: '0 24px 64px' }}>
          <div style={{ backgroundColor: '#fff', borderRadius: 16, border: '1px solid #d9e2ec', padding: 32 }}>
            <h2 style={{ fontSize: 20, fontWeight: 700, color: '#243b53', marginBottom: 24 }}>Instance rules</h2>
            {rules.map((rule, i) => (
              <div key={rule.id} style={{ display: 'flex', gap: 16, alignItems: 'flex-start', marginBottom: 12 }}>
                <span style={{ flexShrink: 0, width: 28, height: 28, borderRadius: '50%', backgroundColor: '#f0f4f8', color: '#486581', fontSize: 13, fontWeight: 700, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  {i + 1}
                </span>
                <span style={{ color: '#334e68', fontSize: 14, lineHeight: 1.6, paddingTop: 4 }}>{rule.text}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* CTA */}
      {canRegister && (
        <div style={{ backgroundColor: '#486581', padding: '64px 24px', textAlign: 'center' as const }}>
          <h2 style={{ fontSize: 32, fontWeight: 700, color: '#fff', marginBottom: 16 }}>
            {isWaitlist ? 'Join the waitlist' : 'Ready to join?'}
          </h2>
          <p style={{ color: '#d9e2ec', marginBottom: 32, fontSize: 18 }}>
            {isWaitlist
              ? 'Sign up and we\'ll review your account and send you an invite when you\'re approved.'
              : 'Create your account in seconds. No phone number required.'}
          </p>
          <Link to="/register" style={{ display: 'inline-block', backgroundColor: '#fff', color: '#334e68', fontWeight: 700, padding: '14px 32px', borderRadius: 12, textDecoration: 'none', fontSize: 18 }}>
            {isWaitlist ? 'Request access →' : 'Create your account'}
          </Link>
        </div>
      )}

      {/* Footer */}
      <div style={{ backgroundColor: '#fff', borderTop: '1px solid #d9e2ec', padding: '24px', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap' as const, gap: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: '#829ab1', fontSize: 13 }}>
          <svg width="18" height="18" viewBox="0 0 96 96" fill="none">
            <rect width="96" height="96" rx="20" fill="#486581"/>
            <path d="M48 16L24 72" stroke="white" strokeWidth="6" strokeLinecap="round"/>
            <path d="M48 16L72 72" stroke="white" strokeWidth="6" strokeLinecap="round"/>
            <line x1="34" y1="50" x2="62" y2="50" stroke="white" strokeWidth="5.5" strokeLinecap="round"/>
          </svg>
          <span>{name} &middot; Powered by Agora</span>
        </div>
        <div style={{ display: 'flex', gap: 24, fontSize: 13 }}>
          <Link to="/login" style={{ color: '#829ab1', textDecoration: 'none' }}>Sign in</Link>
          {canRegister && <Link to="/register" style={{ color: '#829ab1', textDecoration: 'none' }}>Register</Link>}
        </div>
      </div>

    </div>
  )
}
