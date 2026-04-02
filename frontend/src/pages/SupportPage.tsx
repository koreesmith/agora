import { useEffect } from 'react'
import { Link } from 'react-router-dom'

const APP_NAME = 'Agora'
const CONTACT_EMAIL = 'koree@ameth.social'
const INSTANCE_DOMAIN = 'agorasocial.online'

export default function SupportPage() {
  useEffect(() => { window.scrollTo(0, 0) }, [])

  return (
    <div style={{ minHeight: '100vh', backgroundColor: '#f0f4f8', fontFamily: 'Georgia, serif' }}>

      {/* Nav */}
      <nav style={{ backgroundColor: '#fff', borderBottom: '1px solid #d9e2ec', position: 'sticky', top: 0, zIndex: 20 }}>
        <div style={{ maxWidth: 800, margin: '0 auto', padding: '12px 24px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <Link to="/" style={{ display: 'flex', alignItems: 'center', gap: 10, textDecoration: 'none' }}>
            <svg width="28" height="28" viewBox="0 0 96 96" fill="none">
              <rect width="96" height="96" rx="20" fill="#486581"/>
              <path d="M48 16L24 72" stroke="white" strokeWidth="6" strokeLinecap="round"/>
              <path d="M48 16L72 72" stroke="white" strokeWidth="6" strokeLinecap="round"/>
              <line x1="34" y1="50" x2="62" y2="50" stroke="white" strokeWidth="5.5" strokeLinecap="round"/>
              <circle cx="28" cy="50" r="4" fill="#9fb3c8"/>
              <circle cx="68" cy="50" r="4" fill="#9fb3c8"/>
            </svg>
            <span style={{ fontWeight: 700, fontSize: 16, color: '#102a43' }}>{APP_NAME}</span>
          </Link>
          <Link to="/terms" style={{ fontSize: 13, color: '#486581', textDecoration: 'none' }}>Terms of Service →</Link>
        </div>
      </nav>

      {/* Header */}
      <div style={{ backgroundColor: '#102a43', padding: '56px 24px 48px' }}>
        <div style={{ maxWidth: 800, margin: '0 auto' }}>
          <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '0.15em', textTransform: 'uppercase', color: '#9fb3c8', marginBottom: 12 }}>Help &amp; Support</div>
          <h1 style={{ fontSize: 40, fontWeight: 800, color: '#ffffff', margin: '0 0 12px', lineHeight: 1.1 }}>Support</h1>
          <p style={{ color: '#829ab1', fontSize: 15, margin: 0 }}>We're here to help you get the most out of {APP_NAME}.</p>
        </div>
      </div>

      {/* Content */}
      <div style={{ maxWidth: 800, margin: '0 auto', padding: '48px 24px 80px' }}>
        <div style={{ backgroundColor: '#fff', borderRadius: 16, border: '1px solid #d9e2ec', padding: '48px 48px', lineHeight: 1.8 }}>

          {/* Contact */}
          <Section title="Contact Us">
            <p style={p}>
              If you have questions, need assistance, or want to report an issue, the best way to reach us is by email.
              We typically respond within 1–2 business days.
            </p>
            <div style={{ backgroundColor: '#f0f4f8', borderRadius: 10, padding: '16px 20px', marginTop: 8 }}>
              <p style={{ margin: '0 0 4px', fontWeight: 600, color: '#102a43' }}>Email Support</p>
              <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a>
            </div>
          </Section>

          {/* Account issues */}
          <Section title="Account Issues">
            <p style={p}><strong>Forgot your password?</strong> Use the <Link to="/reset-password" style={link}>password reset page</Link> to receive a reset link by email.</p>
            <p style={p}><strong>Didn't receive a verification email?</strong> Check your spam or junk folder. If you still don't see it, contact us at <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a> with your username.</p>
            <p style={p}><strong>Need to delete your account?</strong> You can delete your account at any time from the Settings page once you're logged in. Account deletion is subject to a brief grace period before your data is permanently removed.</p>
            <p style={p}><strong>Want to change your email?</strong> You can update your email address from the Settings page. A verification link will be sent to your new address.</p>
          </Section>

          {/* Reporting */}
          <Section title="Reporting Abuse or Violations">
            <p style={p}>
              If you encounter content or behavior that violates our{' '}
              <Link to="/terms" style={link}>Terms of Service</Link>, please use the in-app report functionality
              or send a description of the issue to <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a>.
            </p>
            <p style={p}>
              We take reports of harassment, spam, and harmful content seriously and will review all reports promptly.
            </p>
          </Section>

          {/* Federation */}
          <Section title="Federation and Other Instances">
            <p style={p}>
              {APP_NAME} supports federation with other compatible instances. If you are experiencing issues related to
              federated content from another server, please note that we only have control over content and accounts on{' '}
              <strong>{INSTANCE_DOMAIN}</strong>. For issues originating from other instances, you should contact
              the administrators of that instance directly.
            </p>
          </Section>

          {/* Privacy */}
          <Section title="Privacy and Data Requests">
            <p style={p}>
              For questions about how your data is collected or used, please review our{' '}
              <Link to="/privacy" style={link}>Privacy Policy</Link>. To request a copy of your data or to
              exercise any other privacy rights, contact us at{' '}
              <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a>.
            </p>
          </Section>

        </div>

        {/* Footer nav */}
        <div style={{ display: 'flex', justifyContent: 'center', gap: 32, marginTop: 32, fontSize: 14 }}>
          <Link to="/" style={{ color: '#486581', textDecoration: 'none' }}>← Back to {APP_NAME}</Link>
          <Link to="/privacy" style={{ color: '#486581', textDecoration: 'none' }}>Privacy Policy</Link>
          <Link to="/terms" style={{ color: '#486581', textDecoration: 'none' }}>Terms of Service →</Link>
        </div>
      </div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginTop: 40 }}>
      <h2 style={{ fontSize: 20, fontWeight: 700, color: '#102a43', marginBottom: 16, paddingBottom: 10, borderBottom: '1px solid #e8edf2' }}>{title}</h2>
      {children}
    </div>
  )
}

const p: React.CSSProperties = { margin: '0 0 14px', fontSize: 15, color: '#334e68', lineHeight: 1.8 }
const link: React.CSSProperties = { color: '#486581', textDecoration: 'underline' }
