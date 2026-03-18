import { useEffect } from 'react'
import { Link } from 'react-router-dom'

const EFFECTIVE_DATE = 'March 18, 2026'
const OPERATOR = 'Koree A. Smith'
const CONTACT_EMAIL = 'koree@ameth.social'
const INSTANCE_DOMAIN = 'ameth.social'
const APP_NAME = 'Agora'

export default function PrivacyPage() {
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
          <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '0.15em', textTransform: 'uppercase', color: '#9fb3c8', marginBottom: 12 }}>Legal</div>
          <h1 style={{ fontSize: 40, fontWeight: 800, color: '#ffffff', margin: '0 0 12px', lineHeight: 1.1 }}>Privacy Policy</h1>
          <p style={{ color: '#829ab1', fontSize: 15, margin: 0 }}>Effective date: {EFFECTIVE_DATE}</p>
        </div>
      </div>

      {/* Content */}
      <div style={{ maxWidth: 800, margin: '0 auto', padding: '48px 24px 80px' }}>
        <div style={{ backgroundColor: '#fff', borderRadius: 16, border: '1px solid #d9e2ec', padding: '48px 48px', lineHeight: 1.8 }}>

          <p style={p}>
            This Privacy Policy describes how {OPERATOR} ("we," "us," or "our") collects, uses, and shares information about you when you use {APP_NAME} at {INSTANCE_DOMAIN} and the {APP_NAME} mobile application (collectively, the "Service"). By using the Service, you agree to the collection and use of information in accordance with this policy.
          </p>

          <Section title="1. Information We Collect">
            <p style={p}><strong>Information you provide directly:</strong></p>
            <ul style={ul}>
              <li style={li}><strong>Account information:</strong> Username, email address, password (stored as a one-way hash), and optional profile details such as display name, bio, location, pronouns, and profile/cover photos.</li>
              <li style={li}><strong>Content:</strong> Posts, comments, reactions, direct messages, group posts, photos, and other content you create or share on the Service.</li>
              <li style={li}><strong>Communications:</strong> Messages you send to other users through the Service.</li>
            </ul>
            <p style={p}><strong>Information collected automatically:</strong></p>
            <ul style={ul}>
              <li style={li}><strong>Log data:</strong> When you access the Service, our servers automatically record information including your IP address, browser type, pages visited, and the date and time of your request. This data is used solely for security and operational purposes and is not shared with third parties.</li>
              <li style={li}><strong>Device information:</strong> If you use our mobile app, we may collect your device type and operating system version to support push notifications.</li>
              <li style={li}><strong>Push notification tokens:</strong> If you opt in to push notifications on mobile, we store your Expo push token to deliver notifications to your device.</li>
            </ul>
            <p style={p}><strong>We do not collect:</strong> We do not use advertising networks, tracking pixels, third-party analytics, or behavioral profiling. We do not sell your data.</p>
          </Section>

          <Section title="2. How We Use Your Information">
            <p style={p}>We use the information we collect solely to provide and improve the Service:</p>
            <ul style={ul}>
              <li style={li}>To create and manage your account</li>
              <li style={li}>To display your content to people you choose to share it with</li>
              <li style={li}>To send you email notifications you have opted into (account verification, password resets, activity notifications)</li>
              <li style={li}>To send push notifications to your mobile device, if you have enabled them</li>
              <li style={li}>To enforce our Terms of Service and protect the safety of our users</li>
              <li style={li}>To comply with legal obligations</li>
            </ul>
            <p style={p}>We do not use your personal information for advertising, profiling, or sale to third parties under any circumstances.</p>
          </Section>

          <Section title="3. Legal Basis for Processing (GDPR)">
            <p style={p}>If you are located in the European Economic Area (EEA), we process your personal data on the following legal bases:</p>
            <ul style={ul}>
              <li style={li}><strong>Contractual necessity:</strong> Processing your account information and content is necessary to provide the Service you signed up for.</li>
              <li style={li}><strong>Legitimate interests:</strong> We process log data to maintain security and prevent abuse, which is in our legitimate interest and does not override your rights.</li>
              <li style={li}><strong>Consent:</strong> Where we send optional notifications, we do so based on your consent, which you may withdraw at any time.</li>
              <li style={li}><strong>Legal obligation:</strong> We may process data to comply with applicable law.</li>
            </ul>
          </Section>

          <Section title="4. Data Sharing and Disclosure">
            <p style={p}>We do not sell, rent, or trade your personal information. We may share your information only in the following limited circumstances:</p>
            <ul style={ul}>
              <li style={li}><strong>With other users:</strong> Content you post is visible to the audience you select (friends, group members, etc.). Your username and public profile information are visible to other logged-in users of the Service.</li>
              <li style={li}><strong>Federation:</strong> If federation is enabled on this instance, some public profile information and posts may be shared with other Agora instances you interact with. This is disclosed to you at the time of interaction.</li>
              <li style={li}><strong>Service providers:</strong> We use a small number of infrastructure providers (hosting, email delivery) who process data on our behalf under data processing agreements and are prohibited from using your data for any other purpose.</li>
              <li style={li}><strong>Legal requirements:</strong> We may disclose information if required by law, court order, or to protect the rights and safety of our users or the public.</li>
              <li style={li}><strong>Business transfer:</strong> If the Service is transferred to another operator, we will notify you and give you the opportunity to delete your account before any transfer takes effect.</li>
            </ul>
          </Section>

          <Section title="5. Data Retention">
            <p style={p}>We retain your personal information for as long as your account is active. When you request account deletion:</p>
            <ul style={ul}>
              <li style={li}>Your account is scheduled for deletion after a grace period (typically 30 days), during which you may cancel the deletion.</li>
              <li style={li}>After the grace period, your account, posts, messages, and associated personal data are permanently deleted from our systems.</li>
              <li style={li}>Server log data is retained for a maximum of 90 days for security purposes, after which it is deleted.</li>
              <li style={li}>Residual copies may exist in backups for up to 30 days after deletion from our live systems.</li>
            </ul>
          </Section>

          <Section title="6. Your Rights">
            <p style={p}><strong>All users</strong> have the right to:</p>
            <ul style={ul}>
              <li style={li}>Access and export a copy of your personal data (available via Settings → Data → Export)</li>
              <li style={li}>Correct inaccurate information in your profile</li>
              <li style={li}>Delete your account and associated data</li>
              <li style={li}>Opt out of email notifications at any time via Settings or the unsubscribe link in any email</li>
            </ul>
            <p style={p}><strong>EEA/UK residents (GDPR)</strong> additionally have the right to:</p>
            <ul style={ul}>
              <li style={li}>Request restriction of processing of your personal data</li>
              <li style={li}>Object to processing based on legitimate interests</li>
              <li style={li}>Data portability — receive your data in a structured, machine-readable format</li>
              <li style={li}>Lodge a complaint with your local data protection authority</li>
            </ul>
            <p style={p}><strong>California residents (CCPA/CPRA)</strong> additionally have the right to:</p>
            <ul style={ul}>
              <li style={li}>Know what personal information is collected about you and how it is used</li>
              <li style={li}>Delete personal information we have collected from you</li>
              <li style={li}>Opt out of the sale or sharing of personal information — <em>we do not sell or share personal information</em></li>
              <li style={li}>Non-discrimination for exercising your privacy rights</li>
              <li style={li}>Correct inaccurate personal information</li>
              <li style={li}>Limit the use of sensitive personal information — we collect only what is necessary to operate the Service</li>
            </ul>
            <p style={p}>To exercise any of these rights, contact us at <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a>. We will respond within 30 days (or within the timeframe required by applicable law).</p>
          </Section>

          <Section title="7. Data Security">
            <p style={p}>We take reasonable technical and organizational measures to protect your personal information, including:</p>
            <ul style={ul}>
              <li style={li}>Passwords are stored using bcrypt hashing and are never stored in plaintext</li>
              <li style={li}>All data in transit is encrypted using TLS/HTTPS</li>
              <li style={li}>Access to production systems is restricted to authorized personnel only</li>
              <li style={li}>Regular security updates are applied to server infrastructure</li>
            </ul>
            <p style={p}>No method of transmission over the Internet or electronic storage is 100% secure. While we strive to use commercially acceptable means to protect your data, we cannot guarantee its absolute security.</p>
          </Section>

          <Section title="8. Children's Privacy">
            <p style={p}>The Service is not directed to children under the age of 13 (or 16 in the EEA). We do not knowingly collect personal information from children. If you believe we have inadvertently collected information from a child, please contact us immediately at <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a> and we will delete it promptly.</p>
          </Section>

          <Section title="9. International Data Transfers">
            <p style={p}>The Service is operated from the United States. If you are accessing the Service from outside the United States, please be aware that your information may be transferred to, stored, and processed in the United States. By using the Service, you consent to the transfer of your information to the United States.</p>
            <p style={p}>For users in the EEA, we ensure that any such transfers are subject to appropriate safeguards in accordance with GDPR requirements.</p>
          </Section>

          <Section title="10. Cookies">
            <p style={p}>The Service uses only a single session authentication token stored in your browser's local storage. We do not use third-party cookies, advertising cookies, or tracking cookies of any kind. You can clear your local storage at any time through your browser settings, which will log you out of the Service.</p>
          </Section>

          <Section title="11. Changes to This Policy">
            <p style={p}>We may update this Privacy Policy from time to time. We will notify you of material changes by posting a notice on the Service and, where required by law, by sending you an email. The date at the top of this policy indicates when it was last updated. Your continued use of the Service after any changes constitutes your acceptance of the new policy.</p>
          </Section>

          <Section title="12. Contact Us">
            <p style={p}>If you have questions, concerns, or requests regarding this Privacy Policy or our data practices, please contact:</p>
            <div style={{ backgroundColor: '#f0f4f8', borderRadius: 10, padding: '16px 20px', marginTop: 8 }}>
              <p style={{ margin: '0 0 4px', fontWeight: 600, color: '#102a43' }}>{OPERATOR}</p>
              <p style={{ margin: '0 0 4px', color: '#486581' }}>Operator of {APP_NAME} ({INSTANCE_DOMAIN})</p>
              <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a>
            </div>
            <p style={{ ...p, marginTop: 16 }}>We are committed to working with you to obtain a fair resolution of any complaint or concern about privacy. If you are located in the EEA and believe we have not adequately addressed your concern, you have the right to contact your national data protection authority.</p>
          </Section>

        </div>

        {/* Footer nav */}
        <div style={{ display: 'flex', justifyContent: 'center', gap: 32, marginTop: 32, fontSize: 14 }}>
          <Link to="/" style={{ color: '#486581', textDecoration: 'none' }}>← Back to {APP_NAME}</Link>
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

const p: React.CSSProperties  = { margin: '0 0 14px', fontSize: 15, color: '#334e68', lineHeight: 1.8 }
const ul: React.CSSProperties = { margin: '0 0 14px', paddingLeft: 24 }
const li: React.CSSProperties = { fontSize: 15, color: '#334e68', lineHeight: 1.8, marginBottom: 6 }
const link: React.CSSProperties = { color: '#486581', textDecoration: 'underline' }
