/**
 * Returns the display handle for a user.
 * Local users:  @username
 * Remote users: @username@instance.com
 */
export function handle(username: string, isRemote?: boolean, remoteInstance?: string): string {
  if (isRemote && remoteInstance) {
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
