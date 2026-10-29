import { Lock } from 'lucide-react'

/**
 * A lock over the bottom-right of a remote account's avatar, marking an
 * account that approves follow requests by hand (AGORA-306).
 *
 * Only meaningful for the fediverse. ActivityPub's manuallyApprovesFollowers
 * has no AT Protocol counterpart: a Bluesky follow is a public repo write with
 * no Accept or Reject to wait on, so a Bluesky account can never be locked and
 * callers should never pass one here.
 *
 * The badge answers a question the follow button alone can't. A follow of a
 * locked account is sent and then simply sits there, and without this the
 * silence is indistinguishable from a delivery failure. It clears the moment
 * the account accepts (or already follows you back), which is what makes it a
 * gate indicator rather than a permanent property badge.
 *
 * Renders nothing unless locked, so it can be dropped in unconditionally
 * wherever a remote avatar appears.
 */
export default function LockedBadge({
  locked,
  pending,
  size = 'sm',
}: {
  locked?: boolean
  /** A follow has been sent and is waiting on approval, vs. not following yet. */
  pending?: boolean
  size?: 'sm' | 'md'
}) {
  if (!locked) return null

  // Sized against the avatar it sits on: 36px rows in Connections, the 80px
  // avatar on a profile. The ring matches each avatar's own backdrop so the
  // badge reads as sitting on top of the photo rather than punched out of it.
  const box  = size === 'md' ? 'w-6 h-6' : 'w-[18px] h-[18px]'
  const icon = size === 'md' ? 13 : 10

  const label = pending
    ? 'Follow request pending approval'
    : 'Requires follow approval'

  return (
    <span
      title={label}
      aria-label={label}
      role="img"
      className={`absolute -bottom-0.5 -right-0.5 ${box} flex items-center justify-center rounded-full
                  bg-agora-700 text-white ring-2 ring-white dark:bg-agora-600 dark:ring-agora-800`}
    >
      <Lock size={icon} strokeWidth={2.5} />
    </span>
  )
}
