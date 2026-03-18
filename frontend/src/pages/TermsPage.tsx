import { useEffect } from 'react'
import { Link } from 'react-router-dom'

const EFFECTIVE_DATE = 'March 18, 2026'
const OPERATOR = 'Koree A. Smith'
const CONTACT_EMAIL = 'koree@ameth.social'
const INSTANCE_DOMAIN = 'ameth.social'
const APP_NAME = 'Agora'

export default function TermsPage() {
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
          <Link to="/privacy" style={{ fontSize: 13, color: '#486581', textDecoration: 'none' }}>Privacy Policy →</Link>
        </div>
      </nav>

      {/* Header */}
      <div style={{ backgroundColor: '#102a43', padding: '56px 24px 48px' }}>
        <div style={{ maxWidth: 800, margin: '0 auto' }}>
          <div style={{ fontSize: 11, fontWeight: 600, letterSpacing: '0.15em', textTransform: 'uppercase', color: '#9fb3c8', marginBottom: 12 }}>Legal</div>
          <h1 style={{ fontSize: 40, fontWeight: 800, color: '#ffffff', margin: '0 0 12px', lineHeight: 1.1 }}>Terms of Service</h1>
          <p style={{ color: '#829ab1', fontSize: 15, margin: 0 }}>Effective date: {EFFECTIVE_DATE}</p>
        </div>
      </div>

      {/* Content */}
      <div style={{ maxWidth: 800, margin: '0 auto', padding: '48px 24px 80px' }}>
        <div style={{ backgroundColor: '#fff', borderRadius: 16, border: '1px solid #d9e2ec', padding: '48px 48px', lineHeight: 1.8 }}>

          <p style={p}>
            These Terms of Service ("Terms") govern your access to and use of {APP_NAME} at {INSTANCE_DOMAIN} and the {APP_NAME} mobile application (collectively, the "Service"), operated by {OPERATOR} ("we," "us," or "our"). By creating an account or using the Service, you agree to be bound by these Terms. If you do not agree, do not use the Service.
          </p>

          <Section title="1. Eligibility">
            <p style={p}>You must be at least 13 years of age to use the Service (or 16 years of age if you are located in the European Economic Area). By using the Service, you represent and warrant that you meet this age requirement and that you have the legal capacity to enter into these Terms. If you are using the Service on behalf of an organization, you represent that you have the authority to bind that organization to these Terms.</p>
          </Section>

          <Section title="2. Your Account">
            <p style={p}>You are responsible for maintaining the confidentiality of your account credentials and for all activity that occurs under your account. You agree to:</p>
            <ul style={ul}>
              <li style={li}>Provide accurate and truthful information when creating your account</li>
              <li style={li}>Keep your password secure and not share it with others</li>
              <li style={li}>Notify us immediately at <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a> if you believe your account has been compromised</li>
              <li style={li}>Not create more than one account per person, or create an account on behalf of another person without their permission</li>
            </ul>
            <p style={p}>We reserve the right to suspend or terminate accounts that violate these Terms.</p>
          </Section>

          <Section title="3. Acceptable Use">
            <p style={p}>You agree to use the Service only for lawful purposes and in a manner consistent with these Terms. You agree <strong>not</strong> to:</p>
            <ul style={ul}>
              <li style={li}>Post, share, or transmit content that is illegal, harassing, threatening, abusive, defamatory, obscene, or otherwise objectionable</li>
              <li style={li}>Harass, bully, intimidate, or harm other users</li>
              <li style={li}>Post content that infringes the intellectual property, privacy, or other rights of any person</li>
              <li style={li}>Impersonate any person or entity, or falsely state or misrepresent your affiliation with a person or entity</li>
              <li style={li}>Post spam, unsolicited commercial messages, or repetitive content</li>
              <li style={li}>Share content that promotes violence, hatred, or discrimination against any individual or group</li>
              <li style={li}>Share child sexual abuse material (CSAM) or any content that sexually exploits minors — this is grounds for immediate termination and will be reported to appropriate authorities</li>
              <li style={li}>Attempt to gain unauthorized access to any part of the Service or other users' accounts</li>
              <li style={li}>Use automated means (bots, scrapers, crawlers) to access the Service without prior written permission</li>
              <li style={li}>Interfere with or disrupt the integrity or performance of the Service</li>
              <li style={li}>Upload viruses or malicious code of any kind</li>
            </ul>
          </Section>

          <Section title="4. Your Content">
            <p style={p}><strong>Ownership:</strong> You retain ownership of all content you post to the Service ("Your Content"). We do not claim ownership over Your Content.</p>
            <p style={p}><strong>License to us:</strong> By posting content to the Service, you grant us a limited, non-exclusive, royalty-free license to store, display, and transmit Your Content solely as necessary to operate and provide the Service to you and other users. This license does not grant us the right to sell, license, or otherwise commercially exploit Your Content.</p>
            <p style={p}><strong>Your responsibility:</strong> You are solely responsible for Your Content and the consequences of posting it. You represent and warrant that:</p>
            <ul style={ul}>
              <li style={li}>You own or have the necessary rights to post Your Content</li>
              <li style={li}>Your Content does not violate the rights of any third party</li>
              <li style={li}>Your Content complies with these Terms and all applicable laws</li>
            </ul>
            <p style={p}><strong>Content removal:</strong> We reserve the right to remove any content that violates these Terms or that we determine, in our sole discretion, is harmful to the Service or its users.</p>
          </Section>

          <Section title="5. Privacy">
            <p style={p}>Your use of the Service is also governed by our <Link to="/privacy" style={link}>Privacy Policy</Link>, which is incorporated into these Terms by reference. Please review the Privacy Policy to understand our practices.</p>
          </Section>

          <Section title="6. Content Moderation">
            <p style={p}>We reserve the right, but not the obligation, to monitor, review, edit, or remove content on the Service. We may take action — including warning, suspension, or termination — against accounts that violate these Terms. We are not liable for any content posted by users.</p>
            <p style={p}>If you encounter content that you believe violates these Terms, please use the report functionality in the Service or contact us at <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a>.</p>
          </Section>

          <Section title="7. Intellectual Property">
            <p style={p}>The Service, including its design, software, trademarks, and content created by us, is owned by {OPERATOR} and is protected by applicable intellectual property laws. You may not copy, reproduce, modify, or create derivative works of the Service without our prior written permission.</p>
            <p style={p}>The name "{APP_NAME}" and associated logos are trademarks of {OPERATOR}. You may not use these marks without prior written permission.</p>
          </Section>

          <Section title="8. Third-Party Services">
            <p style={p}>The Service may allow you to link to or interact with third-party websites or services (for example, by sharing links). We have no control over and assume no responsibility for the content, privacy policies, or practices of any third-party sites or services. We encourage you to review the terms and privacy policies of any third-party services you use.</p>
          </Section>

          <Section title="9. Federation">
            <p style={p}>The Service may support federation with other Agora instances. When you interact with users on federated instances, your content and profile information may be shared with those instances. We are not responsible for the practices of other independently operated instances. You interact with federated instances at your own discretion.</p>
          </Section>

          <Section title="10. Service Availability and Changes">
            <p style={p}>We strive to keep the Service available at all times, but we do not guarantee uninterrupted availability. We may modify, suspend, or discontinue any part of the Service at any time. We will provide reasonable notice of material changes where practicable.</p>
            <p style={p}>We reserve the right to modify these Terms at any time. We will notify you of material changes by posting a notice on the Service and, where required, by email. Your continued use of the Service after such changes constitutes your acceptance of the updated Terms.</p>
          </Section>

          <Section title="11. Account Termination">
            <p style={p}><strong>By you:</strong> You may delete your account at any time through Settings. Account deletion is subject to a grace period, after which your data is permanently deleted.</p>
            <p style={p}><strong>By us:</strong> We reserve the right to suspend or terminate your account if you violate these Terms, with or without notice. For serious violations (such as CSAM), termination will be immediate. For other violations, we will generally attempt to provide notice and an opportunity to remedy the issue first, where appropriate.</p>
            <p style={p}>Upon termination, your right to use the Service ceases immediately. Provisions of these Terms that by their nature should survive termination will survive, including ownership provisions, warranty disclaimers, indemnity, and limitations of liability.</p>
          </Section>

          <Section title="12. Disclaimers">
            <p style={p}>THE SERVICE IS PROVIDED "AS IS" AND "AS AVAILABLE" WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NON-INFRINGEMENT.</p>
            <p style={p}>WE DO NOT WARRANT THAT THE SERVICE WILL BE UNINTERRUPTED, ERROR-FREE, OR FREE OF VIRUSES OR OTHER HARMFUL COMPONENTS. WE DO NOT WARRANT THE ACCURACY OR COMPLETENESS OF ANY CONTENT ON THE SERVICE.</p>
          </Section>

          <Section title="13. Limitation of Liability">
            <p style={p}>TO THE MAXIMUM EXTENT PERMITTED BY APPLICABLE LAW, {OPERATOR.toUpperCase()} SHALL NOT BE LIABLE FOR ANY INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL, OR PUNITIVE DAMAGES, INCLUDING LOSS OF DATA, LOSS OF PROFITS, OR LOSS OF GOODWILL, ARISING OUT OF OR RELATED TO YOUR USE OF OR INABILITY TO USE THE SERVICE.</p>
            <p style={p}>IN NO EVENT SHALL OUR TOTAL LIABILITY TO YOU FOR ALL CLAIMS ARISING OUT OF OR RELATED TO THESE TERMS OR THE SERVICE EXCEED THE GREATER OF (A) THE AMOUNT YOU PAID US IN THE TWELVE MONTHS PRECEDING THE CLAIM OR (B) ONE HUNDRED DOLLARS ($100).</p>
            <p style={p}>SOME JURISDICTIONS DO NOT ALLOW THE EXCLUSION OF CERTAIN WARRANTIES OR THE LIMITATION OF LIABILITY FOR CERTAIN TYPES OF DAMAGES, SO SOME OF THE ABOVE LIMITATIONS MAY NOT APPLY TO YOU.</p>
          </Section>

          <Section title="14. Indemnification">
            <p style={p}>You agree to indemnify, defend, and hold harmless {OPERATOR} from and against any claims, liabilities, damages, losses, and expenses (including reasonable legal fees) arising out of or related to your use of the Service, your violation of these Terms, or your violation of any rights of any third party.</p>
          </Section>

          <Section title="15. Governing Law and Dispute Resolution">
            <p style={p}>These Terms are governed by the laws of the State of Texas, United States, without regard to conflict of law principles. Any dispute arising under these Terms shall be resolved in the state or federal courts located in Texas, and you consent to personal jurisdiction in those courts.</p>
            <p style={p}>For users in the European Union: Nothing in these Terms affects your rights as a consumer under applicable EU law. You may also have the right to use the EU Online Dispute Resolution platform at <a href="https://ec.europa.eu/consumers/odr" style={link} target="_blank" rel="noopener noreferrer">ec.europa.eu/consumers/odr</a>.</p>
          </Section>

          <Section title="16. Contact">
            <p style={p}>If you have questions about these Terms, please contact:</p>
            <div style={{ backgroundColor: '#f0f4f8', borderRadius: 10, padding: '16px 20px', marginTop: 8 }}>
              <p style={{ margin: '0 0 4px', fontWeight: 600, color: '#102a43' }}>{OPERATOR}</p>
              <p style={{ margin: '0 0 4px', color: '#486581' }}>Operator of {APP_NAME} ({INSTANCE_DOMAIN})</p>
              <a href={`mailto:${CONTACT_EMAIL}`} style={link}>{CONTACT_EMAIL}</a>
            </div>
          </Section>

        </div>

        {/* Footer nav */}
        <div style={{ display: 'flex', justifyContent: 'center', gap: 32, marginTop: 32, fontSize: 14 }}>
          <Link to="/" style={{ color: '#486581', textDecoration: 'none' }}>← Back to {APP_NAME}</Link>
          <Link to="/privacy" style={{ color: '#486581', textDecoration: 'none' }}>Privacy Policy →</Link>
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
