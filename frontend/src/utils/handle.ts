/**
 * Returns the display handle for a user.
 * Local users:  @username
 * Remote users: @username@instance.com
 */
export function handle(username: string, isRemote?: boolean, remoteInstance?: string): string {
  // Remote users' username column is already the full synthetic
  // "handle@instance" form (see upsertRemoteAPUser/getOrCreateRemoteUser) —
  // only append remoteInstance if username isn't already qualified, so a
  // remote user doesn't end up doubled as "@handle@instance@instance".
  //
  // AT Proto actors are the exception: remoteInstance is 'bsky.app' there,
  // but it's an internal origin marker, not a real instance domain, and
  // username already holds the full Bluesky handle (e.g. foo.bsky.social),
  // so appending it would double up as "@foo.bsky.social@bsky.app".
  if (isRemote && remoteInstance && remoteInstance !== 'bsky.app' && !username.includes('@')) {
    return `@${username}@${remoteInstance}`
  }
  return `@${username}`
}

/**
 * AGORA-416: whether a search query is someone's full handle rather than a
 * name to match. The backend's /federation/lookup resolves all of these to a
 * profile on whatever network they live on:
 *   @user@instance.com             fediverse (or another Agora instance)
 *   foo.bsky.social / koree.net    Bluesky, the bare domain handle
 *   @foo.bsky.social@bsky.app      Bluesky, the way this app displays one
 * A leading @ is optional on all of them.
 */
export function isFullHandle(q: string): boolean {
  const h = q.trim().replace(/^@/, '')
  const at = h.indexOf('@')
  if (at < 0) return isDomain(h)
  return /^[^\s@]+$/.test(h.slice(0, at)) && isDomain(h.slice(at + 1))
}

/** The bare-domain form of isFullHandle, i.e. only ever a Bluesky handle. */
export function isDomainHandle(q: string): boolean {
  const h = q.trim().replace(/^@/, '')
  return !h.includes('@') && isDomain(h)
}

// Dot-separated hostname labels, with a final label that isn't all digits so
// an IP address or a version number like 4.0.1 doesn't count.
function isDomain(h: string): boolean {
  return h.length <= 253
    && /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]*[a-z][a-z0-9-]*$/i.test(h)
}

/**
 * Returns the profile link path for a user.
 * Remote users are stored with username@instance in the username field,
 * so the profile link just uses the username as-is.
 */
export function profilePath(username: string): string {
  return `/profile/${username}`
}
