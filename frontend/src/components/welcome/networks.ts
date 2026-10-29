import type { InstanceInfo } from './types'

// AGORA-389: the external networks this instance has turned on, in the form
// running copy names them ("Bluesky and the Fediverse"). Empty when the admin
// has both off, in which case copy about federation is left out entirely.
export function instanceNetworks(instance: InstanceInfo): string[] {
  const names: string[] = []
  if (instance.atproto_enabled) names.push('Bluesky')
  if (instance.activitypub_enabled) names.push('the Fediverse')
  return names
}
