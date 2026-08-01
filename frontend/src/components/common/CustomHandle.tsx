import { BadgeCheck } from 'lucide-react'

/**
 * A user's verified custom domain, shown as their handle on Bluesky and the
 * wider AT Protocol network (AGORA-288).
 *
 * Rendered *alongside* @username rather than replacing it. Within Agora,
 * @username is the identity: it's what mentions resolve against and what
 * profile links are built from, so swapping it out here would make the name
 * people see stop matching the name they can type. The domain is the extra
 * fact — the one that carries a verification claim — so it gets the badge and
 * @username stays put.
 *
 * Renders nothing without a domain, which is the common case: the component
 * can be dropped in unconditionally wherever a name appears.
 */
export default function CustomHandle({ domain, size = 'sm' }: { domain?: string, size?: 'sm' | 'md' }) {
  if (!domain) return null
  const text = size === 'md' ? 'text-sm' : 'text-xs'
  const icon = size === 'md' ? 13 : 11
  return (
    <span
      title={`${domain} — a domain this account has verified it controls`}
      className={`inline-flex items-center gap-1 ${text} text-sky-600 dark:text-sky-400 min-w-0`}
    >
      <BadgeCheck size={icon} className="flex-shrink-0" />
      <span className="truncate">{domain}</span>
    </span>
  )
}
