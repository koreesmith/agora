import type { QueryClient } from '@tanstack/react-query'

// AGORA-456: reacting to a post changes that one post in place, wherever it is
// held in the query cache (the timeline, a profile, a group, bookmarks, the
// post's own page), instead of fetching the whole list again. A refetch that
// replaces a paged timeline lets anything new shift the posts under the reader
// and makes the reaction wait for the round trip.

type PostLike = {
  id: string
  my_reaction?: string
  reaction_count?: number
  reaction_counts?: Record<string, number>
  liked?: boolean
}

// What the post looks like once its viewer's reaction is `next` ('' = none).
export function withReaction<T extends PostLike>(post: T, next: string): T {
  const prev = post.my_reaction || ''
  if (prev === next) return post
  const counts = { ...(post.reaction_counts || {}) }
  if (prev) {
    counts[prev] = Math.max(0, (counts[prev] || 0) - 1)
    if (counts[prev] === 0) delete counts[prev]
  }
  if (next) counts[next] = (counts[next] || 0) + 1
  const out: T = {
    ...post,
    my_reaction: next,
    reaction_counts: counts,
    reaction_count: Math.max(0, (post.reaction_count || 0) + (next ? 1 : 0) - (prev ? 1 : 0)),
  }
  if ('liked' in post) out.liked = !!next
  return out
}

function walk(node: any, id: string, next: string, depth: number): any {
  if (depth > 6 || node === null || typeof node !== 'object') return node
  if (Array.isArray(node)) {
    let changed = false
    const out = node.map(n => {
      const r = walk(n, id, next, depth + 1)
      if (r !== n) changed = true
      return r
    })
    return changed ? out : node
  }
  if (node.id === id && 'my_reaction' in node) return withReaction(node, next)
  let changed = false
  const out: any = {}
  for (const k of Object.keys(node)) {
    const r = walk(node[k], id, next, depth + 1)
    if (r !== node[k]) changed = true
    out[k] = r
  }
  return changed ? out : node
}

// Set the viewer's reaction on every cached copy of the post.
export function setReactionInCache(qc: QueryClient, postId: string, next: string) {
  qc.setQueriesData({}, (old: any) => walk(old, postId, next, 0))
}
