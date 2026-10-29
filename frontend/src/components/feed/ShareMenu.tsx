import { useEffect, useRef, useState } from 'react'
import { Share, Link2, Check, Mail, Cloud, Globe, Ellipsis } from 'lucide-react'

// AGORA-411: Share sends a post's link somewhere else, as distinct from
// Repost, which republishes it on Agora.

interface ShareablePost {
  id: string
  visibility: string
  external_only?: boolean
  group_id?: string
  author_profile_private?: boolean
  is_remote?: boolean
  remote_instance?: string
  remote_url?: string
  author_display_name?: string
  author_username?: string
  content?: string
}

// Only posts a signed-out visitor can open get a Share button: a link to a
// friends-only, friend-list, private or group post, or to any post by a
// private profile, would only lead anyone else to an access-denied page.
// Fediverse/Bluesky-only posts are stored private, so the visibility check
// covers them too.
export function isShareable(post: ShareablePost): boolean {
  return post.visibility === 'public' && !post.external_only && !post.group_id && !post.author_profile_private
}

const BLUESKY_SENTINEL = 'bsky.app'

// shareUrl picks the link that will work best for whoever receives it: a post
// written on Agora shares its permalink here, and a post from the Fediverse or
// Bluesky shares the original on its own network, where the recipient can
// follow the author and see the whole conversation.
export function shareUrl(post: ShareablePost, origin = window.location.origin): string {
  const permalink = `${origin}/post/${post.id}`
  const remote = post.remote_url || ''
  if (!post.is_remote || !remote) return permalink
  if (remote.startsWith('at://')) {
    // at://did:plc:xyz/app.bsky.feed.post/rkey
    const m = remote.match(/^at:\/\/([^/]+)\/app\.bsky\.feed\.post\/([^/?#]+)$/)
    return m ? `https://bsky.app/profile/${m[1]}/post/${m[2]}` : permalink
  }
  if (post.remote_instance !== BLUESKY_SENTINEL && /^https?:\/\//.test(remote)) return remote
  return permalink
}

// The few words that go with the link in an email subject or a composed post.
function shareTitle(post: ShareablePost): string {
  const who = post.author_display_name || post.author_username || 'someone'
  return `Post by ${who}`
}

const MASTODON_SERVER_KEY = 'agora_share_mastodon_server'

function rememberedMastodonServer(): string {
  try { return localStorage.getItem(MASTODON_SERVER_KEY) || '' } catch { return '' }
}

// Accepts "mastodon.social", "https://mastodon.social/", or "@me@mastodon.social"
// and returns the bare host, or '' when it doesn't look like one.
export function normalizeServer(input: string): string {
  let s = input.trim().toLowerCase()
  if (s.includes('@')) s = s.slice(s.lastIndexOf('@') + 1)
  s = s.replace(/^https?:\/\//, '').replace(/\/.*$/, '')
  return /^[a-z0-9-]+(\.[a-z0-9-]+)+(:\d+)?$/.test(s) ? s : ''
}

export default function ShareMenu({ post }: { post: ShareablePost }) {
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const [askServer, setAskServer] = useState(false)
  const [server, setServer] = useState(rememberedMastodonServer)
  const ref = useRef<HTMLDivElement>(null)
  const url = shareUrl(post)
  const title = shareTitle(post)
  const canNativeShare = typeof navigator !== 'undefined' && typeof navigator.share === 'function'

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) close()
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') close() }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const close = () => { setOpen(false); setAskServer(false) }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url)
    } catch {
      // Older browsers, or a page not served over HTTPS: fall back to a
      // hidden textarea and the legacy copy command.
      const ta = document.createElement('textarea')
      ta.value = url
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
    }
    setCopied(true)
    setTimeout(() => { setCopied(false); close() }, 1200)
  }

  const openWindow = (href: string) => {
    window.open(href, '_blank', 'noopener,noreferrer')
    close()
  }

  const shareToMastodon = (host: string) => {
    try { localStorage.setItem(MASTODON_SERVER_KEY, host) } catch { /* private mode */ }
    openWindow(`https://${host}/share?text=${encodeURIComponent(url)}`)
  }

  const nativeShare = async () => {
    close()
    try { await navigator.share({ title, url }) } catch { /* dismissed */ }
  }

  const item = 'flex items-center gap-2 w-full px-3 py-2 text-sm text-agora-600 dark:text-agora-300 hover:bg-agora-50 dark:hover:bg-agora-700 text-left'
  const host = normalizeServer(server)

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => (open ? close() : setOpen(true))}
        title="Share"
        aria-label="Share"
        aria-haspopup="menu"
        aria-expanded={open}
        className="flex items-center gap-1.5 text-sm transition-colors hover:text-agora-700 dark:hover:text-agora-200">
        <Share size={16} />
      </button>
      {open && (
        <div role="menu"
          className="absolute left-0 top-7 z-20 bg-white dark:bg-agora-800 border border-agora-200 dark:border-agora-700 rounded-lg shadow-lg py-1 w-56">
          {!askServer ? (
            <>
              <button role="menuitem" onClick={copy} className={item}>
                {copied ? <Check size={14} className="text-green-500" /> : <Link2 size={14} />}
                {copied ? 'Link copied' : 'Copy link'}
              </button>
              <button role="menuitem" className={item}
                onClick={() => { window.location.href = `mailto:?subject=${encodeURIComponent(title)}&body=${encodeURIComponent(url)}`; close() }}>
                <Mail size={14} /> Email
              </button>
              <button role="menuitem" className={item}
                onClick={() => openWindow(`https://bsky.app/intent/compose?text=${encodeURIComponent(url)}`)}>
                <Cloud size={14} /> Post to Bluesky
              </button>
              <button role="menuitem" className={item}
                onClick={() => (host ? shareToMastodon(host) : setAskServer(true))}>
                <Globe size={14} />
                <span className="flex-1 truncate">Post to Mastodon{host ? ` (${host})` : ''}</span>
              </button>
              {host && (
                <button role="menuitem" className={`${item} text-xs text-agora-400 dark:text-agora-500 pl-9 pt-0`}
                  onClick={() => setAskServer(true)}>
                  Use a different server
                </button>
              )}
              {canNativeShare && (
                <button role="menuitem" onClick={nativeShare} className={item}>
                  <Ellipsis size={14} /> More options…
                </button>
              )}
            </>
          ) : (
            <form className="p-3 space-y-2"
              onSubmit={e => { e.preventDefault(); if (host) shareToMastodon(host) }}>
              <label className="block text-xs font-medium text-agora-600 dark:text-agora-300" htmlFor={`masto-${post.id}`}>
                Your Mastodon or Fediverse server
              </label>
              <input id={`masto-${post.id}`} className="input w-full text-sm" autoFocus
                placeholder="mastodon.social"
                value={server} onChange={e => setServer(e.target.value)} />
              <div className="flex justify-end gap-2">
                <button type="button" className="btn-secondary text-xs px-2 py-1" onClick={() => setAskServer(false)}>Back</button>
                <button type="submit" className="btn-primary text-xs px-2 py-1" disabled={!host}>Post</button>
              </div>
            </form>
          )}
        </div>
      )}
    </div>
  )
}
