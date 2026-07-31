import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, AlertCircle, Clock, Globe } from 'lucide-react'
import { customDomainApi } from '../../api'

/**
 * Custom domain setup (AGORA-284) — the primary entry point for pointing a
 * domain you own at your account so it becomes your Bluesky handle.
 *
 * The DNS record and well-known file contents are rendered from what the
 * server sends rather than assembled here: the server is what will actually
 * go looking for them, and two independent constructions of the same string
 * would eventually disagree, presenting to the user as "I did exactly what
 * you told me and it says it failed."
 */
export default function CustomDomainPanel() {
  const qc = useQueryClient()
  const [domain, setDomain] = useState('')
  const [err, setErr] = useState('')
  const [copied, setCopied] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['custom-domain'],
    queryFn: () => customDomainApi.get().then(r => r.data),
  })

  const refresh = () => qc.invalidateQueries({ queryKey: ['custom-domain'] })
  const fail = (e: any) => setErr(e.response?.data?.error || 'Something went wrong')

  const claim = useMutation({
    mutationFn: () => customDomainApi.claim(domain),
    onSuccess: () => { setDomain(''); setErr(''); refresh() },
    onError: fail,
  })

  const verify = useMutation({
    mutationFn: () => customDomainApi.verify(),
    onSuccess: () => { setErr(''); refresh() },
    onError: fail,
  })

  const release = useMutation({
    mutationFn: () => customDomainApi.release(),
    onSuccess: () => { setErr(''); refresh() },
    onError: fail,
  })

  const copy = (label: string, value: string) => {
    navigator.clipboard.writeText(value)
    setCopied(label)
    setTimeout(() => setCopied(''), 1500)
  }

  if (isLoading) {
    return (
      <div className="card p-4">
        <h3 className="font-semibold">Custom domain</h3>
        <p className="text-sm text-agora-400 mt-2">Loading…</p>
      </div>
    )
  }

  // AT Proto being off — instance-wide or for this account — makes a custom
  // handle meaningless rather than merely unavailable: nothing would ever
  // resolve it. Say so instead of offering a form that can't work.
  if (data && data.available === false) {
    return (
      <div className="card p-4 space-y-2">
        <h3 className="font-semibold">Custom domain</h3>
        <p className="text-sm text-agora-500">{data.unavailable_reason}</p>
      </div>
    )
  }

  const claimed = data?.claim
  const inst = data?.instructions

  return (
    <div className="card p-4 space-y-4">
      <div>
        <h3 className="font-semibold">Custom domain</h3>
        <p className="text-sm text-agora-500 mt-1">
          Use a domain you own as your handle on Bluesky and the wider AT Protocol network — so people
          find you as <span className="font-medium">@your-domain.example</span> instead of{' '}
          <span className="font-medium">@{data?.fallback_handle}</span>. Your account, posts, and followers
          are unaffected either way; this only changes the name people see and search for.
        </p>
      </div>

      <div className="rounded-lg bg-agora-50 dark:bg-agora-800/50 px-3 py-2 flex items-center gap-2">
        <Globe size={15} className="text-agora-400 flex-shrink-0" />
        <span className="text-sm">
          Your handle right now: <span className="font-medium">{data?.current_handle}</span>
        </span>
      </div>

      {err && (
        <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2 text-sm text-red-700 dark:text-red-400">
          {err}
        </div>
      )}

      {!claimed && (
        <div className="space-y-2">
          <label className="label">Domain</label>
          <div className="flex gap-2">
            <input
              className="input flex-1"
              placeholder="example.com"
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              value={domain}
              onChange={e => setDomain(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && domain.trim()) claim.mutate() }}
            />
            <button
              onClick={() => claim.mutate()}
              disabled={claim.isPending || !domain.trim()}
              className="btn-primary whitespace-nowrap"
            >
              {claim.isPending ? 'Saving…' : 'Continue'}
            </button>
          </div>
          <p className="text-xs text-agora-400">
            Enter the domain itself, not a URL. You'll get a record to add at your DNS provider on the next step —
            nothing changes until you've added it and it verifies.
          </p>
        </div>
      )}

      {claimed && (
        <div className="space-y-4">
          <StatusBanner claim={claimed} approvalMode={data?.approval_mode} />

          {/* Instructions stay visible even once live: a user re-checking why
              their handle broke, or moving DNS providers, needs the record
              they're supposed to have, not just a note that it worked once. */}
          {claimed.approval_status !== 'rejected' && inst && (
            <div className="space-y-3">
              <p className="text-sm text-agora-500">
                Add <span className="font-medium">either one</span> of these at your DNS provider or web host to prove
                you own <span className="font-medium">{claimed.domain}</span>. The DNS record is the usual choice; the
                file is there for hosts that don't let you edit DNS.
              </p>

              <div className="rounded-lg border border-agora-200 dark:border-agora-700 p-3 space-y-2">
                <p className="text-xs font-semibold uppercase tracking-wide text-agora-400">Option 1 — DNS record</p>
                <Field label="Type"  value={inst.dns_record_type}  copied={copied} onCopy={copy} />
                <Field label="Name"  value={inst.dns_record_name}  copied={copied} onCopy={copy} />
                <Field label="Value" value={inst.dns_record_value} copied={copied} onCopy={copy} />
                <p className="text-xs text-agora-400">
                  Some providers append the domain to the name automatically — if yours does, enter just{' '}
                  <code className="text-xs">_atproto</code>. DNS changes can take a few minutes to an hour to spread.
                </p>
              </div>

              <div className="rounded-lg border border-agora-200 dark:border-agora-700 p-3 space-y-2">
                <p className="text-xs font-semibold uppercase tracking-wide text-agora-400">Option 2 — file on your site</p>
                <Field label="URL"      value={inst.well_known_url}     copied={copied} onCopy={copy} />
                <Field label="Contents" value={inst.well_known_content} copied={copied} onCopy={copy} />
                <p className="text-xs text-agora-400">
                  Must be served over HTTPS, return the text above and nothing else, and not redirect anywhere.
                </p>
              </div>
            </div>
          )}

          <div className="flex gap-2 flex-wrap">
            {claimed.approval_status !== 'rejected' && (
              <button onClick={() => verify.mutate()} disabled={verify.isPending} className="btn-primary">
                {verify.isPending ? 'Checking…' : 'Check verification'}
              </button>
            )}
            <button
              onClick={() => {
                if (confirm(`Remove ${claimed.domain}? Your handle goes back to ${data?.fallback_handle}.`)) release.mutate()
              }}
              disabled={release.isPending}
              className="btn-secondary"
            >
              {claimed.approval_status === 'rejected' ? 'Try a different domain' : 'Remove domain'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function Field({ label, value, copied, onCopy }: {
  label: string
  value: string
  copied: string
  onCopy: (label: string, value: string) => void
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-agora-400 w-16 flex-shrink-0">{label}</span>
      <code className="flex-1 min-w-0 truncate text-xs bg-agora-100 dark:bg-agora-800 rounded px-2 py-1">{value}</code>
      <button onClick={() => onCopy(label, value)} className="btn-ghost text-xs flex items-center gap-1 flex-shrink-0">
        {copied === label ? <Check size={13} /> : <Copy size={13} />}
        {copied === label ? 'Copied' : 'Copy'}
      </button>
    </div>
  )
}

/**
 * The two status axes are deliberately collapsed into one plain-language
 * sentence here. "Verified" and "approved" are meaningful to the server and
 * meaningless to someone who just wants to know whether their handle works —
 * so each combination gets its own answer to that question, and the next
 * action if the answer is no.
 */
function StatusBanner({ claim, approvalMode }: { claim: any, approvalMode?: string }) {
  if (claim.live) {
    return (
      <Banner tone="ok" icon={<Check size={16} />}>
        <span className="font-medium">{claim.domain}</span> is verified and live — it's your handle on Bluesky now.
        {claim.verification_method === 'well-known'
          ? ' Keep the file in place; we re-check it periodically.'
          : ' Keep the DNS record in place; we re-check it periodically.'}
      </Banner>
    )
  }
  if (claim.approval_status === 'rejected') {
    return (
      <Banner tone="bad" icon={<AlertCircle size={16} />}>
        Your request for <span className="font-medium">{claim.domain}</span> was declined by an administrator.
        {claim.rejection_reason && <> Reason: {claim.rejection_reason}</>}
      </Banner>
    )
  }
  if (claim.verification_status === 'verified') {
    return (
      <Banner tone="wait" icon={<Clock size={16} />}>
        <span className="font-medium">{claim.domain}</span> is verified and waiting for an administrator to approve it.
        {approvalMode === 'manual' && ' New domains on this instance are reviewed by hand.'} You'll be notified either way.
      </Banner>
    )
  }
  if (claim.verification_status === 'failed') {
    return (
      <Banner tone="bad" icon={<AlertCircle size={16} />}>
        We couldn't verify <span className="font-medium">{claim.domain}</span> yet.
        {claim.last_error && <> {claim.last_error}</>}
        {' '}Add one of the records below, then check again — new DNS records often take a little while to spread.
      </Banner>
    )
  }
  return (
    <Banner tone="wait" icon={<Clock size={16} />}>
      <span className="font-medium">{claim.domain}</span> is claimed but not verified yet. Add one of the records below,
      then press Check verification.
    </Banner>
  )
}

function Banner({ tone, icon, children }: { tone: 'ok'|'wait'|'bad', icon: React.ReactNode, children: React.ReactNode }) {
  const tones = {
    ok:   'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800 text-green-800 dark:text-green-300',
    wait: 'bg-amber-50 dark:bg-amber-900/20 border-amber-200 dark:border-amber-800 text-amber-800 dark:text-amber-300',
    bad:  'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800 text-red-700 dark:text-red-400',
  }
  return (
    <div className={`rounded-lg border px-3 py-2 text-sm flex gap-2 ${tones[tone]}`}>
      <span className="flex-shrink-0 mt-0.5">{icon}</span>
      <span>{children}</span>
    </div>
  )
}
