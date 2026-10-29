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
 * Returns the profile link path for a user.
 * Remote users are stored with username@instance in the username field,
 * so the profile link just uses the username as-is.
 */
export function profilePath(username: string): string {
  return `/profile/${username}`
}
